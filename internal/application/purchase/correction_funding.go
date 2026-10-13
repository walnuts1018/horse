package purchase

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"slices"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type correctionFundingRuleVersion struct {
	poolID  gofrsuuid.UUID
	version int32
}

func correctionFundingExpense(ctx context.Context, executor bob.Executor, householdID, purchaseID gofrsuuid.UUID, itemID sql.Null[gofrsuuid.UUID], allocations []domain.AllocationAmount) ([]domain.FundingExpense, map[string]domain.PoolFundingRule, error) {
	var amount int64
	for _, allocation := range allocations {
		if allocation.Amount < 0 {
			return nil, nil, domain.ErrInvalidFundingExpense
		}
		var ok bool
		amount, ok = checkedAdd(amount, allocation.Amount)
		if !ok {
			return nil, nil, domain.ErrInvalidFundingExpense
		}
	}
	if amount < 0 || len(allocations) == 0 {
		return nil, nil, domain.ErrInvalidFundingExpense
	}
	householdUUID := householdID
	payments, err := models.PurchasePayments.Query(sm.Where(psql.And(
		models.PurchasePayments.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.PurchasePayments.Columns.PurchaseID.EQ(psql.Arg(purchaseID)),
	)), sm.OrderBy(models.PurchasePayments.Columns.LineNo)).All(ctx, executor)
	if err != nil {
		return nil, nil, err
	}
	paymentSnapshots, err := models.PurchasePaymentFundingSnapshots.Query(sm.Where(psql.And(
		models.PurchasePaymentFundingSnapshots.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.PurchasePaymentFundingSnapshots.Columns.PurchaseID.EQ(psql.Arg(purchaseID)),
	)), sm.OrderBy(models.PurchasePaymentFundingSnapshots.Columns.PaymentLineNo), sm.OrderBy(models.PurchasePaymentFundingSnapshots.Columns.MemberID)).All(ctx, executor)
	if err != nil {
		return nil, nil, err
	}
	if len(payments) == 0 || len(paymentSnapshots) == 0 {
		return nil, nil, fmt.Errorf("%w: purchase %s payment", correctionErrMissingPurchaseFundingSnapshot, purchaseID)
	}
	snapshotsByLine := make(map[int32]models.PurchasePaymentFundingSnapshotSlice)
	for _, snapshot := range paymentSnapshots {
		snapshotsByLine[snapshot.PaymentLineNo] = append(snapshotsByLine[snapshot.PaymentLineNo], snapshot)
	}
	rules := make(map[string]domain.PoolFundingRule)
	paymentWeights := make([]domain.Weight, 0, len(payments))
	for _, payment := range payments {
		snapshots := snapshotsByLine[payment.LineNo]
		if len(snapshots) == 0 {
			return nil, nil, fmt.Errorf("%w: purchase %s payment line %d", correctionErrMissingPurchaseFundingSnapshot, purchaseID, payment.LineNo)
		}
		first := snapshots[0]
		alias := fmt.Sprintf("%s:correction-payment:%d", purchaseID, payment.LineNo)
		rule := domain.PoolFundingRule{PoolID: alias, Version: int(first.FundingRuleVersion)}
		for _, snapshot := range snapshots {
			if snapshot.PoolID != first.PoolID || snapshot.FundingRuleVersion != first.FundingRuleVersion || snapshot.SettlementPolicy != first.SettlementPolicy || snapshot.SnapshotSource != first.SnapshotSource {
				return nil, nil, fmt.Errorf("%w: purchase %s payment line %d is inconsistent", correctionErrMissingPurchaseFundingSnapshot, purchaseID, payment.LineNo)
			}
			rule.FundingShares = append(rule.FundingShares, domain.MemberWeight{MemberID: snapshot.MemberID.String(), Weight: snapshot.FundingWeight})
		}
		if first.SettlementPolicy.Valid {
			rule.Kind = domain.FundingPoolShared
			rule.SettlementPolicy = first.SettlementPolicy.V
		} else {
			if len(snapshots) != 1 || first.FundingWeight != 1 {
				return nil, nil, fmt.Errorf("%w: purchase %s personal payment line %d is inconsistent", correctionErrMissingPurchaseFundingSnapshot, purchaseID, payment.LineNo)
			}
			rule.Kind = domain.FundingPoolPersonal
			rule.OwnerMemberID = first.MemberID.String()
			rule.FundingShares = nil
		}
		rules[alias] = rule
		paymentWeights = append(paymentWeights, domain.Weight{PoolID: alias, Value: payment.AmountYen})
		delete(snapshotsByLine, payment.LineNo)
	}
	if len(snapshotsByLine) != 0 {
		return nil, nil, fmt.Errorf("%w: purchase %s has orphaned payment snapshots", correctionErrMissingPurchaseFundingSnapshot, purchaseID)
	}
	allocatedPayments, err := domain.Allocate(amount, paymentWeights)
	if err != nil {
		return nil, nil, err
	}
	fundingPayments := make([]domain.FundingPayment, 0, len(allocatedPayments))
	for _, payment := range allocatedPayments {
		if payment.Amount == 0 {
			continue
		}
		fundingPayments = append(fundingPayments, domain.FundingPayment{ID: payment.PoolID, PoolID: payment.PoolID, Amount: payment.Amount})
	}
	where := []bob.Expression{
		models.PurchaseMemberBurdens.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.PurchaseMemberBurdens.Columns.PurchaseID.EQ(psql.Arg(purchaseID)),
	}
	if itemID.Valid {
		where = append(where, models.PurchaseMemberBurdens.Columns.ItemID.EQ(psql.Arg(itemID.V)))
	}
	burdens, err := models.PurchaseMemberBurdens.Query(sm.Where(psql.And(where...)),
		sm.OrderBy(models.PurchaseMemberBurdens.Columns.PoolID), sm.OrderBy(models.PurchaseMemberBurdens.Columns.MemberID),
	).All(ctx, executor)
	if err != nil {
		return nil, nil, err
	}
	if len(burdens) == 0 {
		return nil, nil, fmt.Errorf("%w: purchase %s has no member burden snapshot", correctionErrMissingPurchaseFundingSnapshot, purchaseID)
	}
	versions := make(map[correctionFundingRuleVersion]struct{})
	weightsByPool := make(map[gofrsuuid.UUID]map[gofrsuuid.UUID]int64)
	versionsByPool := make(map[gofrsuuid.UUID]int32)
	for _, burden := range burdens {
		key := correctionFundingRuleVersion{poolID: burden.PoolID, version: burden.FundingRuleVersion}
		versions[key] = struct{}{}
		if previous, found := versionsByPool[burden.PoolID]; found && previous != burden.FundingRuleVersion {
			return nil, nil, fmt.Errorf("%w: purchase %s pool %s has inconsistent funding rule versions", correctionErrMissingPurchaseFundingSnapshot, purchaseID, burden.PoolID)
		}
		versionsByPool[burden.PoolID] = burden.FundingRuleVersion
		memberWeights := weightsByPool[burden.PoolID]
		if memberWeights == nil {
			memberWeights = make(map[gofrsuuid.UUID]int64)
			weightsByPool[burden.PoolID] = memberWeights
		}
		if previous, found := memberWeights[burden.MemberID]; found && previous != burden.FundingWeight {
			return nil, nil, fmt.Errorf("%w: purchase %s pool %s has inconsistent member weights", correctionErrMissingPurchaseFundingSnapshot, purchaseID, burden.PoolID)
		}
		memberWeights[burden.MemberID] = burden.FundingWeight
	}
	keys := make([]correctionFundingRuleVersion, 0, len(versions))
	for key := range versions {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(left, right correctionFundingRuleVersion) int {
		if order := cmp.Compare(left.poolID.String(), right.poolID.String()); order != 0 {
			return order
		}
		return cmp.Compare(left.version, right.version)
	})
	conditions := make([]bob.Expression, 0, len(keys))
	for _, key := range keys {
		conditions = append(conditions, psql.And(
			models.PoolFundingRules.Columns.PoolID.EQ(psql.Arg(key.poolID)),
			models.PoolFundingRules.Columns.Version.EQ(psql.Arg(key.version)),
		))
	}
	historicalRules, err := models.PoolFundingRules.Query(sm.Where(psql.And(
		models.PoolFundingRules.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		psql.Or(conditions...),
	))).All(ctx, executor)
	if err != nil {
		return nil, nil, err
	}
	historicalByKey := make(map[correctionFundingRuleVersion]*models.PoolFundingRule, len(historicalRules))
	for _, rule := range historicalRules {
		historicalByKey[correctionFundingRuleVersion{poolID: rule.PoolID, version: rule.Version}] = rule
	}
	allocationAmounts := make(map[gofrsuuid.UUID]int64, len(allocations))
	for _, allocation := range allocations {
		poolID, err := gofrsuuid.FromString(allocation.PoolID)
		if err != nil {
			return nil, nil, err
		}
		allocationAmounts[poolID] = allocation.Amount
	}
	if len(allocationAmounts) != len(weightsByPool) {
		return nil, nil, fmt.Errorf("%w: purchase %s correction allocation snapshot is inconsistent", correctionErrMissingPurchaseFundingSnapshot, purchaseID)
	}
	fundingAllocations := make([]domain.PoolAmount, 0, len(allocationAmounts))
	for poolID, storedMemberWeights := range weightsByPool {
		key := correctionFundingRuleVersion{poolID: poolID, version: versionsByPool[poolID]}
		storedRule, found := historicalByKey[key]
		if !found {
			return nil, nil, fmt.Errorf("%w: purchase %s pool %s funding rule version %d", correctionErrMissingPurchaseFundingSnapshot, purchaseID, poolID, key.version)
		}
		allocationAlias := fmt.Sprintf("%s:correction-allocation:%s", purchaseID, poolID)
		rule := domain.PoolFundingRule{PoolID: allocationAlias, Version: int(key.version)}
		memberIDs := make([]gofrsuuid.UUID, 0, len(storedMemberWeights))
		for memberID := range storedMemberWeights {
			memberIDs = append(memberIDs, memberID)
		}
		slices.SortFunc(memberIDs, func(left, right gofrsuuid.UUID) int { return cmp.Compare(left.String(), right.String()) })
		memberWeights := make([]domain.MemberWeight, 0, len(memberIDs))
		for _, memberID := range memberIDs {
			memberWeights = append(memberWeights, domain.MemberWeight{MemberID: memberID.String(), Weight: storedMemberWeights[memberID]})
		}
		if storedRule.OwnerMemberID.Valid {
			if len(memberWeights) != 1 || memberWeights[0].MemberID != storedRule.OwnerMemberID.V.String() || memberWeights[0].Weight != 1 {
				return nil, nil, fmt.Errorf("%w: purchase %s personal allocation snapshot is inconsistent", correctionErrMissingPurchaseFundingSnapshot, purchaseID)
			}
			rule.Kind = domain.FundingPoolPersonal
			rule.OwnerMemberID = storedRule.OwnerMemberID.V.String()
		} else {
			rule.Kind = domain.FundingPoolShared
			rule.FundingShares = memberWeights
		}
		rules[allocationAlias] = rule
		fundingAllocations = append(fundingAllocations, domain.PoolAmount{PoolID: allocationAlias, Amount: allocationAmounts[poolID]})
	}
	if len(fundingAllocations) == 0 {
		return nil, nil, domain.ErrInvalidFundingExpense
	}
	items := []domain.FundingExpenseItem{{Amount: amount, Allocations: fundingAllocations}}
	expenses, err := domain.SplitFundingExpense(items, fundingPayments)
	if err != nil {
		return nil, nil, err
	}
	return expenses, rules, nil
}
