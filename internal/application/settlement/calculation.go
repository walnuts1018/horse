package settlement

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type fundingKey struct{ memberID, poolID string }

type scheduledFunding struct {
	amount   int64
	schedule *models.FundingSchedule
}

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
	results, err := calculatePeriodsAccounting(ctx, db, householdID, start, start.AddDate(0, 1, 0))
	if err != nil {
		return nil, err
	}
	return results[period], nil
}

func calculatePeriodsAccounting(ctx context.Context, db bob.Executor, householdID string, start, end time.Time) (map[string][]domain.SettlementAmount, error) {
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	results, err := calculatePeriodsAccountingForHouseholds(ctx, db, map[gofrsuuid.UUID]time.Time{householdUUID: start}, map[gofrsuuid.UUID]time.Time{householdUUID: end})
	if err != nil {
		return nil, err
	}
	return results[householdUUID], nil
}

func calculatePeriodsAccountingForHouseholds(ctx context.Context, db bob.Executor, starts, ends map[gofrsuuid.UUID]time.Time) (map[gofrsuuid.UUID]map[string][]domain.SettlementAmount, error) {
	householdIDs := make([]gofrsuuid.UUID, 0, len(starts))
	start, end := time.Time{}, time.Time{}
	for householdID, value := range starts {
		householdIDs = append(householdIDs, householdID)
		if start.IsZero() || value.Before(start) {
			start = value
		}
		if ends[householdID].After(end) {
			end = ends[householdID]
		}
	}
	start = time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, start.Location())
	end = time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, end.Location())
	if !start.Before(end) {
		return map[gofrsuuid.UUID]map[string][]domain.SettlementAmount{}, nil
	}
	firstPeriod, endPeriod := start.Format("2006-01"), end.Format("2006-01")
	householdArgs := make([]bob.Expression, len(householdIDs))
	for index, householdID := range householdIDs {
		householdArgs[index] = psql.Arg(householdID)
	}
	householdMembers, err := models.HouseholdMembers.Query(
		sm.Where(models.HouseholdMembers.Columns.HouseholdID.In(householdArgs...)),
		sm.OrderBy(models.HouseholdMembers.Columns.JoinedAt),
		sm.OrderBy(models.HouseholdMembers.Columns.MemberID),
	).All(ctx, db)
	if err != nil {
		return nil, err
	}
	if err := householdMembers.LoadMember(ctx, db); err != nil {
		return nil, err
	}
	obligations, err := models.FundingObligations.Query(sm.Where(psql.And(
		models.FundingObligations.Columns.HouseholdID.In(householdArgs...),
		models.FundingObligations.Columns.Period.GTE(psql.Arg(firstPeriod)),
		models.FundingObligations.Columns.Period.LT(psql.Arg(endPeriod)),
	))).All(ctx, db)
	if err != nil {
		return nil, err
	}
	schedules, err := models.FundingSchedules.Query(sm.Where(psql.And(
		models.FundingSchedules.Columns.HouseholdID.In(householdArgs...),
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
		return nil, err
	}
	expensesByHousehold, rules, err := loadFundingExpensesForHouseholds(ctx, db, householdIDs, start, end)
	if err != nil {
		return nil, err
	}
	adjustments, err := models.SettlementAdjustments.Query(sm.Where(psql.And(
		models.SettlementAdjustments.Columns.HouseholdID.In(householdArgs...),
		models.SettlementAdjustments.Columns.Period.GTE(psql.Arg(firstPeriod)),
		models.SettlementAdjustments.Columns.Period.LT(psql.Arg(endPeriod)),
	))).All(ctx, db)
	if err != nil {
		return nil, err
	}
	obligationsByHousehold := make(map[gofrsuuid.UUID]map[string]models.FundingObligationSlice)
	for _, obligation := range obligations {
		if obligationsByHousehold[obligation.HouseholdID] == nil {
			obligationsByHousehold[obligation.HouseholdID] = make(map[string]models.FundingObligationSlice)
		}
		obligationsByHousehold[obligation.HouseholdID][obligation.Period] = append(obligationsByHousehold[obligation.HouseholdID][obligation.Period], obligation)
	}
	adjustmentsByHousehold := make(map[gofrsuuid.UUID]map[string]models.SettlementAdjustmentSlice)
	for _, adjustment := range adjustments {
		if adjustmentsByHousehold[adjustment.HouseholdID] == nil {
			adjustmentsByHousehold[adjustment.HouseholdID] = make(map[string]models.SettlementAdjustmentSlice)
		}
		adjustmentsByHousehold[adjustment.HouseholdID][adjustment.Period] = append(adjustmentsByHousehold[adjustment.HouseholdID][adjustment.Period], adjustment)
	}
	membersByHousehold := make(map[gofrsuuid.UUID]models.HouseholdMemberSlice, len(householdIDs))
	for _, householdMember := range householdMembers {
		membersByHousehold[householdMember.HouseholdID] = append(membersByHousehold[householdMember.HouseholdID], householdMember)
	}
	schedulesByHousehold := make(map[gofrsuuid.UUID]models.FundingScheduleSlice, len(householdIDs))
	for _, schedule := range schedules {
		schedulesByHousehold[schedule.HouseholdID] = append(schedulesByHousehold[schedule.HouseholdID], schedule)
	}
	results := make(map[gofrsuuid.UUID]map[string][]domain.SettlementAmount, len(householdIDs))
	for _, householdID := range householdIDs {
		results[householdID] = make(map[string][]domain.SettlementAmount)
		for month := starts[householdID]; month.Before(ends[householdID]); month = month.AddDate(0, 1, 0) {
			period := month.Format("2006-01")
			members := membersByHousehold[householdID]
			inputs := make([]domain.SettlementInput, 0, len(members))
			memberIndex := make(map[string]int, len(members))
			for _, householdMember := range members {
				if !memberActiveDuringPeriod(householdMember, month) {
					continue
				}
				memberID := householdMember.MemberID.String()
				memberIndex[memberID] = len(inputs)
				inputs = append(inputs, domain.SettlementInput{MemberID: memberID, MemberName: householdMember.R.Member.DisplayName})
			}
			if err := addFundingAmountsFromRows(month, month.AddDate(0, 1, 0), obligationsByHousehold[householdID][period], schedulesByHousehold[householdID], inputs, memberIndex); err != nil {
				return nil, err
			}
			changes, err := domain.CalculateExpenseAdjustments(expensesByHousehold[householdID][period], rules)
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
			periodAdjustments := adjustmentsByHousehold[householdID][period]
			slices.SortFunc(periodAdjustments, func(left, right *models.SettlementAdjustment) int {
				return cmp.Compare(left.MemberID.String(), right.MemberID.String())
			})
			for _, adjustment := range periodAdjustments {
				index, found := memberIndex[adjustment.MemberID.String()]
				if !found {
					return nil, domain.ErrInvalidSettlement
				}
				inputs[index].MonthlyAdjustmentYen, found = checkedAdd(inputs[index].MonthlyAdjustmentYen, adjustment.AmountYen)
				if !found {
					return nil, domain.ErrInvalidSettlement
				}
			}
			results[householdID][period], err = domain.CalculateSettlement(inputs)
			if err != nil {
				return nil, err
			}
		}
	}
	return results, nil
}

func memberActiveDuringPeriod(member *models.HouseholdMember, start time.Time) bool {
	return !member.LeftAt.Valid || member.LeftAt.V.After(start)
}

func CalculatePeriod(ctx context.Context, db bob.Executor, householdID, period string) ([]domain.SettlementAmount, error) {
	return calculatePeriodAccounting(ctx, db, householdID, period)
}

func addFundingAmountsFromRows(start, end time.Time, obligations models.FundingObligationSlice, schedules models.FundingScheduleSlice, inputs []domain.SettlementInput, memberIndex map[string]int) error {
	type funding struct{ scheduled, paid int64 }
	amounts := make(map[fundingKey]funding)
	for _, obligation := range obligations {
		key := fundingKey{memberID: obligation.MemberID.String(), poolID: obligation.PoolID.String()}
		amounts[key] = funding{scheduled: obligation.AmountYen, paid: obligation.PaidYen}
	}
	slices.SortFunc(schedules, func(left, right *models.FundingSchedule) int {
		if comparison := cmp.Compare(left.MemberID.String(), right.MemberID.String()); comparison != 0 {
			return comparison
		}
		return cmp.Compare(left.PoolID.String(), right.PoolID.String())
	})
	scheduleAmounts, err := aggregateFundingSchedules(start, end, schedules)
	if err != nil {
		return err
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
		if scheduleAmounts[key].amount > value.scheduled {
			value.scheduled = scheduleAmounts[key].amount
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

func aggregateFundingSchedules(start, end time.Time, schedules models.FundingScheduleSlice) (map[fundingKey]scheduledFunding, error) {
	amounts := make(map[fundingKey]scheduledFunding)
	periodEnd := dateOnly(end.AddDate(0, 0, -1))
	for _, schedule := range schedules {
		startsOn := dateOnly(schedule.StartsOn)
		endsOn := time.Time{}
		if schedule.EndsOn.Valid {
			endsOn = dateOnly(schedule.EndsOn.V)
		}
		if !startsOn.Before(end) || schedule.EndsOn.Valid && endsOn.Before(periodEnd) || !schedule.EndsOn.Valid && !schedule.Active {
			continue
		}
		key := fundingKey{memberID: schedule.MemberID.String(), poolID: schedule.PoolID.String()}
		current, exists := amounts[key]
		if exists && !scheduleIsNewer(schedule, current.schedule) {
			continue
		}
		amount, err := fundingScheduleAmount(start, end, schedule)
		if err != nil {
			return nil, err
		}
		amounts[key] = scheduledFunding{amount: amount, schedule: schedule}
	}
	return amounts, nil
}

func scheduleIsNewer(candidate, current *models.FundingSchedule) bool {
	if !candidate.StartsOn.Equal(current.StartsOn) {
		return candidate.StartsOn.After(current.StartsOn)
	}
	if !candidate.CreatedAt.Equal(current.CreatedAt) {
		return candidate.CreatedAt.After(current.CreatedAt)
	}
	return candidate.ID.String() > current.ID.String()
}

func dateOnly(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func fundingScheduleAmount(start, end time.Time, schedule *models.FundingSchedule) (int64, error) {
	periodStart, periodEnd := dateOnly(start), dateOnly(end)
	startsOn := dateOnly(schedule.StartsOn)
	endsOn := time.Time{}
	if schedule.EndsOn.Valid {
		endsOn = dateOnly(schedule.EndsOn.V)
	}
	switch schedule.Cadence {
	case "MONTHLY":
		if !startsOn.Before(periodEnd) || !endsOn.IsZero() && endsOn.Before(periodStart) {
			return 0, nil
		}
		return schedule.AmountYen, nil
	case "CUSTOM":
		if startsOn.Before(periodStart) || !startsOn.Before(periodEnd) || !endsOn.IsZero() && startsOn.After(endsOn) {
			return 0, nil
		}
		return schedule.AmountYen, nil
	case "WEEKLY":
		firstDate := startsOn
		if firstDate.Before(periodStart) {
			days := int(periodStart.Sub(firstDate).Hours() / 24)
			weeks := (days + 6) / 7
			firstDate = firstDate.AddDate(0, 0, weeks*7)
		}
		var total int64
		for occurrence := firstDate; occurrence.Before(periodEnd) && (endsOn.IsZero() || !occurrence.After(endsOn)); occurrence = occurrence.AddDate(0, 0, 7) {
			var ok bool
			total, ok = checkedAdd(total, schedule.AmountYen)
			if !ok {
				return 0, domain.ErrInvalidSettlement
			}
		}
		return total, nil
	default:
		return 0, domain.ErrInvalidSettlement
	}
}

func checkedAdd(left, right int64) (int64, bool) {
	return accounting.Add(left, right)
}
