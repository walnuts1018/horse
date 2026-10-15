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
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/inputvalidation"
	"github.com/walnuts1018/horse/internal/application/txcleanup"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const (
	updateFieldOccurredAt         = "occurredAt"
	updateFieldMerchant           = "merchant"
	updateFieldPaymentAllocations = "paymentAllocations"
)

func (s *Service) Update(ctx context.Context, command UpdateCommand) (UpdateResult, error) {
	if err := validatePurchaseUpdateCommand(command); err != nil {
		return UpdateResult{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return UpdateResult{}, err
	}
	defer txcleanup.Rollback(ctx, tx)
	result, err := s.updateInTransaction(ctx, tx, command, purchaseUpdateOptions{})
	if err != nil {
		return UpdateResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return UpdateResult{}, err
	}
	return result, nil
}

type purchaseUpdateOptions struct {
	deferAccountingSync bool
	accountingChanged   *bool
}

type purchaseUpdateValidationInput struct {
	changedFields      []string
	occurredAt         *time.Time
	datePrecision      *string
	merchant           *string
	paymentAllocations *[]Payment
}

type purchaseUpdatePlan struct {
	revision          int64
	fieldRevisions    []byte
	paymentAccountID  sql.Null[gofrsuuid.UUID]
	updatesPayments   bool
	accountingChanged bool
}

func validatePurchaseUpdateFields(input purchaseUpdateValidationInput) error {
	if len(input.changedFields) == 0 {
		return fmt.Errorf("%w: patch must contain a field", ErrBadInput)
	}
	seen := make(map[string]struct{}, len(input.changedFields))
	for _, field := range input.changedFields {
		if field != updateFieldOccurredAt && field != updateFieldMerchant && field != updateFieldPaymentAllocations {
			return fmt.Errorf("%w: invalid changed field", ErrBadInput)
		}
		if _, exists := seen[field]; exists {
			return fmt.Errorf("%w: duplicate changed field", ErrBadInput)
		}
		seen[field] = struct{}{}
	}
	if slices.Contains(input.changedFields, updateFieldOccurredAt) && input.occurredAt == nil {
		return fmt.Errorf("%w: occurredAt is required", ErrBadInput)
	}
	if input.datePrecision != nil && (input.occurredAt == nil || !slices.Contains(input.changedFields, updateFieldOccurredAt) || !validDatePrecision(*input.datePrecision)) {
		return fmt.Errorf("%w: valid date precision requires occurredAt", ErrBadInput)
	}
	if slices.Contains(input.changedFields, updateFieldMerchant) && input.merchant == nil {
		return fmt.Errorf("%w: merchant is required", ErrBadInput)
	}
	if slices.Contains(input.changedFields, updateFieldPaymentAllocations) && input.paymentAllocations == nil {
		return fmt.Errorf("%w: payment allocations are required", ErrBadInput)
	}
	return nil
}

func validatePurchaseUpdateCommand(command UpdateCommand) error {
	if err := inputvalidation.Check(command); err != nil {
		return err
	}
	if err := validatePurchaseUpdateFields(purchaseUpdateValidationInput{
		changedFields: command.ChangedFields, occurredAt: command.OccurredAt, datePrecision: command.DatePrecision,
		merchant: command.Merchant, paymentAllocations: command.PaymentAllocations,
	}); err != nil {
		return err
	}
	if _, err := parseUUID(command.PurchaseID, "purchase"); err != nil {
		return err
	}
	if _, err := parseUUID(command.ActorID, "actor"); err != nil {
		return err
	}
	return nil
}

func (s *Service) updateInTransaction(ctx context.Context, tx pgx.Tx, command UpdateCommand, options purchaseUpdateOptions) (UpdateResult, error) {
	if err := validatePurchaseUpdateCommand(command); err != nil {
		return UpdateResult{}, err
	}

	purchaseUUID, err := parseUUID(command.PurchaseID, "purchase")
	if err != nil {
		return UpdateResult{}, err
	}
	actorUUID, err := parseUUID(command.ActorID, "actor")
	if err != nil {
		return UpdateResult{}, err
	}
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
	if err := lockPurchaseAccountingPeriods(ctx, executor, observed.HouseholdID,
		accounting.PeriodFor(observed.OccurredAt), accounting.PeriodFor(targetOccurredAt)); err != nil {
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
	if householdID != observedHouseholdID || accounting.PeriodFor(purchase.OccurredAt) != accounting.PeriodFor(observed.OccurredAt) {
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
	plan, err := planPurchaseUpdate(purchase, observed.OccurredAt, command.BaseRevision, command.ChangedFields, command.OccurredAt, command.DatePrecision, command.Merchant)
	if err != nil {
		return UpdateResult{}, err
	}
	if options.accountingChanged != nil {
		*options.accountingChanged = *options.accountingChanged || plan.accountingChanged
	}
	purchaseAccountingChanged := plan.accountingChanged
	var payments []Payment
	if plan.updatesPayments {
		payments = *command.PaymentAllocations
		if err := validatePurchaseUpdatePaymentAmounts(ctx, executor, purchase, status, payments); err != nil {
			return UpdateResult{}, err
		}
		payments, err = validatePayments(ctx, tx, purchase.HouseholdID, payments)
		if err != nil {
			return UpdateResult{}, err
		}
	}
	if err := applyPurchaseUpdatePlan(ctx, executor, purchase, plan, payments); err != nil {
		return UpdateResult{}, err
	}
	if status == postedStatus && !options.deferAccountingSync {
		if purchaseAccountingChanged {
			if err := syncPostedPurchaseAccounting(ctx, executor, purchase); err != nil {
				return UpdateResult{}, err
			}
		} else if slices.Contains(command.ChangedFields, updateFieldMerchant) {
			total, err := purchaseItemsTotal(ctx, executor, purchaseUUID)
			if err != nil {
				return UpdateResult{}, err
			}
			if err := updatePostedPurchaseLedgerEntries(ctx, executor, purchase, total); err != nil {
				return UpdateResult{}, err
			}
		}
	}
	if err := insertPurchaseUpdateEvent(ctx, executor, purchase.HouseholdID, actorUUID, purchaseUUID, plan.revision, command.ChangedFields); err != nil {
		return UpdateResult{}, err
	}
	return UpdateResult{PurchaseID: command.PurchaseID}, nil
}

func planPurchaseUpdate(purchase *models.Purchase, accountingBaseline time.Time, baseRevision int64, changedFields []string, occurredAt *time.Time, datePrecision, merchant *string) (purchaseUpdatePlan, error) {
	if baseRevision > purchase.Revision {
		return purchaseUpdatePlan{}, ErrConflict
	}
	conflict, err := purchaseUpdateValuesConflict(purchase.FieldRevisions.Val, baseRevision, changedFields)
	if err != nil {
		return purchaseUpdatePlan{}, err
	}
	if conflict {
		return purchaseUpdatePlan{}, ErrConflict
	}
	revision := purchase.Revision + 1
	fieldRevisions, err := purchaseUpdateFieldRevisions(purchase.FieldRevisions.Val, changedFields, revision)
	if err != nil {
		return purchaseUpdatePlan{}, err
	}
	updatesPayments := slices.Contains(changedFields, updateFieldPaymentAllocations)
	paymentAccountID := purchase.PaymentAccountID
	if updatesPayments {
		paymentAccountID = sql.Null[gofrsuuid.UUID]{}
	}
	if occurredAt != nil {
		purchase.OccurredAt = *occurredAt
	}
	if datePrecision != nil {
		purchase.DatePrecision = *datePrecision
	}
	if merchant != nil {
		purchase.Merchant = strings.TrimSpace(*merchant)
	}
	return purchaseUpdatePlan{
		revision: revision, fieldRevisions: fieldRevisions, paymentAccountID: paymentAccountID,
		updatesPayments: updatesPayments, accountingChanged: updatesPayments || !purchase.OccurredAt.Equal(accountingBaseline),
	}, nil
}

func validatePurchaseUpdatePaymentAmounts(ctx context.Context, executor bob.Executor, purchase *models.Purchase, status string, payments []Payment) error {
	total, err := purchaseItemsTotal(ctx, executor, purchase.ID)
	if err != nil {
		return err
	}
	if status != postedStatus {
		return nil
	}
	var paid int64
	for _, payment := range payments {
		var ok bool
		paid, ok = checkedAdd(paid, payment.AmountYen)
		if !ok {
			return fmt.Errorf("%w: payment total is too large", ErrBadInput)
		}
	}
	if paid != total {
		return fmt.Errorf("%w: payment allocations must equal purchase total", ErrBadInput)
	}
	return nil
}

func applyPurchaseUpdatePlan(ctx context.Context, executor bob.Executor, purchase *models.Purchase, plan purchaseUpdatePlan, payments []Payment) error {
	if plan.updatesPayments {
		if err := replaceUpdatePayments(ctx, executor, purchase, payments); err != nil {
			return err
		}
	}
	fieldRevisions := types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(plan.fieldRevisions)}
	_, err := models.Purchases.Update((&models.PurchaseSetter{
		OccurredAt: &purchase.OccurredAt, DatePrecision: &purchase.DatePrecision, Merchant: &purchase.Merchant, PaymentAccountID: &plan.paymentAccountID,
		Revision: &plan.revision, FieldRevisions: &fieldRevisions,
	}).UpdateMod(),
		um.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchase.ID))),
		um.SetCol("updated_at").To(psql.F("now")),
	).Exec(ctx, executor)
	return err
}

func updatePostedPurchaseLedgerEntries(ctx context.Context, executor bob.Executor, purchase *models.Purchase, total int64) error {
	ledgers, err := models.LedgerEntries.Query(sm.Where(psql.And(
		models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(purchase.HouseholdID)),
		models.LedgerEntries.Columns.SourcePurchaseID.EQ(psql.Arg(purchase.ID)),
	))).All(ctx, executor)
	if err != nil {
		return err
	}
	for _, ledger := range ledgers {
		if _, err := models.LedgerEntries.Update((&models.LedgerEntrySetter{
			OccurredAt: &purchase.OccurredAt, Description: &purchase.Merchant, AmountYen: &total,
		}).UpdateMod(), um.Where(models.LedgerEntries.Columns.ID.EQ(psql.Arg(ledger.ID)))).Exec(ctx, executor); err != nil {
			return err
		}
	}
	return nil
}

func purchaseItemsTotal(ctx context.Context, executor bob.Executor, purchaseID gofrsuuid.UUID) (int64, error) {
	items, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseID)))).All(ctx, executor)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, item := range items {
		var ok bool
		total, ok = checkedAdd(total, item.AmountYen)
		if !ok {
			return 0, fmt.Errorf("%w: purchase total is too large", ErrBadInput)
		}
	}
	return total, nil
}

func validDatePrecision(value string) bool {
	return value == "DATE_TIME" || value == "DATE_ONLY"
}

func requirePurchaseMember(ctx context.Context, executor bob.Executor, householdID, actorID gofrsuuid.UUID) error {
	member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(actorID)),
		models.HouseholdMembers.Columns.LeftAt.IsNull(),
	))).Exists(ctx, executor)
	if err != nil {
		return err
	}
	if !member {
		return ErrNotFound
	}
	return nil
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
	return accounting.RecordEvent(ctx, executor, householdID, purchaseID, "purchase.updated", revision, fields, actorID)
}
