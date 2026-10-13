package settlement

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func PoolFundingRules(ctx context.Context, db bob.Executor, householdID string) (map[string]domain.PoolFundingRule, error) {
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	pools, err := models.Pools.Query(sm.Where(models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdUUID)))).All(ctx, db)
	if err != nil {
		return nil, err
	}
	versionByPool := make(map[gofrsuuid.UUID]int32, len(pools))
	rules := make(map[string]domain.PoolFundingRule, len(pools))
	for _, pool := range pools {
		id := pool.ID.String()
		versionByPool[pool.ID] = pool.FundingRuleVersion
		rules[id] = domain.PoolFundingRule{PoolID: id, Kind: pool.Kind, Version: int(pool.FundingRuleVersion)}
	}
	fundingRules, err := models.PoolFundingRules.Query(sm.Where(models.PoolFundingRules.Columns.HouseholdID.EQ(psql.Arg(householdUUID)))).All(ctx, db)
	if err != nil {
		return nil, err
	}
	for _, storedRule := range fundingRules {
		if versionByPool[storedRule.PoolID] != storedRule.Version {
			continue
		}
		rule := rules[storedRule.PoolID.String()]
		if storedRule.OwnerMemberID.Valid {
			rule.OwnerMemberID = storedRule.OwnerMemberID.V.String()
		}
		if storedRule.SettlementPolicy.Valid {
			rule.SettlementPolicy = storedRule.SettlementPolicy.V
		}
		rules[rule.PoolID] = rule
	}
	fundingShares, err := models.PoolFundingShares.Query(sm.Where(models.PoolFundingShares.Columns.HouseholdID.EQ(psql.Arg(householdUUID)))).All(ctx, db)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(fundingShares, func(left, right *models.PoolFundingShare) int {
		if comparison := cmp.Compare(left.PoolID.String(), right.PoolID.String()); comparison != 0 {
			return comparison
		}
		if comparison := cmp.Compare(left.Version, right.Version); comparison != 0 {
			return comparison
		}
		return cmp.Compare(left.MemberID.String(), right.MemberID.String())
	})
	for _, share := range fundingShares {
		if versionByPool[share.PoolID] != share.Version {
			continue
		}
		rule := rules[share.PoolID.String()]
		rule.FundingShares = append(rule.FundingShares, domain.MemberWeight{MemberID: share.MemberID.String(), Weight: share.Weight})
		rules[rule.PoolID] = rule
	}
	return rules, nil
}

func calculatePeriodAccounting(ctx context.Context, db bob.Executor, householdID, period string) ([]domain.SettlementAmount, error) {
	start, err := parseAccountingPeriod(period)
	if err != nil {
		return nil, fmt.Errorf("%w: period must use YYYY-MM", ErrBadInput)
	}
	end := start.AddDate(0, 1, 0)
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	householdMembers, err := models.HouseholdMembers.Query(
		sm.Where(models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdUUID))),
		sm.OrderBy(models.HouseholdMembers.Columns.JoinedAt),
		sm.OrderBy(models.HouseholdMembers.Columns.MemberID),
	).All(ctx, db)
	if err != nil {
		return nil, err
	}
	if err := householdMembers.LoadMember(ctx, db); err != nil {
		return nil, err
	}
	inputs := make([]domain.SettlementInput, 0, len(householdMembers))
	memberIndex := make(map[string]int, len(householdMembers))
	for _, householdMember := range householdMembers {
		memberID := householdMember.MemberID.String()
		memberIndex[memberID] = len(inputs)
		inputs = append(inputs, domain.SettlementInput{MemberID: memberID, MemberName: householdMember.R.Member.DisplayName})
	}
	if err := addFundingAmounts(ctx, db, householdUUID, period, start, end, inputs, memberIndex); err != nil {
		return nil, err
	}
	expenses, rules, err := loadFundingExpenses(ctx, db, householdUUID, start, end)
	if err != nil {
		return nil, err
	}
	changes, err := domain.CalculateExpenseAdjustments(expenses, rules)
	if err != nil {
		return nil, err
	}
	for _, change := range changes {
		index, found := memberIndex[change.MemberID]
		if !found {
			return nil, domain.ErrInvalidFundingExpense
		}
		input := &inputs[index]
		input.MonthlyAdjustmentYen, found = checkedAdd(input.MonthlyAdjustmentYen, change.MonthlyAdjustmentYen)
		if !found {
			return nil, domain.ErrInvalidFundingExpense
		}
		input.AdvanceCreditYen, found = checkedAdd(input.AdvanceCreditYen, change.AdvanceCreditYen)
		if !found {
			return nil, domain.ErrInvalidFundingExpense
		}
	}
	adjustments, err := models.SettlementAdjustments.Query(sm.Where(psql.And(
		models.SettlementAdjustments.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.SettlementAdjustments.Columns.Period.EQ(psql.Arg(period)),
	))).All(ctx, db)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(adjustments, func(left, right *models.SettlementAdjustment) int {
		return cmp.Compare(left.MemberID.String(), right.MemberID.String())
	})
	for _, adjustment := range adjustments {
		index, found := memberIndex[adjustment.MemberID.String()]
		if !found {
			return nil, domain.ErrInvalidSettlement
		}
		inputs[index].MonthlyAdjustmentYen, found = checkedAdd(inputs[index].MonthlyAdjustmentYen, adjustment.AmountYen)
		if !found {
			return nil, domain.ErrInvalidSettlement
		}
	}
	return domain.CalculateSettlement(inputs)
}

func CalculatePeriod(ctx context.Context, db bob.Executor, householdID, period string) ([]domain.SettlementAmount, error) {
	return calculatePeriodAccounting(ctx, db, householdID, period)
}

func addFundingAmounts(ctx context.Context, db bob.Executor, householdUUID gofrsuuid.UUID, period string, start, end time.Time, inputs []domain.SettlementInput, memberIndex map[string]int) error {
	type fundingKey struct{ memberID, poolID string }
	type funding struct{ scheduled, paid int64 }
	amounts := make(map[fundingKey]funding)
	obligations, err := models.FundingObligations.Query(sm.Where(psql.And(
		models.FundingObligations.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.FundingObligations.Columns.Period.EQ(psql.Arg(period)),
	))).All(ctx, db)
	if err != nil {
		return err
	}
	for _, obligation := range obligations {
		key := fundingKey{memberID: obligation.MemberID.String(), poolID: obligation.PoolID.String()}
		amounts[key] = funding{scheduled: obligation.AmountYen, paid: obligation.PaidYen}
	}
	schedules, err := models.FundingSchedules.Query(sm.Where(psql.And(
		models.FundingSchedules.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.FundingSchedules.Columns.Cadence.EQ(psql.Arg("MONTHLY")),
		psql.Or(
			models.FundingSchedules.Columns.Active.EQ(psql.Arg(true)),
			models.FundingSchedules.Columns.EndsOn.IsNotNull(),
		),
		models.FundingSchedules.Columns.StartsOn.LT(psql.Arg(end)),
		psql.Or(
			models.FundingSchedules.Columns.EndsOn.IsNull(),
			models.FundingSchedules.Columns.EndsOn.GTE(psql.Arg(start)),
		),
	))).All(ctx, db)
	if err != nil {
		return err
	}
	slices.SortFunc(schedules, func(left, right *models.FundingSchedule) int {
		if comparison := cmp.Compare(left.MemberID.String(), right.MemberID.String()); comparison != 0 {
			return comparison
		}
		return cmp.Compare(left.PoolID.String(), right.PoolID.String())
	})
	scheduleAmounts := make(map[fundingKey]int64)
	for _, schedule := range schedules {
		key := fundingKey{memberID: schedule.MemberID.String(), poolID: schedule.PoolID.String()}
		var ok bool
		scheduleAmounts[key], ok = checkedAdd(scheduleAmounts[key], schedule.AmountYen)
		if !ok {
			return domain.ErrInvalidSettlement
		}
	}
	scheduleKeys := make([]fundingKey, 0, len(scheduleAmounts))
	for key := range scheduleAmounts {
		scheduleKeys = append(scheduleKeys, key)
	}
	slices.SortFunc(scheduleKeys, func(left, right fundingKey) int {
		if comparison := cmp.Compare(left.memberID, right.memberID); comparison != 0 {
			return comparison
		}
		return cmp.Compare(left.poolID, right.poolID)
	})
	for _, key := range scheduleKeys {
		value := amounts[key]
		if scheduleAmounts[key] > value.scheduled {
			value.scheduled = scheduleAmounts[key]
		}
		amounts[key] = value
	}
	keys := make([]fundingKey, 0, len(amounts))
	for key := range amounts {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(left, right fundingKey) int {
		if comparison := cmp.Compare(left.memberID, right.memberID); comparison != 0 {
			return comparison
		}
		return cmp.Compare(left.poolID, right.poolID)
	})
	for _, key := range keys {
		amount := amounts[key]
		index, exists := memberIndex[key.memberID]
		if !exists {
			return domain.ErrInvalidSettlement
		}
		input := &inputs[index]
		input.ScheduledYen, exists = checkedAdd(input.ScheduledYen, amount.scheduled)
		if !exists {
			return domain.ErrInvalidSettlement
		}
		input.PaidYen, exists = checkedAdd(input.PaidYen, amount.paid)
		if !exists {
			return domain.ErrInvalidSettlement
		}
	}
	return nil
}

func checkedAdd(left, right int64) (int64, bool) {
	if right > 0 && left > math.MaxInt64-right || right < 0 && left < math.MinInt64-right {
		return 0, false
	}
	return left + right, true
}
