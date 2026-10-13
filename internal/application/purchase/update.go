package purchase

import (
	"context"
	"database/sql"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const (
	updateFieldOccurredAt         = "occurredAt"
	updateFieldMerchant           = "merchant"
	updateFieldPaymentAllocations = "paymentAllocations"
)

var purchaseUpdateLocation = time.FixedZone("JST", 9*60*60)

func (s *Service) Update(ctx context.Context, command UpdateCommand, operations UpdateOperations) (UpdateResult, error) {
	if len(command.ChangedFields) == 0 {
		return UpdateResult{}, fmt.Errorf("%w: patch must contain a field", ErrBadInput)
	}
	seen := make(map[string]struct{}, len(command.ChangedFields))
	for _, field := range command.ChangedFields {
		if field != updateFieldOccurredAt && field != updateFieldMerchant && field != updateFieldPaymentAllocations {
			return UpdateResult{}, fmt.Errorf("%w: invalid changed field", ErrBadInput)
		}
		if _, exists := seen[field]; exists {
			return UpdateResult{}, fmt.Errorf("%w: duplicate changed field", ErrBadInput)
		}
		seen[field] = struct{}{}
	}
	if _, ok := seen[updateFieldOccurredAt]; ok && command.OccurredAt == nil {
		return UpdateResult{}, fmt.Errorf("%w: occurredAt is required", ErrBadInput)
	}
	if _, ok := seen[updateFieldMerchant]; ok && command.Merchant == nil {
		return UpdateResult{}, fmt.Errorf("%w: merchant is required", ErrBadInput)
	}
	if _, ok := seen[updateFieldPaymentAllocations]; ok && command.PaymentAllocations == nil {
		return UpdateResult{}, fmt.Errorf("%w: payment allocations are required", ErrBadInput)
	}

	purchaseUUID, err := gofrsuuid.FromString(command.PurchaseID)
	if err != nil {
		return UpdateResult{}, ErrNotFound
	}
	actorUUID, err := gofrsuuid.FromString(command.ActorID)
	if err != nil {
		return UpdateResult{}, ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return UpdateResult{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)

	observed, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseUUID)))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return UpdateResult{}, ErrNotFound
	}
	if err != nil {
		return UpdateResult{}, err
	}
	observedHouseholdID := observed.HouseholdID.String()
	if err := requirePurchaseMember(ctx, executor, observed.HouseholdID, actorUUID); err != nil {
		return UpdateResult{}, err
	}
	targetOccurredAt := observed.OccurredAt
	if command.OccurredAt != nil {
		targetOccurredAt = *command.OccurredAt
	}
	if operations.LockAccountingPeriods == nil {
		return UpdateResult{}, errors.New("purchase update requires accounting period locking")
	}
	if err := operations.LockAccountingPeriods(ctx, tx, observedHouseholdID,
		purchaseAccountingPeriod(observed.OccurredAt), purchaseAccountingPeriod(targetOccurredAt)); err != nil {
		return UpdateResult{}, err
	}

	purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseUUID))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return UpdateResult{}, ErrNotFound
	}
	if err != nil {
		return UpdateResult{}, err
	}
	householdID := purchase.HouseholdID.String()
	status := purchase.Status
	if householdID != observedHouseholdID || purchaseAccountingPeriod(purchase.OccurredAt) != purchaseAccountingPeriod(observed.OccurredAt) {
		return UpdateResult{}, ErrConflict
	}
	if err := requirePurchaseMember(ctx, executor, purchase.HouseholdID, actorUUID); err != nil {
		return UpdateResult{}, err
	}
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(purchase.HouseholdID))), sm.ForShare()).One(ctx, executor); err != nil {
		return UpdateResult{}, err
	}
	if status != draftStatus && status != postedStatus {
		return UpdateResult{}, fmt.Errorf("%w: closed purchases require a correction", ErrConflict)
	}
	if command.BaseRevision > purchase.Revision {
		return UpdateResult{}, ErrConflict
	}
	conflict, err := purchaseUpdateValuesConflict(purchase.FieldRevisions.Val, command.BaseRevision, command.ChangedFields)
	if err != nil {
		return UpdateResult{}, err
	}
	if conflict {
		return UpdateResult{}, ErrConflict
	}
	newRevision := purchase.Revision + 1
	updatedFields, err := purchaseUpdateFieldRevisions(purchase.FieldRevisions.Val, command.ChangedFields, newRevision)
	if err != nil {
		return UpdateResult{}, err
	}

	updatePayments := slices.Contains(command.ChangedFields, updateFieldPaymentAllocations)
	paymentAccountID := purchase.PaymentAccountID
	if updatePayments {
		paymentAccountID = sql.Null[gofrsuuid.UUID]{}
	}
	if command.OccurredAt != nil {
		purchase.OccurredAt = *command.OccurredAt
	}
	if command.Merchant != nil {
		merchant := strings.TrimSpace(*command.Merchant)
		if len(merchant) > 240 {
			return UpdateResult{}, fmt.Errorf("%w: merchant name is too long", ErrBadInput)
		}
		purchase.Merchant = merchant
	}
	if updatePayments {
		if operations.ReplacePayments == nil {
			return UpdateResult{}, errors.New("purchase update requires payment replacement")
		}
		payments := *command.PaymentAllocations
		var total int64
		items, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, executor)
		if err != nil {
			return UpdateResult{}, err
		}
		for _, item := range items {
			var ok bool
			total, ok = checkedAdd(total, item.AmountYen)
			if !ok {
				return UpdateResult{}, fmt.Errorf("%w: purchase total is too large", ErrBadInput)
			}
		}
		if status == postedStatus {
			var paid int64
			for _, payment := range payments {
				var ok bool
				paid, ok = checkedAdd(paid, payment.AmountYen)
				if !ok {
					return UpdateResult{}, fmt.Errorf("%w: payment total is too large", ErrBadInput)
				}
			}
			if paid != total {
				return UpdateResult{}, fmt.Errorf("%w: payment allocations must equal purchase total", ErrBadInput)
			}
		}
		if err := operations.ReplacePayments(ctx, tx, householdID, command.PurchaseID, payments); err != nil {
			return UpdateResult{}, err
		}
	}

	fieldRevisions := types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(updatedFields)}
	if _, err := models.Purchases.Update((&models.PurchaseSetter{
		OccurredAt: &purchase.OccurredAt, Merchant: &purchase.Merchant, PaymentAccountID: &paymentAccountID,
		Revision: &newRevision, FieldRevisions: &fieldRevisions,
	}).UpdateMod(),
		um.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseUUID))),
		um.SetCol("updated_at").To(psql.F("now")),
	).Exec(ctx, executor); err != nil {
		return UpdateResult{}, err
	}
	if status == postedStatus {
		if operations.SyncAccounting == nil {
			return UpdateResult{}, errors.New("purchase update requires accounting synchronization")
		}
		if err := operations.SyncAccounting(ctx, tx, command.PurchaseID); err != nil {
			return UpdateResult{}, err
		}
	}
	if err := insertPurchaseUpdateEvent(ctx, executor, purchase.HouseholdID, actorUUID, purchaseUUID, newRevision, command.ChangedFields); err != nil {
		return UpdateResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return UpdateResult{}, err
	}
	return UpdateResult{PurchaseID: command.PurchaseID}, nil
}

func requirePurchaseMember(ctx context.Context, executor bob.Executor, householdID, actorID gofrsuuid.UUID) error {
	member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(actorID)),
	))).Exists(ctx, executor)
	if err != nil {
		return err
	}
	if !member {
		return ErrNotFound
	}
	return nil
}

func purchaseAccountingPeriod(value time.Time) string {
	return value.In(purchaseUpdateLocation).Format("2006-01")
}

func purchaseUpdateValuesConflict(fieldRevisions []byte, base int64, fields []string) (bool, error) {
	if base < 1 {
		return true, nil
	}
	var revisions map[string]int64
	if len(fieldRevisions) == 0 {
		return false, nil
	}
	if err := stdjson.Unmarshal(fieldRevisions, &revisions); err != nil {
		return false, err
	}
	for _, field := range fields {
		if revisions[field] > base {
			return true, nil
		}
	}
	return false, nil
}

func purchaseUpdateFieldRevisions(current []byte, fields []string, revision int64) ([]byte, error) {
	var revisions map[string]int64
	if len(current) == 0 {
		revisions = make(map[string]int64)
	} else if err := stdjson.Unmarshal(current, &revisions); err != nil {
		return nil, err
	}
	for _, field := range fields {
		revisions[field] = revision
	}
	return stdjson.Marshal(revisions)
}

func insertPurchaseUpdateEvent(ctx context.Context, executor bob.Executor, householdID, actorID, purchaseID gofrsuuid.UUID, revision int64, fields []string) error {
	changedFields, err := stdjson.Marshal(fields)
	if err != nil {
		return err
	}
	eventType := "purchase.updated"
	actor := sql.Null[gofrsuuid.UUID]{V: actorID, Valid: true}
	encodedFields := types.JSON[stdjson.RawMessage]{Val: changedFields}
	_, err = models.HouseholdEvents.Insert(&models.HouseholdEventSetter{
		HouseholdID: &householdID, Type: &eventType, EntityID: &purchaseID, Revision: &revision,
		ChangedFields: &encodedFields, ActorID: &actor,
	}).Exec(ctx, executor)
	return err
}
