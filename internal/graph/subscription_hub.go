package graph

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	uuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const (
	subscriptionHubPollInterval            = time.Second
	subscriptionHubMaxIdlePollInterval     = 5 * time.Second
	subscriptionHubMaxRetryInterval        = 30 * time.Second
	subscriptionAuthorizationCheckInterval = 30 * time.Second
)

type subscriptionHub struct {
	db           *pgxpool.Pool
	logger       *slog.Logger
	mu           sync.Mutex
	nextID       uint64
	purchases    map[uint64]*purchaseSubscriptionWatch
	drafts       map[uint64]*sharedDraftSubscriptionWatch
	cancelPoller context.CancelFunc
}

type purchaseSubscriptionWatch struct {
	householdID uuid.UUID
	cursor      int64
	changes     chan *model.PurchaseChange
}

type sharedDraftSubscriptionWatch struct {
	draftID uuid.UUID
	cursor  int64
	changes chan *model.SharedPurchaseDraftChange
}

type purchaseSubscriptionCursor struct {
	householdID uuid.UUID
	cursor      int64
}

type sharedDraftSubscriptionCursor struct {
	draftID uuid.UUID
	cursor  int64
}

func newSubscriptionHub(db *pgxpool.Pool, logger *slog.Logger) *subscriptionHub {
	if logger == nil {
		logger = slog.Default()
	}
	return &subscriptionHub{
		db: db, logger: logger, purchases: make(map[uint64]*purchaseSubscriptionWatch),
		drafts: make(map[uint64]*sharedDraftSubscriptionWatch),
	}
}

func (h *subscriptionHub) watchPurchases(householdID uuid.UUID, cursor int64) (<-chan *model.PurchaseChange, func()) {
	watch := &purchaseSubscriptionWatch{householdID: householdID, cursor: cursor, changes: make(chan *model.PurchaseChange, 16)}
	h.mu.Lock()
	h.nextID++
	id := h.nextID
	h.purchases[id] = watch
	h.startPollerLocked()
	h.mu.Unlock()
	return watch.changes, func() { h.removePurchaseWatch(id) }
}

func (h *subscriptionHub) watchDraft(draftID uuid.UUID, cursor int64) (<-chan *model.SharedPurchaseDraftChange, func()) {
	watch := &sharedDraftSubscriptionWatch{draftID: draftID, cursor: cursor, changes: make(chan *model.SharedPurchaseDraftChange, 1)}
	h.mu.Lock()
	h.nextID++
	id := h.nextID
	h.drafts[id] = watch
	h.startPollerLocked()
	h.mu.Unlock()
	return watch.changes, func() { h.removeDraftWatch(id) }
}

func (h *subscriptionHub) startPollerLocked() {
	if h.cancelPoller != nil || h.db == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.cancelPoller = cancel
	go h.run(ctx)
}

func (h *subscriptionHub) removePurchaseWatch(id uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if watch, ok := h.purchases[id]; ok {
		delete(h.purchases, id)
		close(watch.changes)
	}
	h.stopPollerWhenIdleLocked()
}

func (h *subscriptionHub) removeDraftWatch(id uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if watch, ok := h.drafts[id]; ok {
		delete(h.drafts, id)
		close(watch.changes)
	}
	h.stopPollerWhenIdleLocked()
}

func (h *subscriptionHub) stopPollerWhenIdleLocked() {
	if len(h.purchases)+len(h.drafts) != 0 || h.cancelPoller == nil {
		return
	}
	h.cancelPoller()
	h.cancelPoller = nil
}

func (h *subscriptionHub) run(ctx context.Context) {
	retryInterval := subscriptionHubPollInterval
	idlePollInterval := subscriptionHubPollInterval
	failed := false
	for {
		active, err := h.poll(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if !failed {
				h.logger.WarnContext(ctx, "GraphQL subscription poll failed", "error", err)
				failed = true
			}
			if !waitForSubscriptionPoll(ctx, retryInterval) {
				return
			}
			retryInterval = min(retryInterval*2, subscriptionHubMaxRetryInterval)
			continue
		}
		if failed {
			h.logger.InfoContext(ctx, "GraphQL subscription poll recovered")
			failed = false
		}
		retryInterval = subscriptionHubPollInterval
		if active {
			idlePollInterval = subscriptionHubPollInterval
		} else {
			idlePollInterval = nextSubscriptionPollInterval(idlePollInterval, false)
		}
		if !waitForSubscriptionPoll(ctx, idlePollInterval) {
			return
		}
	}
}

func nextSubscriptionPollInterval(current time.Duration, active bool) time.Duration {
	if active {
		return subscriptionHubPollInterval
	}
	return min(current*2, subscriptionHubMaxIdlePollInterval)
}

func waitForSubscriptionPoll(ctx context.Context, interval time.Duration) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (h *subscriptionHub) poll(ctx context.Context) (bool, error) {
	purchaseCursors, draftCursors := h.subscriptionCursors()
	active := false
	if len(purchaseCursors) > 0 {
		found, err := h.pollPurchases(ctx, purchaseCursors)
		if err != nil {
			return false, err
		}
		active = active || found
	}
	if len(draftCursors) > 0 {
		found, err := h.pollDrafts(ctx, draftCursors)
		if err != nil {
			return false, err
		}
		active = active || found
	}
	return active, nil
}

func (h *subscriptionHub) subscriptionCursors() ([]purchaseSubscriptionCursor, []sharedDraftSubscriptionCursor) {
	h.mu.Lock()
	defer h.mu.Unlock()
	purchaseByHousehold := make(map[uuid.UUID]int64)
	for _, watch := range h.purchases {
		cursor, ok := purchaseByHousehold[watch.householdID]
		if !ok || watch.cursor < cursor {
			purchaseByHousehold[watch.householdID] = watch.cursor
		}
	}
	draftByID := make(map[uuid.UUID]int64)
	for _, watch := range h.drafts {
		cursor, ok := draftByID[watch.draftID]
		if !ok || watch.cursor < cursor {
			draftByID[watch.draftID] = watch.cursor
		}
	}
	purchaseCursors := make([]purchaseSubscriptionCursor, 0, len(purchaseByHousehold))
	for householdID, cursor := range purchaseByHousehold {
		purchaseCursors = append(purchaseCursors, purchaseSubscriptionCursor{householdID: householdID, cursor: cursor})
	}
	draftCursors := make([]sharedDraftSubscriptionCursor, 0, len(draftByID))
	for draftID, cursor := range draftByID {
		draftCursors = append(draftCursors, sharedDraftSubscriptionCursor{draftID: draftID, cursor: cursor})
	}
	return purchaseCursors, draftCursors
}

func (h *subscriptionHub) pollPurchases(ctx context.Context, cursors []purchaseSubscriptionCursor) (bool, error) {
	conditions := make([]bob.Expression, 0, len(cursors))
	for _, cursor := range cursors {
		conditions = append(conditions, psql.And(
			models.HouseholdEvents.Columns.HouseholdID.EQ(psql.Arg(cursor.householdID)),
			models.HouseholdEvents.Columns.Cursor.GT(psql.Arg(cursor.cursor)),
		))
	}
	events, err := models.HouseholdEvents.Query(
		sm.Where(psql.Or(conditions...)),
		sm.OrderBy(models.HouseholdEvents.Columns.Cursor),
		sm.Limit(100),
	).All(ctx, bobpgx.NewPool(h.db))
	if err != nil {
		return false, err
	}
	blocked := make(map[uint64]bool)
	for _, event := range events {
		change, ok := purchaseChange(event)
		if !ok {
			h.advancePurchaseCursors(event.HouseholdID, event.Cursor, blocked)
			continue
		}
		if !h.hasAvailablePurchaseWatch(event.HouseholdID, event.Cursor, blocked) {
			h.blockFullPurchaseWatches(event.HouseholdID, event.Cursor, blocked)
			continue
		}
		change.Purchase, err = purchaseForChange(ctx, h.db, event)
		if errors.Is(err, ErrNotFound) {
			h.advancePurchaseCursors(event.HouseholdID, event.Cursor, blocked)
			continue
		}
		if err != nil {
			return false, err
		}
		h.publishPurchaseChange(event.HouseholdID, event.Cursor, change, blocked)
	}
	return len(events) > 0, nil
}

func (h *subscriptionHub) blockFullPurchaseWatches(householdID uuid.UUID, cursor int64, blocked map[uint64]bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, watch := range h.purchases {
		if watch.householdID == householdID && watch.cursor < cursor && len(watch.changes) == cap(watch.changes) {
			blocked[id] = true
		}
	}
}

func (h *subscriptionHub) hasAvailablePurchaseWatch(householdID uuid.UUID, cursor int64, blocked map[uint64]bool) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, watch := range h.purchases {
		if blocked[id] || watch.householdID != householdID || watch.cursor >= cursor {
			continue
		}
		if len(watch.changes) < cap(watch.changes) {
			return true
		}
	}
	return false
}

func (h *subscriptionHub) publishPurchaseChange(householdID uuid.UUID, cursor int64, change *model.PurchaseChange, blocked map[uint64]bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, watch := range h.purchases {
		if blocked[id] || watch.householdID != householdID || watch.cursor >= cursor {
			continue
		}
		select {
		case watch.changes <- change:
			watch.cursor = cursor
		default:
			blocked[id] = true
		}
	}
}

func (h *subscriptionHub) advancePurchaseCursors(householdID uuid.UUID, cursor int64, blocked map[uint64]bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, watch := range h.purchases {
		if !blocked[id] && watch.householdID == householdID && watch.cursor < cursor {
			watch.cursor = cursor
		}
	}
}

func (h *subscriptionHub) pollDrafts(ctx context.Context, cursors []sharedDraftSubscriptionCursor) (bool, error) {
	conditions := make([]bob.Expression, 0, len(cursors))
	for _, cursor := range cursors {
		conditions = append(conditions, psql.And(
			models.SharedPurchaseDrafts.Columns.ID.EQ(psql.Arg(cursor.draftID)),
			models.SharedPurchaseDrafts.Columns.Revision.GT(psql.Arg(cursor.cursor)),
		))
	}
	drafts, err := models.SharedPurchaseDrafts.Query(
		sm.Where(psql.Or(conditions...)),
	).All(ctx, bobpgx.NewPool(h.db))
	if err != nil {
		return false, err
	}
	blocked := make(map[uint64]bool)
	for _, draft := range drafts {
		h.publishDraftChange(draft.ID, draft.Revision, blocked)
	}
	return len(drafts) > 0, nil
}

func (h *subscriptionHub) publishDraftChange(draftID uuid.UUID, revision int64, blocked map[uint64]bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, watch := range h.drafts {
		if blocked[id] || watch.draftID != draftID || watch.cursor >= revision {
			continue
		}
		select {
		case watch.changes <- sharedPurchaseDraftChange(revision):
			watch.cursor = revision
		default:
			blocked[id] = true
		}
	}
}
