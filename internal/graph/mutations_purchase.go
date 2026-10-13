package graph

import (
	"context"
	"database/sql"
	stdjson "encoding/json"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func validatePurchasePaymentInputs(ctx context.Context, tx pgx.Tx, householdID string, inputs []*model.PurchasePaymentInput) ([]model.PurchasePaymentInput, error) {
	result := make([]model.PurchasePaymentInput, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		if input == nil || input.PaymentAccountID == "" || input.AmountYen <= 0 || int64(input.AmountYen) > maxMoney {
			return nil, fmt.Errorf("%w: payment allocations require an account and positive amount", ErrBadInput)
		}
		if _, exists := seen[input.PaymentAccountID]; exists {
			return nil, fmt.Errorf("%w: payment account may appear only once", ErrBadInput)
		}
		seen[input.PaymentAccountID] = struct{}{}
		householdUUID, parseErr := gofrsuuid.FromString(householdID)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: invalid household id", ErrBadInput)
		}
		accountUUID, parseErr := gofrsuuid.FromString(input.PaymentAccountID)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: invalid payment account id", ErrBadInput)
		}
		_, err := models.Accounts.Query(sm.Where(psql.And(
			models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			models.Accounts.Columns.ID.EQ(psql.Arg(accountUUID)),
		))).One(ctx, bobpgx.NewTx(tx, nil))
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		result = append(result, *input)
	}
	return result, nil
}

func replacePurchasePaymentRows(ctx context.Context, tx pgx.Tx, householdID, purchaseID string, payments []model.PurchasePaymentInput) error {
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return err
	}
	purchaseUUID, err := gofrsuuid.FromString(purchaseID)
	if err != nil {
		return err
	}
	executor := bobpgx.NewTx(tx, nil)
	purchase, err := models.Purchases.Query(sm.Where(psql.And(
		models.Purchases.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.Purchases.Columns.ID.EQ(psql.Arg(purchaseUUID)),
	))).One(ctx, executor)
	if err != nil {
		return err
	}
	var oldSnapshots map[gofrsuuid.UUID][]*models.PurchasePaymentFundingSnapshot
	var oldSnapshotsByPool map[gofrsuuid.UUID][]*models.PurchasePaymentFundingSnapshot
	var oldAccounts map[gofrsuuid.UUID]struct{}
	if purchase.Status == string(model.PurchaseStatusPosted) {
		oldPayments, err := models.PurchasePayments.Query(sm.Where(psql.And(
			models.PurchasePayments.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			models.PurchasePayments.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)),
		))).All(ctx, executor)
		if err != nil {
			return err
		}
		accountByLine := make(map[int32]gofrsuuid.UUID, len(oldPayments))
		oldAccounts = make(map[gofrsuuid.UUID]struct{}, len(oldPayments))
		for _, oldPayment := range oldPayments {
			accountByLine[oldPayment.LineNo] = oldPayment.AccountID
			oldAccounts[oldPayment.AccountID] = struct{}{}
		}
		oldSnapshots = make(map[gofrsuuid.UUID][]*models.PurchasePaymentFundingSnapshot, len(oldPayments))
		oldSnapshotsByPool = make(map[gofrsuuid.UUID][]*models.PurchasePaymentFundingSnapshot)
		oldSnapshotsByLinePool := make(map[purchasePaymentSnapshotKey][]*models.PurchasePaymentFundingSnapshot)
		poolByLine := make(map[int32]gofrsuuid.UUID)
		snapshots, err := models.PurchasePaymentFundingSnapshots.Query(sm.Where(psql.And(
			models.PurchasePaymentFundingSnapshots.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			models.PurchasePaymentFundingSnapshots.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)),
		))).All(ctx, executor)
		if err != nil {
			return err
		}
		for _, snapshot := range snapshots {
			accountID, ok := accountByLine[snapshot.PaymentLineNo]
			if !ok {
				return fmt.Errorf("%w: purchase %s payment line %d has no payment", ErrMissingPurchaseFundingSnapshot, purchaseID, snapshot.PaymentLineNo)
			}
			oldSnapshots[accountID] = append(oldSnapshots[accountID], snapshot)
			if poolID, found := poolByLine[snapshot.PaymentLineNo]; found && poolID != snapshot.PoolID {
				return fmt.Errorf("%w: purchase %s payment line %d has multiple pools", ErrMissingPurchaseFundingSnapshot, purchaseID, snapshot.PaymentLineNo)
			}
			poolByLine[snapshot.PaymentLineNo] = snapshot.PoolID
			key := purchasePaymentSnapshotKey{lineNo: snapshot.PaymentLineNo, poolID: snapshot.PoolID}
			oldSnapshotsByLinePool[key] = append(oldSnapshotsByLinePool[key], snapshot)
		}
		for key, snapshotSet := range oldSnapshotsByLinePool {
			if previousSet, found := oldSnapshotsByPool[key.poolID]; found {
				if !equalPurchasePaymentSnapshotSets(previousSet, snapshotSet) {
					return fmt.Errorf("%w: purchase %s has inconsistent snapshots for pool %s", ErrMissingPurchaseFundingSnapshot, purchaseID, key.poolID)
				}
				continue
			}
			oldSnapshotsByPool[key.poolID] = snapshotSet
		}
	}
	if _, err := models.PurchasePayments.Delete(dm.Where(psql.And(
		models.PurchasePayments.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.PurchasePayments.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)),
	))).Exec(ctx, executor); err != nil {
		return err
	}
	for index, payment := range payments {
		accountUUID, err := gofrsuuid.FromString(payment.PaymentAccountID)
		if err != nil {
			return err
		}
		lineNo, amount := int32(index+1), int64(payment.AmountYen)
		if _, err := models.PurchasePayments.Insert(&models.PurchasePaymentSetter{
			HouseholdID: &householdUUID, PurchaseID: &purchaseUUID, LineNo: &lineNo, AccountID: &accountUUID, AmountYen: &amount,
		}).Exec(ctx, executor); err != nil {
			return err
		}
		if purchase.Status != string(model.PurchaseStatusPosted) {
			continue
		}
		account, err := models.Accounts.Query(sm.Where(psql.And(
			models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			models.Accounts.Columns.ID.EQ(psql.Arg(accountUUID)),
		))).One(ctx, executor)
		if err != nil {
			return err
		}
		if !account.PoolID.Valid {
			return fmt.Errorf("%w: purchase %s payment account %s has no pool", ErrUnattributedPurchasePayment, purchaseID, accountUUID)
		}
		existing := snapshotsForPool(oldSnapshots[accountUUID], account.PoolID.V)
		if len(existing) == 0 {
			existing = oldSnapshotsByPool[account.PoolID.V]
		}
		if len(existing) > 0 {
			for _, snapshot := range existing {
				paymentLineNo, poolID, memberID := lineNo, snapshot.PoolID, snapshot.MemberID
				version, weight, settlement, source := snapshot.FundingRuleVersion, snapshot.FundingWeight, snapshot.SettlementPolicy, snapshot.SnapshotSource
				if _, err := models.PurchasePaymentFundingSnapshots.Insert(&models.PurchasePaymentFundingSnapshotSetter{
					HouseholdID: &householdUUID, PurchaseID: &purchaseUUID, PaymentLineNo: &paymentLineNo,
					PoolID: &poolID, MemberID: &memberID, FundingRuleVersion: &version, FundingWeight: &weight,
					SettlementPolicy: &settlement, SnapshotSource: &source,
				}).Exec(ctx, executor); err != nil {
					return err
				}
			}
			continue
		}
		if _, wasPaidByAccount := oldAccounts[accountUUID]; wasPaidByAccount && len(oldSnapshots[accountUUID]) == 0 && len(oldSnapshotsByPool[account.PoolID.V]) == 0 {
			return fmt.Errorf("%w: purchase %s payment account %s has no existing snapshot", ErrMissingPurchaseFundingSnapshot, purchaseID, accountUUID)
		}
		fundingRule, err := currentPurchaseFundingRule(ctx, executor, householdUUID, account.PoolID.V)
		if err != nil {
			return fmt.Errorf("pool %s: %w", account.PoolID.V, err)
		}
		for _, fundingWeight := range fundingRule.weights {
			memberID, err := gofrsuuid.FromString(fundingWeight.PoolID)
			if err != nil {
				return fmt.Errorf("parse funding member id: %w", err)
			}
			paymentLineNo, poolID := lineNo, account.PoolID.V
			weight, version, source := fundingWeight.Value, fundingRule.version, "POSTED"
			if _, err := models.PurchasePaymentFundingSnapshots.Insert(&models.PurchasePaymentFundingSnapshotSetter{
				HouseholdID: &householdUUID, PurchaseID: &purchaseUUID, PaymentLineNo: &paymentLineNo,
				PoolID: &poolID, MemberID: &memberID, FundingRuleVersion: &version, FundingWeight: &weight,
				SettlementPolicy: &fundingRule.settlementPolicy, SnapshotSource: &source,
			}).Exec(ctx, executor); err != nil {
				return err
			}
		}
	}
	return nil
}

type purchasePaymentSnapshotKey struct {
	lineNo int32
	poolID gofrsuuid.UUID
}

func snapshotsForPool(snapshots []*models.PurchasePaymentFundingSnapshot, poolID gofrsuuid.UUID) []*models.PurchasePaymentFundingSnapshot {
	result := make([]*models.PurchasePaymentFundingSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.PoolID == poolID {
			result = append(result, snapshot)
		}
	}
	return result
}

func equalPurchasePaymentSnapshotSets(left, right []*models.PurchasePaymentFundingSnapshot) bool {
	if len(left) != len(right) || len(left) == 0 {
		return false
	}
	byMember := make(map[gofrsuuid.UUID]*models.PurchasePaymentFundingSnapshot, len(left))
	for _, snapshot := range left {
		byMember[snapshot.MemberID] = snapshot
	}
	for _, snapshot := range right {
		previous, found := byMember[snapshot.MemberID]
		if !found || previous.PoolID != snapshot.PoolID || previous.FundingRuleVersion != snapshot.FundingRuleVersion ||
			previous.FundingWeight != snapshot.FundingWeight || previous.SettlementPolicy != snapshot.SettlementPolicy ||
			previous.SnapshotSource != snapshot.SnapshotSource {
			return false
		}
	}
	return true
}

func purchasePayments(ctx context.Context, tx pgx.Tx, householdID, purchaseID string, legacyAccountID *string, total int64) ([]model.PurchasePaymentInput, error) {
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, err
	}
	purchaseUUID, err := gofrsuuid.FromString(purchaseID)
	if err != nil {
		return nil, err
	}
	rows, err := models.PurchasePayments.Query(sm.Where(psql.And(
		models.PurchasePayments.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.PurchasePayments.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)),
	)), sm.OrderBy(models.PurchasePayments.Columns.LineNo)).All(ctx, bobpgx.NewTx(tx, nil))
	if err != nil {
		return nil, err
	}
	payments := make([]model.PurchasePaymentInput, 0, len(rows))
	var paid int64
	for _, row := range rows {
		payment := model.PurchasePaymentInput{PaymentAccountID: row.AccountID.String(), AmountYen: graphLong(row.AmountYen)}
		var ok bool
		paid, ok = checkedAdd(paid, row.AmountYen)
		if !ok {
			return nil, fmt.Errorf("%w: payment total is too large", ErrBadInput)
		}
		payments = append(payments, payment)
	}
	if len(payments) == 0 && legacyAccountID != nil {
		payments = append(payments, model.PurchasePaymentInput{PaymentAccountID: *legacyAccountID, AmountYen: graphLong(total)})
		paid = total
	}
	if len(payments) > 0 && paid != total {
		return nil, fmt.Errorf("%w: payment allocations must equal purchase total", ErrBadInput)
	}
	return payments, nil
}

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
		if row.Status == "CLOSED" {
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
		models.Journals.Columns.SourceKind.EQ(psql.Arg("PURCHASE")),
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

func (r *mutationResolver) updatePurchase(ctx context.Context, input model.UpdatePurchaseInput) (*model.Purchase, error) {
	changedFields := purchaseChangedFieldKeys(input.ChangedFields)
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	purchaseUUID, err := gofrsuuid.FromString(input.PurchaseID)
	if err != nil {
		return nil, ErrNotFound
	}
	executor := bobpgx.NewTx(tx, nil)
	observedPurchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseUUID)))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	observedHouseholdID := observedPurchase.HouseholdID.String()
	observedOccurredAt := observedPurchase.OccurredAt
	if _, err := requireMember(ctx, r.DB, observedHouseholdID); err != nil {
		return nil, err
	}
	targetOccurredAt := observedOccurredAt
	if input.Patch != nil && input.Patch.OccurredAt != nil {
		targetOccurredAt, err = parseTime(*input.Patch.OccurredAt)
		if err != nil {
			return nil, err
		}
	}
	if err := lockAccountingPeriods(ctx, tx, observedHouseholdID, accountingPeriod(observedOccurredAt), accountingPeriod(targetOccurredAt)); err != nil {
		return nil, err
	}
	purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseUUID))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	householdID := purchase.HouseholdID.String()
	status := purchase.Status
	if householdID != observedHouseholdID || !purchase.OccurredAt.Equal(observedOccurredAt) {
		return nil, ErrConflict
	}
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(purchase.HouseholdID))), sm.ForShare()).One(ctx, executor); err != nil {
		return nil, err
	}
	if status != string(model.PurchaseStatusDraft) && status != string(model.PurchaseStatusPosted) {
		return nil, fmt.Errorf("%w: closed purchases require a correction", ErrConflict)
	}
	if input.BaseRevision > int(purchase.Revision) {
		return nil, ErrConflict
	}
	if err := validateChangedFields(changedFields, "occurredAt", "merchant", "paymentAllocations"); err != nil {
		return nil, err
	}
	for _, field := range changedFields {
		switch field {
		case "occurredAt":
			if input.Patch == nil || input.Patch.OccurredAt == nil {
				return nil, fmt.Errorf("%w: changed field has no value", ErrBadInput)
			}
		case "merchant":
			if input.Patch == nil || input.Patch.Merchant == nil {
				return nil, fmt.Errorf("%w: changed field has no value", ErrBadInput)
			}
		case "paymentAllocations":
			if input.Patch == nil || input.Patch.PaymentAllocations == nil {
				return nil, fmt.Errorf("%w: changed field has no value", ErrBadInput)
			}
		}
	}
	if input.Patch != nil && (input.Patch.OccurredAt != nil && !slices.Contains(changedFields, "occurredAt") || input.Patch.Merchant != nil && !slices.Contains(changedFields, "merchant") || input.Patch.PaymentAllocations != nil && !slices.Contains(changedFields, "paymentAllocations")) {
		return nil, fmt.Errorf("%w: patch contains fields missing from changedFields", ErrBadInput)
	}
	conflict, err := valuesConflict(purchase.FieldRevisions.Val, input.BaseRevision, changedFields)
	if err != nil {
		return nil, err
	}
	if conflict {
		return nil, ErrConflict
	}
	newRevision := purchase.Revision + 1
	updatedFields, err := setFieldRevisions(purchase.FieldRevisions.Val, changedFields, newRevision)
	if err != nil {
		return nil, err
	}
	updatePayments := slices.Contains(changedFields, "paymentAllocations")
	paymentAccountID := purchase.PaymentAccountID
	if updatePayments {
		paymentAccountID = sql.Null[gofrsuuid.UUID]{}
	}
	if input.Patch != nil {
		if slices.Contains(changedFields, "occurredAt") {
			occurredAt, err := parseTime(*input.Patch.OccurredAt)
			if err != nil {
				return nil, err
			}
			purchase.OccurredAt = occurredAt
		}
		if slices.Contains(changedFields, "merchant") {
			value := strings.TrimSpace(*input.Patch.Merchant)
			if len(value) > 240 {
				return nil, fmt.Errorf("%w: merchant name is too long", ErrBadInput)
			}
			purchase.Merchant = value
		}
		if updatePayments {
			payments, err := validatePurchasePaymentInputs(ctx, tx, householdID, input.Patch.PaymentAllocations)
			if err != nil {
				return nil, err
			}
			items, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, executor)
			if err != nil {
				return nil, err
			}
			var total int64
			for _, item := range items {
				var ok bool
				total, ok = checkedAdd(total, item.AmountYen)
				if !ok {
					return nil, fmt.Errorf("%w: purchase total is too large", ErrBadInput)
				}
			}
			if status == string(model.PurchaseStatusPosted) {
				var paid int64
				for _, payment := range payments {
					var ok bool
					paid, ok = checkedAdd(paid, int64(payment.AmountYen))
					if !ok {
						return nil, fmt.Errorf("%w: payment total is too large", ErrBadInput)
					}
				}
				if paid != total {
					return nil, fmt.Errorf("%w: payment allocations must equal purchase total", ErrBadInput)
				}
			}
			if err := replacePurchasePaymentRows(ctx, tx, householdID, input.PurchaseID, payments); err != nil {
				return nil, err
			}
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
		return nil, err
	}
	if status == string(model.PurchaseStatusPosted) {
		if err := syncPostedPurchaseAccounting(ctx, tx, input.PurchaseID); err != nil {
			return nil, err
		}
	}
	if err := writeEvent(ctx, tx, householdID, "purchase.updated", input.PurchaseID, newRevision, changedFields, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadPurchase(ctx, r.DB, input.PurchaseID)
}

func (r *mutationResolver) updatePurchaseItem(ctx context.Context, input model.UpdatePurchaseItemInput) (*model.PurchaseItem, error) {
	changedFields := purchaseItemChangedFieldKeys(input.ChangedFields)
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	itemUUID, err := gofrsuuid.FromString(input.ItemID)
	if err != nil {
		return nil, ErrNotFound
	}
	executor := bobpgx.NewTx(tx, nil)
	observedItem, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemUUID)))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := observedItem.LoadPurchase(ctx, executor); err != nil {
		return nil, err
	}
	if observedItem.R.Purchase == nil {
		return nil, ErrNotFound
	}
	observedPurchase := observedItem.R.Purchase
	householdID := observedItem.HouseholdID.String()
	purchaseID := observedItem.PurchaseID.String()
	observedOccurredAt := observedPurchase.OccurredAt
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	household, err := models.Households.Query(
		sm.Where(models.Households.Columns.ID.EQ(psql.Arg(observedItem.HouseholdID))),
		sm.ForShare(),
	).One(ctx, executor)
	if err != nil {
		return nil, err
	}
	if err := lockAccountingPeriods(ctx, tx, householdID, accountingPeriod(observedOccurredAt)); err != nil {
		return nil, err
	}
	purchaseUUID := observedItem.PurchaseID
	purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseUUID))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	item, err := models.PurchaseItems.Query(sm.Where(psql.And(
		models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemUUID)),
		models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)),
	)), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !purchase.OccurredAt.Equal(observedOccurredAt) {
		return nil, ErrConflict
	}
	status := purchase.Status
	if status != string(model.PurchaseStatusDraft) && status != string(model.PurchaseStatusPosted) {
		return nil, fmt.Errorf("%w: closed purchases require a correction", ErrConflict)
	}
	name, amount := item.Name, item.AmountYen
	var categoryID *string
	if item.CategoryID.Valid {
		category := item.CategoryID.V.String()
		categoryID = &category
	}
	var productVariantID *string
	if item.ProductVariantID.Valid {
		variant := item.ProductVariantID.V.String()
		productVariantID = &variant
	}
	allocationOverride := item.AllocationOverride
	revision := item.Revision
	fieldRevisions := item.FieldRevisions.Val
	merchant := purchase.Merchant
	ruleVersion := household.DefaultAllocationVersion
	if input.BaseRevision > int(item.Revision) {
		return nil, ErrConflict
	}
	if err := validateChangedFields(changedFields, "name", "amountYen", "quantity", "printedUnitPrice", "categoryId", "productVariantId", "allocations"); err != nil {
		return nil, err
	}
	for _, field := range changedFields {
		switch field {
		case "name":
			if input.Name == nil {
				return nil, fmt.Errorf("%w: changed field has no value", ErrBadInput)
			}
		case "amountYen":
			if input.AmountYen == nil {
				return nil, fmt.Errorf("%w: changed field has no value", ErrBadInput)
			}
		case "allocations":
			if input.Allocations == nil {
				return nil, fmt.Errorf("%w: changed field has no value", ErrBadInput)
			}
		}
	}
	if input.Name != nil && !slices.Contains(changedFields, "name") || input.AmountYen != nil && !slices.Contains(changedFields, "amountYen") ||
		input.Quantity.IsSet() && !slices.Contains(changedFields, "quantity") ||
		input.PrintedUnitPrice.IsSet() && !slices.Contains(changedFields, "printedUnitPrice") ||
		input.CategoryID != nil && !slices.Contains(changedFields, "categoryId") || input.ProductVariantID != nil && !slices.Contains(changedFields, "productVariantId") || input.Allocations != nil && !slices.Contains(changedFields, "allocations") {
		return nil, fmt.Errorf("%w: patch contains fields missing from changedFields", ErrBadInput)
	}
	if input.ProductLearning != nil && !slices.Contains(changedFields, "productVariantId") {
		return nil, fmt.Errorf("%w: product learning requires a product variant change", ErrBadInput)
	}
	conflict, err := valuesConflict(item.FieldRevisions.Val, input.BaseRevision, changedFields)
	if err != nil {
		return nil, err
	}
	if conflict {
		return nil, ErrConflict
	}
	currentMeasure := purchaseItemMeasure{
		quantityNumerator: item.QuantityNumerator, quantityDenominator: item.QuantityDenominator, quantityUnit: item.QuantityUnit,
		printedUnitPriceYen: item.PrintedUnitPriceYen, printedUnitPriceQuantityNumerator: item.PrintedUnitPriceQuantityNumerator,
		printedUnitPriceQuantityDenominator: item.PrintedUnitPriceQuantityDenominator, printedUnitPriceUnit: item.PrintedUnitPriceUnit,
	}
	newMeasure, err := measureFromUpdate(input, currentMeasure, changedFields)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	newName, newAmount, newCategory := name, amount, categoryID
	if input.Name != nil {
		newName = strings.TrimSpace(*input.Name)
		if newName == "" || len(newName) > 240 {
			return nil, fmt.Errorf("%w: invalid item name", ErrBadInput)
		}
	}
	if input.AmountYen != nil {
		newAmount = int64(*input.AmountYen)
		if newAmount < 0 || newAmount > maxMoney {
			return nil, fmt.Errorf("%w: invalid item amount", ErrBadInput)
		}
	}
	updateCategoryID := slices.Contains(changedFields, "categoryId")
	if updateCategoryID {
		newCategory = input.CategoryID
	}
	updateProductVariantID := slices.Contains(changedFields, "productVariantId")
	newProductVariantID := productVariantID
	if updateProductVariantID {
		newProductVariantID = input.ProductVariantID
	}
	learningScope := model.ProductLearningScopeItemOnly
	if input.ProductLearning != nil {
		learningScope = *input.ProductLearning
	}
	if !learningScope.IsValid() || input.VariantName != nil && learningScope != model.ProductLearningScopeVariantName {
		return nil, fmt.Errorf("%w: invalid product learning scope", ErrBadInput)
	}
	if updateProductVariantID && newProductVariantID != nil {
		variantUUID, err := gofrsuuid.FromString(*newProductVariantID)
		if err != nil {
			return nil, ErrNotFound
		}
		variant, err := models.ProductVariants.Query(sm.Where(psql.And(
			models.ProductVariants.Columns.ID.EQ(psql.Arg(variantUUID)),
			models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(observedItem.HouseholdID)),
			models.ProductVariants.Columns.Archived.EQ(psql.Arg(false)),
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if err := variant.LoadProductConcept(ctx, executor); err != nil {
			return nil, err
		}
		if variant.R.ProductConcept == nil || variant.R.ProductConcept.Archived {
			return nil, ErrNotFound
		}
	}
	if learningScope != model.ProductLearningScopeItemOnly && newProductVariantID == nil {
		return nil, fmt.Errorf("%w: product learning requires a linked variant", ErrBadInput)
	}
	var weights []domain.Weight
	var appliedRuleID *string
	var appliedRuleVersion = ruleVersion
	newAllocationOverride := allocationOverride
	if input.Allocations != nil {
		weights, err = allocationWeights(input.Allocations)
		newAllocationOverride = true
	} else if (updateCategoryID || updateProductVariantID) && !allocationOverride {
		weights, appliedRuleID, appliedRuleVersion, err = allocationRuleWeights(ctx, tx, householdID, newCategory, newProductVariantID, normalizeMerchant(merchant))
		if err == nil && appliedRuleID == nil {
			weights, err = loadWeights(ctx, tx, householdID, ruleVersion)
		}
	} else {
		allocations, queryErr := models.ItemAllocations.Query(
			sm.Where(models.ItemAllocations.Columns.ItemID.EQ(psql.Arg(itemUUID))),
			sm.OrderBy(models.ItemAllocations.Columns.PoolID),
		).All(ctx, executor)
		if queryErr != nil {
			return nil, queryErr
		}
		for _, allocation := range allocations {
			weight := domain.Weight{PoolID: allocation.PoolID.String(), Value: allocation.Weight}
			if len(weights) == 0 {
				if allocation.RuleID.Valid {
					ruleID := allocation.RuleID.V.String()
					appliedRuleID = &ruleID
				}
				appliedRuleVersion = allocation.RuleVersion
			}
			weights = append(weights, weight)
		}
		if len(weights) == 0 {
			weights, err = loadWeights(ctx, tx, householdID, ruleVersion)
		}
	}
	if err != nil {
		return nil, err
	}
	allocated, err := domain.Allocate(newAmount, weights)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	var adjustedPayments []model.PurchasePaymentInput
	updatePayments := false
	if newAmount != item.AmountYen {
		purchaseItems, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, executor)
		if err != nil {
			return nil, err
		}
		var currentTotal int64
		for _, purchaseItem := range purchaseItems {
			var ok bool
			currentTotal, ok = checkedAdd(currentTotal, purchaseItem.AmountYen)
			if !ok || currentTotal > maxMoney {
				return nil, fmt.Errorf("%w: purchase total is too large", ErrBadInput)
			}
		}
		remainingTotal := currentTotal - item.AmountYen
		newTotal, ok := checkedAdd(remainingTotal, newAmount)
		if !ok || newTotal > maxMoney {
			return nil, fmt.Errorf("%w: purchase total is too large", ErrBadInput)
		}
		var legacyAccountID *string
		if purchase.PaymentAccountID.Valid {
			id := purchase.PaymentAccountID.V.String()
			legacyAccountID = &id
		}
		payments, err := purchasePayments(ctx, tx, householdID, purchaseID, legacyAccountID, currentTotal)
		if err != nil {
			return nil, err
		}
		if len(payments) > 0 {
			paymentWeights := make([]domain.Weight, 0, len(payments))
			for _, payment := range payments {
				paymentWeights = append(paymentWeights, domain.Weight{PoolID: payment.PaymentAccountID, Value: int64(payment.AmountYen)})
			}
			paymentAmounts, err := domain.Allocate(newTotal, paymentWeights)
			if err != nil {
				return nil, fmt.Errorf("%w: %w", ErrBadInput, err)
			}
			paymentAmountByAccount := make(map[string]int64, len(paymentAmounts))
			for _, paymentAmount := range paymentAmounts {
				paymentAmountByAccount[paymentAmount.PoolID] = paymentAmount.Amount
			}
			adjustedPayments = make([]model.PurchasePaymentInput, 0, len(payments))
			for _, payment := range payments {
				if amount := paymentAmountByAccount[payment.PaymentAccountID]; amount > 0 {
					adjustedPayments = append(adjustedPayments, model.PurchasePaymentInput{PaymentAccountID: payment.PaymentAccountID, AmountYen: graphLong(amount)})
				}
			}
			updatePayments = true
		}
	}
	newRevision := revision + 1
	updatedFields, err := setFieldRevisions(fieldRevisions, changedFields, newRevision)
	if err != nil {
		return nil, err
	}
	if learningScope == model.ProductLearningScopeVariantName {
		variantName := strings.TrimSpace(trimOptional(input.VariantName))
		variantKey := normalizeProductText(variantName)
		if variantKey == "" || len(variantName) > 240 {
			return nil, fmt.Errorf("%w: invalid normalized product name", ErrBadInput)
		}
		variantUUID, err := gofrsuuid.FromString(*newProductVariantID)
		if err != nil {
			return nil, ErrNotFound
		}
		householdUUID := observedItem.HouseholdID
		if _, err := models.ProductVariants.Update((&models.ProductVariantSetter{
			Name: &variantName, NormalizedName: &variantKey,
		}).UpdateMod(),
			um.Where(psql.And(
				models.ProductVariants.Columns.ID.EQ(psql.Arg(variantUUID)),
				models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			)),
			um.SetCol("revision").To(models.ProductVariants.Columns.Revision.Plus(psql.Arg(int64(1)))),
			um.SetCol("updated_at").To(psql.F("now")),
		).Exec(ctx, executor); err != nil {
			return nil, err
		}
		variant, err := models.ProductVariants.Query(sm.Where(models.ProductVariants.Columns.ID.EQ(psql.Arg(variantUUID)))).One(ctx, executor)
		if err != nil {
			return nil, err
		}
		if err := writeEvent(ctx, tx, householdID, "product.variant.renamed", *newProductVariantID, variant.Revision, []string{"name"}, user.MemberID); err != nil {
			return nil, err
		}
	}
	itemName, itemAmount := newName, newAmount
	categoryValue := sql.Null[gofrsuuid.UUID]{}
	if newCategory != nil {
		categoryUUID, err := gofrsuuid.FromString(*newCategory)
		if err != nil {
			return nil, ErrNotFound
		}
		categoryValue = sql.Null[gofrsuuid.UUID]{V: categoryUUID, Valid: true}
	}
	variantValue := sql.Null[gofrsuuid.UUID]{}
	if newProductVariantID != nil {
		variantUUID, err := gofrsuuid.FromString(*newProductVariantID)
		if err != nil {
			return nil, ErrNotFound
		}
		variantValue = sql.Null[gofrsuuid.UUID]{V: variantUUID, Valid: true}
	}
	fieldRevisionValue := types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(updatedFields)}
	if _, err := models.PurchaseItems.Update((&models.PurchaseItemSetter{
		Name: &itemName, AmountYen: &itemAmount, CategoryID: &categoryValue,
		ProductVariantID: &variantValue, AllocationOverride: &newAllocationOverride,
		Revision: &newRevision, FieldRevisions: &fieldRevisionValue,
		QuantityNumerator: &newMeasure.quantityNumerator, QuantityDenominator: &newMeasure.quantityDenominator, QuantityUnit: &newMeasure.quantityUnit,
		PrintedUnitPriceYen: &newMeasure.printedUnitPriceYen, PrintedUnitPriceQuantityNumerator: &newMeasure.printedUnitPriceQuantityNumerator,
		PrintedUnitPriceQuantityDenominator: &newMeasure.printedUnitPriceQuantityDenominator, PrintedUnitPriceUnit: &newMeasure.printedUnitPriceUnit,
	}).UpdateMod(),
		um.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemUUID))),
		um.SetCol("updated_at").To(psql.F("now")),
	).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if updateProductVariantID && newProductVariantID != nil {
		merchantKey := normalizeMerchant(merchant)
		if learningScope == model.ProductLearningScopeMerchantAlias {
			if merchantKey == "" {
				return nil, fmt.Errorf("%w: merchant-specific learning requires a merchant", ErrBadInput)
			}
			var aliasID string
			if err := upsertProductAlias(ctx, tx, householdID, *newProductVariantID, merchantKey, name, normalizeProductText(name), &aliasID); err != nil {
				return nil, err
			}
			aliasUUID, err := gofrsuuid.FromString(aliasID)
			if err != nil {
				return nil, ErrNotFound
			}
			alias, err := models.ProductAliases.Query(sm.Where(models.ProductAliases.Columns.ID.EQ(psql.Arg(aliasUUID)))).One(ctx, executor)
			if err != nil {
				return nil, err
			}
			if err := writeEvent(ctx, tx, householdID, "product.alias.learned", aliasID, alias.Revision, []string{"alias", "variantId", "merchant"}, user.MemberID); err != nil {
				return nil, err
			}
		}
		correctionUUID := generatedUUID(newID())
		variantUUID, err := gofrsuuid.FromString(*newProductVariantID)
		if err != nil {
			return nil, ErrNotFound
		}
		actorUUID, err := gofrsuuid.FromString(user.MemberID)
		if err != nil {
			return nil, err
		}
		var previousVariant sql.Null[gofrsuuid.UUID]
		if productVariantID != nil {
			previousUUID, err := gofrsuuid.FromString(*productVariantID)
			if err != nil {
				return nil, ErrNotFound
			}
			previousVariant = sql.Null[gofrsuuid.UUID]{V: previousUUID, Valid: true}
		}
		correctionScope := string(learningScope)
		if _, err := models.ProductCorrections.Insert(&models.ProductCorrectionSetter{
			ID: &correctionUUID, HouseholdID: &observedItem.HouseholdID, PurchaseID: &purchaseUUID,
			ItemID: &itemUUID, RawName: &name, PreviousProductVariantID: &previousVariant,
			ProductVariantID: &variantUUID, LearningScope: &correctionScope, MerchantKey: &merchantKey, ActorID: &actorUUID,
		}).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	if _, err := models.ItemAllocations.Delete(dm.Where(models.ItemAllocations.Columns.ItemID.EQ(psql.Arg(itemUUID)))).Exec(ctx, executor); err != nil {
		return nil, err
	}
	for _, allocation := range allocated {
		poolUUID := generatedUUID(allocation.PoolID)
		var ruleUUID sql.Null[gofrsuuid.UUID]
		if appliedRuleID != nil {
			ruleUUID = sql.Null[gofrsuuid.UUID]{V: generatedUUID(*appliedRuleID), Valid: true}
		}
		if _, err := models.ItemAllocations.Insert(&models.ItemAllocationSetter{
			HouseholdID: &observedItem.HouseholdID, PurchaseID: &purchaseUUID, ItemID: &itemUUID,
			PoolID: &poolUUID, Weight: &allocation.Weight, AmountYen: &allocation.Amount,
			RuleVersion: &appliedRuleVersion, RuleID: &ruleUUID,
		}).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	if updatePayments {
		if err := replacePurchasePaymentRows(ctx, tx, householdID, purchaseID, adjustedPayments); err != nil {
			return nil, err
		}
	}
	if _, err := models.Purchases.Update(um.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseUUID))),
		um.SetCol("revision").To(models.Purchases.Columns.Revision.Plus(psql.Arg(int64(1)))),
		um.SetCol("updated_at").To(psql.F("now")),
	).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, householdID, "purchase.item.updated", input.ItemID, newRevision, changedFields, user.MemberID); err != nil {
		return nil, err
	}
	if status == string(model.PurchaseStatusPosted) {
		if err := syncPostedPurchaseAccounting(ctx, tx, purchaseID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	loadedPurchase, err := loadPurchase(ctx, r.DB, purchaseID)
	if err != nil {
		return nil, err
	}
	for _, item := range loadedPurchase.Items {
		if item.ID == input.ItemID {
			return item, nil
		}
	}
	return nil, ErrNotFound
}

func setFieldRevisions(current []byte, fields []string, revision int64) ([]byte, error) {
	var revisions map[string]int64
	if len(current) == 0 {
		revisions = make(map[string]int64)
	} else if err := json.Unmarshal(current, &revisions); err != nil {
		return nil, err
	}
	for _, field := range fields {
		revisions[field] = revision
	}
	return json.Marshal(revisions)
}

func purchaseChangedFieldKeys[T ~string](fields []T) []string {
	keys := make([]string, len(fields))
	for i, field := range fields {
		switch string(field) {
		case "OCCURRED_AT":
			keys[i] = "occurredAt"
		case "MERCHANT":
			keys[i] = "merchant"
		case "PAYMENT_ALLOCATIONS":
			keys[i] = "paymentAllocations"
		default:
			keys[i] = string(field)
		}
	}
	return keys
}

func purchaseItemChangedFieldKeys(fields []model.PurchaseItemChangedField) []string {
	keys := make([]string, len(fields))
	for i, field := range fields {
		switch field {
		case model.PurchaseItemChangedFieldName:
			keys[i] = "name"
		case model.PurchaseItemChangedFieldAmountYen:
			keys[i] = "amountYen"
		case model.PurchaseItemChangedFieldQuantity:
			keys[i] = "quantity"
		case model.PurchaseItemChangedFieldPrintedUnitPrice:
			keys[i] = "printedUnitPrice"
		case model.PurchaseItemChangedFieldCategoryID:
			keys[i] = "categoryId"
		case model.PurchaseItemChangedFieldProductVariantID:
			keys[i] = "productVariantId"
		case model.PurchaseItemChangedFieldAllocations:
			keys[i] = "allocations"
		default:
			keys[i] = string(field)
		}
	}
	return keys
}

func bulkPurchaseItemChangedFieldKeys(fields []model.BulkPurchaseItemChangedField) []string {
	keys := make([]string, len(fields))
	for i, field := range fields {
		switch field {
		case model.BulkPurchaseItemChangedFieldCategoryID:
			keys[i] = "categoryId"
		case model.BulkPurchaseItemChangedFieldProductVariantID:
			keys[i] = "productVariantId"
		case model.BulkPurchaseItemChangedFieldAllocations:
			keys[i] = "allocations"
		case model.BulkPurchaseItemChangedFieldTags:
			keys[i] = "tags"
		default:
			keys[i] = string(field)
		}
	}
	return keys
}

func validateChangedFields(fields []string, allowed ...string) error {
	if len(fields) == 0 {
		return fmt.Errorf("%w: changedFields is required", ErrBadInput)
	}
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if !slices.Contains(allowed, field) {
			return fmt.Errorf("%w: invalid changed field", ErrBadInput)
		}
		if _, exists := seen[field]; exists {
			return fmt.Errorf("%w: duplicate changed field", ErrBadInput)
		}
		seen[field] = struct{}{}
	}
	return nil
}

func valuesConflict(fieldRevisions []byte, base int, fields []string) (bool, error) {
	if base < 1 {
		return true, nil
	}
	var revisions map[string]int64
	if len(fieldRevisions) == 0 {
		return false, nil
	}
	if err := json.Unmarshal(fieldRevisions, &revisions); err != nil {
		return false, err
	}
	for _, field := range fields {
		if revisions[field] > int64(base) {
			return true, nil
		}
	}
	return false, nil
}
