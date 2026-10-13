package graph

import (
	"context"
	"fmt"
	"slices"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func lockAccountingPeriods(ctx context.Context, tx pgx.Tx, householdID string, periods ...string) error {
	slices.Sort(periods)
	periods = slices.Compact(periods)
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return fmt.Errorf("%w: invalid household id", ErrBadInput)
	}
	executor := bobpgx.NewTx(tx, nil)
	for _, period := range periods {
		if err := ensureAccountingPeriod(ctx, executor, householdUUID, period); err != nil {
			return err
		}
		row, err := models.AccountingPeriods.Query(sm.Where(psql.And(
			models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			models.AccountingPeriods.Columns.Period.EQ(psql.Arg(period)),
		)), sm.ForUpdate()).One(ctx, executor)
		if err != nil {
			return err
		}
		if row.Status == statusClosed {
			return fmt.Errorf("%w: accounting period is closed", ErrConflict)
		}
	}
	return nil
}
func ensureAccountingPeriod(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, period string) error {
	status := "OPEN"
	revision := int64(0)
	_, err := models.AccountingPeriods.Insert(&models.AccountingPeriodSetter{
		HouseholdID: &householdID, Period: &period, Status: &status, Revision: &revision,
	}, im.OnConflict("household_id", "period").DoNothing()).Exec(ctx, executor)
	return err
}
func syncPostedPurchaseAccounting(ctx context.Context, tx pgx.Tx, purchaseID string) error {
	purchaseUUID, err := gofrsuuid.FromString(purchaseID)
	if err != nil {
		return err
	}
	executor := bobpgx.NewTx(tx, nil)
	purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseUUID)))).One(ctx, executor)
	if err != nil {
		return err
	}
	householdID := purchase.HouseholdID.String()
	occurredAt, merchant := purchase.OccurredAt, purchase.Merchant
	var paymentAccountID *string
	if purchase.PaymentAccountID.Valid {
		id := purchase.PaymentAccountID.V.String()
		paymentAccountID = &id
	}
	journal, err := models.Journals.Query(sm.Where(psql.And(
		models.Journals.Columns.HouseholdID.EQ(psql.Arg(purchase.HouseholdID)),
		models.Journals.Columns.SourceKind.EQ(psql.Arg(sourceKindPurchase)),
		models.Journals.Columns.SourceID.EQ(psql.Arg(purchaseUUID)),
	))).One(ctx, executor)
	if err != nil {
		return err
	}
	items, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, executor)
	if err != nil {
		return err
	}
	var total int64
	for _, item := range items {
		var ok bool
		total, ok = checkedAdd(total, item.AmountYen)
		if !ok {
			return fmt.Errorf("%w: purchase total is too large", ErrBadInput)
		}
	}
	if total <= 0 {
		return fmt.Errorf("%w: purchase total must be greater than zero", ErrBadInput)
	}
	if _, err := models.Journals.Update((&models.JournalSetter{OccurredAt: &occurredAt}).UpdateMod(), um.Where(models.Journals.Columns.ID.EQ(psql.Arg(journal.ID)))).Exec(ctx, executor); err != nil {
		return err
	}
	if _, err := models.JournalEntries.Delete(dm.Where(models.JournalEntries.Columns.JournalID.EQ(psql.Arg(journal.ID)))).Exec(ctx, executor); err != nil {
		return err
	}
	allocations, err := models.ItemAllocations.Query(sm.Where(models.ItemAllocations.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, executor)
	if err != nil {
		return err
	}
	if err := refreshPurchaseMemberBurden(ctx, executor, householdID, purchaseUUID, allocations); err != nil {
		return err
	}
	purchaseAllocations := make([]accounting.PurchaseAllocation, 0, len(allocations))
	for _, allocation := range allocations {
		purchaseAllocations = append(purchaseAllocations, accounting.PurchaseAllocation{PoolID: allocation.PoolID, AmountYen: allocation.AmountYen})
	}
	payments, err := purchasePayments(ctx, tx, householdID, purchaseID, paymentAccountID, total)
	if err != nil {
		return err
	}
	purchasePayments := make([]accounting.PurchasePayment, 0, len(payments))
	for _, payment := range payments {
		accountUUID, parseErr := gofrsuuid.FromString(payment.PaymentAccountID)
		if parseErr != nil {
			return parseErr
		}
		purchasePayments = append(purchasePayments, accounting.PurchasePayment{AccountID: accountUUID, AmountYen: int64(payment.AmountYen)})
	}
	entries, err := accounting.BuildPurchaseEntries(purchaseAllocations, purchasePayments, total)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	if err := accounting.InsertEntries(ctx, executor, journal.ID, entries); err != nil {
		return err
	}
	ledgers, err := models.LedgerEntries.Query(sm.Where(psql.And(
		models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(purchase.HouseholdID)),
		models.LedgerEntries.Columns.SourcePurchaseID.EQ(psql.Arg(purchaseUUID)),
	))).All(ctx, executor)
	if err != nil {
		return err
	}
	for _, ledger := range ledgers {
		if _, err := models.LedgerEntries.Update((&models.LedgerEntrySetter{
			OccurredAt: &occurredAt, Description: &merchant, AmountYen: &total,
		}).UpdateMod(), um.Where(models.LedgerEntries.Columns.ID.EQ(psql.Arg(ledger.ID)))).Exec(ctx, executor); err != nil {
			return err
		}
	}
	return nil
}
