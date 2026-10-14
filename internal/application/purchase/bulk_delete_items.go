package purchase

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/txcleanup"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type BulkDeleteItemsCommand struct {
	ActorID string
	Targets []BulkItemTarget
}

type BulkDeleteItemsResult struct {
	DeletedItemIDs []string
}

func (s *Service) BulkDeleteItems(ctx context.Context, command BulkDeleteItemsCommand) (BulkDeleteItemsResult, error) {
	if len(command.Targets) == 0 || len(command.Targets) > 100 {
		return BulkDeleteItemsResult{}, fmt.Errorf("%w: select between one and one hundred items", ErrBadInput)
	}
	actorID, err := parseUUID(command.ActorID, "actor")
	if err != nil {
		return BulkDeleteItemsResult{}, err
	}
	type targetState struct {
		command BulkItemTarget
		id      gofrsuuid.UUID
		item    *models.PurchaseItem
	}
	targets := make([]targetState, 0, len(command.Targets))
	seen := make(map[gofrsuuid.UUID]struct{}, len(command.Targets))
	for _, target := range command.Targets {
		id, err := parseUUID(target.ItemID, "item")
		if err != nil {
			return BulkDeleteItemsResult{}, ErrNotFound
		}
		if _, exists := seen[id]; exists {
			return BulkDeleteItemsResult{}, fmt.Errorf("%w: duplicate item target", ErrBadInput)
		}
		seen[id] = struct{}{}
		targets = append(targets, targetState{command: target, id: id})
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return BulkDeleteItemsResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			txcleanup.Rollback(ctx, tx)
		}
	}()
	executor := bobpgx.NewTx(tx, nil)
	var householdID gofrsuuid.UUID
	purchases := make(map[gofrsuuid.UUID]*models.Purchase)
	periods := make([]string, 0, len(targets))
	for index := range targets {
		item, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(targets[index].id)))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return BulkDeleteItemsResult{}, ErrNotFound
		}
		if err != nil {
			return BulkDeleteItemsResult{}, err
		}
		targets[index].item = item
		if index == 0 {
			householdID = item.HouseholdID
		} else if householdID != item.HouseholdID {
			return BulkDeleteItemsResult{}, fmt.Errorf("%w: all items must belong to one household", ErrBadInput)
		}
		if _, exists := purchases[item.PurchaseID]; !exists {
			purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(item.PurchaseID)))).One(ctx, executor)
			if errors.Is(err, sql.ErrNoRows) {
				return BulkDeleteItemsResult{}, ErrNotFound
			}
			if err != nil {
				return BulkDeleteItemsResult{}, err
			}
			purchases[item.PurchaseID] = purchase
			periods = append(periods, accounting.PeriodFor(purchase.OccurredAt))
		}
	}
	if err := requirePurchaseMember(ctx, executor, householdID, actorID); err != nil {
		return BulkDeleteItemsResult{}, err
	}
	if err := lockPurchaseAccountingPeriods(ctx, executor, householdID, periods...); err != nil {
		return BulkDeleteItemsResult{}, err
	}
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForShare()).One(ctx, executor); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return BulkDeleteItemsResult{}, ErrNotFound
		}
		return BulkDeleteItemsResult{}, err
	}
	purchaseIDs := make([]gofrsuuid.UUID, 0, len(purchases))
	for id := range purchases {
		purchaseIDs = append(purchaseIDs, id)
	}
	slices.SortFunc(purchaseIDs, func(left, right gofrsuuid.UUID) int {
		return strings.Compare(left.String(), right.String())
	})
	for _, id := range purchaseIDs {
		purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(id))), sm.ForUpdate()).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return BulkDeleteItemsResult{}, ErrNotFound
		}
		if err != nil {
			return BulkDeleteItemsResult{}, err
		}
		observed := purchases[id]
		if purchase.HouseholdID != householdID || !purchase.OccurredAt.Equal(observed.OccurredAt) {
			return BulkDeleteItemsResult{}, ErrConflict
		}
		if purchase.Status != draftStatus && purchase.Status != postedStatus {
			return BulkDeleteItemsResult{}, fmt.Errorf("%w: closed purchases require a correction", ErrConflict)
		}
		purchases[id] = purchase
	}
	lockedItems := make([]targetState, 0, len(targets))
	lockOrder := slices.Clone(targets)
	slices.SortFunc(lockOrder, func(left, right targetState) int {
		return strings.Compare(left.id.String(), right.id.String())
	})
	for _, target := range lockOrder {
		item, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(target.id))), sm.ForUpdate()).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return BulkDeleteItemsResult{}, ErrNotFound
		}
		if err != nil {
			return BulkDeleteItemsResult{}, err
		}
		if item.HouseholdID != householdID || item.PurchaseID != target.item.PurchaseID || int(item.Revision) != target.command.BaseRevision {
			return BulkDeleteItemsResult{}, ErrConflict
		}
		lockedItems = append(lockedItems, targetState{command: target.command, id: target.id, item: item})
	}
	targetsByPurchase := make(map[gofrsuuid.UUID]int, len(purchases))
	for _, target := range lockedItems {
		targetsByPurchase[target.item.PurchaseID]++
		_, err := models.Corrections.Query(sm.Where(psql.And(
			models.Corrections.Columns.PurchaseID.EQ(psql.Arg(target.item.PurchaseID)),
			models.Corrections.Columns.ItemID.EQ(psql.Arg(target.id)),
		)), sm.Limit(1)).One(ctx, executor)
		if err == nil {
			return BulkDeleteItemsResult{}, fmt.Errorf("%w: corrected items cannot be deleted", ErrConflict)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return BulkDeleteItemsResult{}, err
		}
	}
	for _, id := range purchaseIDs {
		items, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(id)))).All(ctx, executor)
		if err != nil {
			return BulkDeleteItemsResult{}, err
		}
		if len(items) <= targetsByPurchase[id] {
			return BulkDeleteItemsResult{}, fmt.Errorf("%w: purchases must retain at least one item", ErrConflict)
		}
	}
	deletedAmountByPurchase := make(map[gofrsuuid.UUID]int64, len(purchases))
	for _, target := range lockedItems {
		amount, ok := checkedAdd(deletedAmountByPurchase[target.item.PurchaseID], target.item.AmountYen)
		if !ok || amount > maxMoney {
			return BulkDeleteItemsResult{}, fmt.Errorf("%w: purchase total is too large", ErrBadInput)
		}
		deletedAmountByPurchase[target.item.PurchaseID] = amount
	}
	for _, id := range purchaseIDs {
		deletedAmount := deletedAmountByPurchase[id]
		if deletedAmount == 0 {
			continue
		}
		purchase := purchases[id]
		payments, updatePayments, err := adjustedPurchasePayments(ctx, executor, purchase, deletedAmount, 0)
		if err != nil {
			return BulkDeleteItemsResult{}, err
		}
		if updatePayments {
			validated, err := validatePayments(ctx, tx, householdID, payments)
			if err != nil {
				return BulkDeleteItemsResult{}, err
			}
			if err := replaceUpdatePayments(ctx, executor, purchase, validated); err != nil {
				return BulkDeleteItemsResult{}, err
			}
		}
	}
	for _, target := range lockedItems {
		if _, err := models.PurchaseItems.Delete(dm.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(target.id)))).Exec(ctx, executor); err != nil {
			return BulkDeleteItemsResult{}, err
		}
		if err := accounting.RecordEvent(ctx, executor, householdID, target.id, "purchase.item.deleted", target.item.Revision+1, []string{"deleted"}, actorID); err != nil {
			return BulkDeleteItemsResult{}, err
		}
	}
	for _, id := range purchaseIDs {
		purchase := purchases[id]
		if _, err := models.Purchases.Update(um.Where(models.Purchases.Columns.ID.EQ(psql.Arg(id))), um.SetCol("revision").To(models.Purchases.Columns.Revision.Plus(psql.Arg(int64(1)))), um.SetCol("updated_at").To(psql.F("now"))).Exec(ctx, executor); err != nil {
			return BulkDeleteItemsResult{}, err
		}
		if purchase.Status == postedStatus {
			if err := syncPostedPurchaseAccounting(ctx, executor, purchase); err != nil {
				return BulkDeleteItemsResult{}, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return BulkDeleteItemsResult{}, err
	}
	committed = true
	result := BulkDeleteItemsResult{DeletedItemIDs: make([]string, 0, len(lockedItems))}
	for _, target := range lockedItems {
		result.DeletedItemIDs = append(result.DeletedItemIDs, target.id.String())
	}
	return result, nil
}
