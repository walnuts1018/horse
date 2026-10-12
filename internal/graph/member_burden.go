package graph

import (
	"context"
	"database/sql"
	"fmt"
	"math"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func memberWeights(rule domain.PoolFundingRule) ([]domain.Weight, error) {
	if rule.Kind == domain.FundingPoolPersonal {
		if rule.OwnerMemberID == "" {
			return nil, domain.ErrIncompleteFundingRule
		}
		return []domain.Weight{{PoolID: rule.OwnerMemberID, Value: 1}}, nil
	}
	if rule.Kind != domain.FundingPoolShared || len(rule.FundingShares) == 0 {
		return nil, domain.ErrIncompleteFundingRule
	}
	weights := make([]domain.Weight, 0, len(rule.FundingShares))
	for _, share := range rule.FundingShares {
		weights = append(weights, domain.Weight{PoolID: share.MemberID, Value: share.Weight})
	}
	return weights, nil
}

func snapshotPurchaseMemberBurden(ctx context.Context, executor bob.Executor, householdID, purchaseID string, allocations models.ItemAllocationSlice) error {
	rules, err := poolFundingRules(ctx, executor, householdID)
	if err != nil {
		return err
	}
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return fmt.Errorf("parse household id: %w", err)
	}
	purchaseUUID, err := gofrsuuid.FromString(purchaseID)
	if err != nil {
		return fmt.Errorf("parse purchase id: %w", err)
	}
	for _, allocation := range allocations {
		poolID := allocation.PoolID.String()
		rule, ok := rules[poolID]
		if !ok {
			continue
		}
		weights, err := memberWeights(rule)
		if err != nil {
			continue
		}
		if rule.Version < 1 || rule.Version > math.MaxInt32 {
			return fmt.Errorf("pool %s has an invalid funding rule version", poolID)
		}
		parts, err := domain.Allocate(allocation.AmountYen, weights)
		if err != nil {
			return err
		}
		for _, part := range parts {
			memberID, err := gofrsuuid.FromString(part.PoolID)
			if err != nil {
				return fmt.Errorf("parse member id: %w", err)
			}
			poolUUID := allocation.PoolID
			itemUUID := allocation.ItemID
			version := int32(rule.Version)
			snapshotSource := "POSTED"
			if _, err := models.PurchaseMemberBurdens.Insert(&models.PurchaseMemberBurdenSetter{
				HouseholdID: &householdUUID, PurchaseID: &purchaseUUID, ItemID: &itemUUID, PoolID: &poolUUID,
				MemberID: &memberID, AmountYen: &part.Amount, FundingRuleVersion: &version, FundingWeight: &part.Weight, SnapshotSource: &snapshotSource,
			}).Exec(ctx, executor); err != nil {
				return err
			}
		}
	}
	return nil
}

func snapshotPurchasePaymentFunding(ctx context.Context, executor bob.Executor, householdID string, purchase *models.Purchase) error {
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return fmt.Errorf("parse household id: %w", err)
	}
	type paymentSnapshot struct {
		lineNo    int32
		accountID gofrsuuid.UUID
	}
	payments, err := models.PurchasePayments.Query(sm.Where(psql.And(
		models.PurchasePayments.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.PurchasePayments.Columns.PurchaseID.EQ(psql.Arg(purchase.ID)),
	)), sm.OrderBy(models.PurchasePayments.Columns.LineNo)).All(ctx, executor)
	if err != nil {
		return err
	}
	snapshots := make([]paymentSnapshot, 0, len(payments))
	for _, payment := range payments {
		snapshots = append(snapshots, paymentSnapshot{lineNo: payment.LineNo, accountID: payment.AccountID})
	}
	if len(snapshots) == 0 {
		if !purchase.PaymentAccountID.Valid {
			return fmt.Errorf("%w: purchase %s has no payment account", ErrUnattributedPurchasePayment, purchase.ID)
		}
		items, err := models.PurchaseItems.Query(sm.Where(psql.And(
			models.PurchaseItems.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchase.ID)),
		))).All(ctx, executor)
		if err != nil {
			return err
		}
		var amount int64
		for _, item := range items {
			var ok bool
			amount, ok = checkedAdd(amount, item.AmountYen)
			if !ok {
				return fmt.Errorf("%w: purchase total is too large", ErrBadInput)
			}
		}
		lineNo := int32(1)
		accountID := purchase.PaymentAccountID.V
		if _, err := models.PurchasePayments.Insert(&models.PurchasePaymentSetter{
			HouseholdID: &householdUUID, PurchaseID: &purchase.ID, LineNo: &lineNo, AccountID: &accountID, AmountYen: &amount,
		}).Exec(ctx, executor); err != nil {
			return err
		}
		snapshots = append(snapshots, paymentSnapshot{lineNo: lineNo, accountID: accountID})
	}
	rules, err := poolFundingRules(ctx, executor, householdID)
	if err != nil {
		return err
	}
	for _, payment := range snapshots {
		account, err := models.Accounts.Query(sm.Where(psql.And(
			models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			models.Accounts.Columns.ID.EQ(psql.Arg(payment.accountID)),
		))).One(ctx, executor)
		if err != nil {
			return err
		}
		if !account.PoolID.Valid {
			continue
		}
		poolID := account.PoolID.V
		rule, ok := rules[poolID.String()]
		if !ok || rule.Version < 1 || rule.Version > math.MaxInt32 {
			continue
		}
		weights, err := memberWeights(rule)
		if err != nil {
			continue
		}
		var policy sql.Null[string]
		if rule.Kind == domain.FundingPoolShared {
			policy = sql.Null[string]{V: rule.SettlementPolicy, Valid: true}
		}
		version := int32(rule.Version)
		for _, weight := range weights {
			memberID, err := gofrsuuid.FromString(weight.PoolID)
			if err != nil {
				return fmt.Errorf("parse member id: %w", err)
			}
			lineNo := payment.lineNo
			snapshotSource := "POSTED"
			if _, err := models.PurchasePaymentFundingSnapshots.Insert(&models.PurchasePaymentFundingSnapshotSetter{
				HouseholdID: &householdUUID, PurchaseID: &purchase.ID, PaymentLineNo: &lineNo, PoolID: &poolID,
				MemberID: &memberID, FundingRuleVersion: &version, FundingWeight: &weight.Value, SettlementPolicy: &policy, SnapshotSource: &snapshotSource,
			}).Exec(ctx, executor); err != nil {
				return err
			}
		}
	}
	return nil
}

func refreshPurchaseMemberBurden(ctx context.Context, executor bob.Executor, householdID string, purchaseUUID gofrsuuid.UUID, allocations models.ItemAllocationSlice) error {
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return fmt.Errorf("parse household id: %w", err)
	}
	previous, err := models.PurchaseMemberBurdens.Query(sm.Where(psql.And(
		models.PurchaseMemberBurdens.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.PurchaseMemberBurdens.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)),
	))).All(ctx, executor)
	if err != nil {
		return err
	}
	type allocationKey struct {
		itemID gofrsuuid.UUID
		poolID gofrsuuid.UUID
	}
	weightsByAllocation := make(map[allocationKey][]domain.Weight)
	versionsByAllocation := make(map[allocationKey]int32)
	sourcesByAllocation := make(map[allocationKey]string)
	for _, row := range previous {
		key := allocationKey{itemID: row.ItemID, poolID: row.PoolID}
		weightsByAllocation[key] = append(weightsByAllocation[key], domain.Weight{PoolID: row.MemberID.String(), Value: row.FundingWeight})
		versionsByAllocation[key] = row.FundingRuleVersion
		sourcesByAllocation[key] = row.SnapshotSource
	}
	if _, err := models.PurchaseMemberBurdens.Delete(dm.Where(psql.And(
		models.PurchaseMemberBurdens.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.PurchaseMemberBurdens.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)),
	))).Exec(ctx, executor); err != nil {
		return err
	}
	for _, allocation := range allocations {
		key := allocationKey{itemID: allocation.ItemID, poolID: allocation.PoolID}
		weights := weightsByAllocation[key]
		if len(weights) == 0 {
			continue
		}
		parts, err := domain.Allocate(allocation.AmountYen, weights)
		if err != nil {
			return err
		}
		version := versionsByAllocation[key]
		snapshotSource := sourcesByAllocation[key]
		for _, part := range parts {
			memberID, err := gofrsuuid.FromString(part.PoolID)
			if err != nil {
				return fmt.Errorf("parse member id: %w", err)
			}
			itemUUID, poolUUID := allocation.ItemID, allocation.PoolID
			if _, err := models.PurchaseMemberBurdens.Insert(&models.PurchaseMemberBurdenSetter{
				HouseholdID: &householdUUID, PurchaseID: &purchaseUUID, ItemID: &itemUUID, PoolID: &poolUUID,
				MemberID: &memberID, AmountYen: &part.Amount, FundingRuleVersion: &version, FundingWeight: &part.Weight, SnapshotSource: &snapshotSource,
			}).Exec(ctx, executor); err != nil {
				return err
			}
		}
	}
	return nil
}

func snapshotCorrectionMemberBurden(ctx context.Context, executor bob.Executor, householdID string, correctionID, purchaseID gofrsuuid.UUID, itemID sql.Null[gofrsuuid.UUID], shares []domain.AllocationAmount, delta int64) error {
	where := []bob.Expression{models.PurchaseMemberBurdens.Columns.PurchaseID.EQ(psql.Arg(purchaseID))}
	if itemID.Valid {
		where = append(where, models.PurchaseMemberBurdens.Columns.ItemID.EQ(psql.Arg(itemID.V)))
	}
	snapshots, err := models.PurchaseMemberBurdens.Query(sm.Where(psql.And(where...))).All(ctx, executor)
	if err != nil {
		return err
	}
	byPool := make(map[gofrsuuid.UUID]map[gofrsuuid.UUID]int64)
	versions := make(map[gofrsuuid.UUID]int32)
	for _, snapshot := range snapshots {
		if byPool[snapshot.PoolID] == nil {
			byPool[snapshot.PoolID] = make(map[gofrsuuid.UUID]int64)
		}
		byPool[snapshot.PoolID][snapshot.MemberID] = snapshot.FundingWeight
		versions[snapshot.PoolID] = snapshot.FundingRuleVersion
	}
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return fmt.Errorf("parse household id: %w", err)
	}
	for _, share := range shares {
		poolID, err := gofrsuuid.FromString(share.PoolID)
		if err != nil {
			return fmt.Errorf("parse pool id: %w", err)
		}
		memberWeightsByID := byPool[poolID]
		if len(memberWeightsByID) == 0 {
			continue
		}
		weights := make([]domain.Weight, 0, len(memberWeightsByID))
		for memberID, weight := range memberWeightsByID {
			weights = append(weights, domain.Weight{PoolID: memberID.String(), Value: weight})
		}
		parts, err := domain.Allocate(share.Amount, weights)
		if err != nil {
			return err
		}
		for _, part := range parts {
			if part.Amount == 0 {
				continue
			}
			memberID, err := gofrsuuid.FromString(part.PoolID)
			if err != nil {
				return fmt.Errorf("parse member id: %w", err)
			}
			amount := part.Amount
			if delta < 0 {
				amount = -amount
			}
			itemIDValue := itemID
			version := versions[poolID]
			if _, err := models.CorrectionMemberBurdens.Insert(&models.CorrectionMemberBurdenSetter{
				HouseholdID: &householdUUID, CorrectionID: &correctionID, ItemID: &itemIDValue,
				PoolID: &poolID, MemberID: &memberID, AmountDeltaYen: &amount,
				FundingRuleVersion: &version, FundingWeight: &part.Weight,
			}).Exec(ctx, executor); err != nil {
				return err
			}
		}
	}
	return nil
}
