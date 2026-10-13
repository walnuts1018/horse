package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
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
			weight, version, source := fundingWeight.Value, fundingRule.version, statusPosted
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
