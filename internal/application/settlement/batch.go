package settlement

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type GetRequest struct {
	HouseholdID string
	Period      string
}

type getRequestKey struct {
	householdID gofrsuuid.UUID
	period      string
}

func (w *Workflow) GetMany(ctx context.Context, requests []GetRequest) ([]Settlement, error) {
	if len(requests) == 0 {
		return []Settlement{}, nil
	}
	parsed := make([]getRequestKey, len(requests))
	householdSet := make(map[gofrsuuid.UUID]struct{}, len(requests))
	periodSet := make(map[string]struct{}, len(requests))
	for index, request := range requests {
		if _, err := parsePeriod(request.Period); err != nil {
			return nil, fmt.Errorf("%w: period must use YYYY-MM", ErrBadInput)
		}
		householdID, err := gofrsuuid.FromString(request.HouseholdID)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid household id", ErrBadInput)
		}
		parsed[index] = getRequestKey{householdID: householdID, period: request.Period}
		householdSet[householdID] = struct{}{}
		periodSet[request.Period] = struct{}{}
	}
	householdIDs := make([]gofrsuuid.UUID, 0, len(householdSet))
	for id := range householdSet {
		householdIDs = append(householdIDs, id)
	}
	slices.SortFunc(householdIDs, func(left, right gofrsuuid.UUID) int { return cmp.Compare(left.String(), right.String()) })
	householdArgs := make([]bob.Expression, len(householdIDs))
	for index, id := range householdIDs {
		householdArgs[index] = psql.Arg(id)
	}
	periods := make([]string, 0, len(periodSet))
	for period := range periodSet {
		periods = append(periods, period)
	}
	slices.Sort(periods)
	periodArgs := make([]bob.Expression, len(periods))
	for index, period := range periods {
		periodArgs[index] = psql.Arg(period)
	}

	tx, err := w.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	executor := bobpgx.NewTx(tx, nil)
	runs, err := models.SettlementRuns.Query(sm.Where(psql.And(
		models.SettlementRuns.Columns.HouseholdID.In(householdArgs...),
		models.SettlementRuns.Columns.Period.In(periodArgs...),
		models.SettlementRuns.Columns.Status.EQ(psql.Arg(closedStatus)),
	))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	runsByKey := make(map[getRequestKey]*models.SettlementRun, len(runs))
	runIDs := make([]bob.Expression, 0, len(runs))
	for _, run := range runs {
		key := getRequestKey{householdID: run.HouseholdID, period: run.Period}
		current := runsByKey[key]
		if current == nil || current.Revision < run.Revision {
			runsByKey[key] = run
		}
	}
	for _, run := range runsByKey {
		runIDs = append(runIDs, psql.Arg(run.ID))
	}
	linesByRun := make(map[gofrsuuid.UUID][]*models.SettlementLine)
	if len(runIDs) > 0 {
		lines, err := models.SettlementLines.Query(sm.Where(models.SettlementLines.Columns.SettlementRunID.In(runIDs...))).All(ctx, executor)
		if err != nil {
			return nil, err
		}
		for _, line := range lines {
			linesByRun[line.SettlementRunID] = append(linesByRun[line.SettlementRunID], line)
		}
	}
	var members models.HouseholdMemberSlice
	if len(runIDs) > 0 {
		members, err = models.HouseholdMembers.Query(sm.Where(models.HouseholdMembers.Columns.HouseholdID.In(householdArgs...))).All(ctx, executor)
		if err != nil {
			return nil, err
		}
		if err := members.LoadMember(ctx, executor); err != nil {
			return nil, err
		}
	}
	namesByHousehold := make(map[gofrsuuid.UUID]map[gofrsuuid.UUID]string, len(householdIDs))
	for _, member := range members {
		if namesByHousehold[member.HouseholdID] == nil {
			namesByHousehold[member.HouseholdID] = make(map[gofrsuuid.UUID]string)
		}
		namesByHousehold[member.HouseholdID][member.MemberID] = member.R.Member.DisplayName
	}
	evidenceRequests := make([]GetRequest, 0, len(parsed))
	evidenceSeen := make(map[evidenceKey]struct{}, len(parsed))
	for _, key := range parsed {
		mapKey := evidenceKey{householdID: key.householdID, period: key.period}
		if _, found := evidenceSeen[mapKey]; found {
			continue
		}
		evidenceSeen[mapKey] = struct{}{}
		evidenceRequests = append(evidenceRequests, GetRequest{HouseholdID: key.householdID.String(), Period: key.period})
	}
	evidenceByKey, err := settlementEvidenceMany(ctx, executor, evidenceRequests)
	if err != nil {
		return nil, err
	}
	openRequests := make([]GetRequest, 0, len(requests))
	openIndexes := make([]int, 0, len(requests))
	result := make([]Settlement, len(requests))
	for index, key := range parsed {
		evidence := evidenceByKey[evidenceKey{householdID: key.householdID, period: key.period}]
		run := runsByKey[key]
		if run == nil {
			openRequests = append(openRequests, requests[index])
			openIndexes = append(openIndexes, index)
			result[index] = Settlement{HouseholdID: key.householdID.String(), Period: key.period, Status: "OPEN", Evidence: evidence}
			continue
		}
		lines := linesByRun[run.ID]
		names := namesByHousehold[key.householdID]
		slices.SortFunc(lines, func(left, right *models.SettlementLine) int {
			return cmp.Compare(names[left.MemberID], names[right.MemberID])
		})
		value := Settlement{HouseholdID: key.householdID.String(), Period: key.period, Status: closedStatus, Revision: run.Revision, CreatedAt: new(run.CreatedAt), Lines: make([]domain.SettlementAmount, 0, len(lines)), Evidence: evidence}
		for _, line := range lines {
			value.Lines = append(value.Lines, domain.SettlementAmount{
				MemberID: line.MemberID.String(), MemberName: names[line.MemberID], ScheduledYen: line.ScheduledYen,
				PaidYen: line.PaidYen, CarryYen: line.CarryYen, MonthlyAdjustmentYen: line.MonthlyAdjustmentYen,
				AdvanceCreditYen: line.AdvanceCreditYen, AmountDueYen: line.AmountDueYen,
			})
		}
		result[index] = value
	}
	if len(openRequests) > 0 {
		previews, err := settlementPreviewsMany(ctx, executor, openRequests)
		if err != nil {
			return nil, err
		}
		for index, preview := range previews {
			request := openRequests[index]
			previous := result[openIndexes[index]]
			result[openIndexes[index]] = Settlement{HouseholdID: request.HouseholdID, Period: request.Period, Status: "OPEN", Lines: preview, Evidence: previous.Evidence}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func settlementPreviewsMany(ctx context.Context, db bob.Executor, requests []GetRequest) ([][]domain.SettlementAmount, error) {
	keys := make([]getRequestKey, len(requests))
	householdSet := make(map[gofrsuuid.UUID]struct{}, len(requests))
	maxTarget := time.Time{}
	for index, request := range requests {
		householdID, err := gofrsuuid.FromString(request.HouseholdID)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid household id", ErrBadInput)
		}
		target, err := parsePeriod(request.Period)
		if err != nil {
			return nil, fmt.Errorf("%w: period must use YYYY-MM", ErrBadInput)
		}
		keys[index] = getRequestKey{householdID: householdID, period: request.Period}
		householdSet[householdID] = struct{}{}
		if target.After(maxTarget) {
			maxTarget = target
		}
	}
	householdIDs := make([]gofrsuuid.UUID, 0, len(householdSet))
	for householdID := range householdSet {
		householdIDs = append(householdIDs, householdID)
	}
	slices.SortFunc(householdIDs, func(left, right gofrsuuid.UUID) int { return cmp.Compare(left.String(), right.String()) })
	householdArgs := make([]bob.Expression, len(householdIDs))
	for index, householdID := range householdIDs {
		householdArgs[index] = psql.Arg(householdID)
	}
	runs, err := models.SettlementRuns.Query(sm.Where(psql.And(
		models.SettlementRuns.Columns.HouseholdID.In(householdArgs...),
		models.SettlementRuns.Columns.Period.LT(psql.Arg(maxTarget.Format("2006-01"))),
		models.SettlementRuns.Columns.Status.EQ(psql.Arg(closedStatus)),
	))).All(ctx, db)
	if err != nil {
		return nil, err
	}
	latestRunByRequest := make(map[getRequestKey]*models.SettlementRun, len(requests))
	for _, run := range runs {
		for _, key := range keys {
			if key.householdID != run.HouseholdID || run.Period >= key.period {
				continue
			}
			current := latestRunByRequest[key]
			if current == nil || current.Period < run.Period || current.Period == run.Period && current.Revision < run.Revision {
				latestRunByRequest[key] = run
			}
		}
	}
	runIDs := make([]bob.Expression, 0, len(runs))
	selectedRuns := make(map[gofrsuuid.UUID]*models.SettlementRun)
	for _, run := range latestRunByRequest {
		selectedRuns[run.ID] = run
	}
	for id := range selectedRuns {
		runIDs = append(runIDs, psql.Arg(id))
	}
	carryByRun := make(map[gofrsuuid.UUID]map[gofrsuuid.UUID]int64, len(selectedRuns))
	if len(runIDs) > 0 {
		lines, err := models.SettlementLines.Query(sm.Where(models.SettlementLines.Columns.SettlementRunID.In(runIDs...))).All(ctx, db)
		if err != nil {
			return nil, err
		}
		for _, line := range lines {
			if carryByRun[line.SettlementRunID] == nil {
				carryByRun[line.SettlementRunID] = make(map[gofrsuuid.UUID]int64)
			}
			carryByRun[line.SettlementRunID][line.MemberID] = line.AmountDueYen
		}
	}
	noRunHouseholds := make(map[gofrsuuid.UUID]struct{})
	for _, key := range keys {
		if latestRunByRequest[key] == nil {
			noRunHouseholds[key.householdID] = struct{}{}
		}
	}
	earliestByHousehold := make(map[gofrsuuid.UUID]string, len(noRunHouseholds))
	if len(noRunHouseholds) > 0 {
		noRunIDs := make([]bob.Expression, 0, len(noRunHouseholds))
		for id := range noRunHouseholds {
			noRunIDs = append(noRunIDs, psql.Arg(id))
		}
		obligations, err := models.FundingObligations.Query(sm.Where(models.FundingObligations.Columns.HouseholdID.In(noRunIDs...))).All(ctx, db)
		if err != nil {
			return nil, err
		}
		for _, obligation := range obligations {
			earliestByHousehold[obligation.HouseholdID] = earlierPeriod(earliestByHousehold[obligation.HouseholdID], obligation.Period)
		}
		schedules, err := models.FundingSchedules.Query(sm.Where(models.FundingSchedules.Columns.HouseholdID.In(noRunIDs...))).All(ctx, db)
		if err != nil {
			return nil, err
		}
		for _, schedule := range schedules {
			earliestByHousehold[schedule.HouseholdID] = earlierPeriod(earliestByHousehold[schedule.HouseholdID], accounting.PeriodFor(schedule.StartsOn))
		}
		purchases, err := models.Purchases.Query(sm.Where(psql.And(
			models.Purchases.Columns.HouseholdID.In(noRunIDs...),
			models.Purchases.Columns.Status.In(psql.Arg("POSTED"), psql.Arg("CLOSED")),
		))).All(ctx, db)
		if err != nil {
			return nil, err
		}
		for _, purchase := range purchases {
			earliestByHousehold[purchase.HouseholdID] = earlierPeriod(earliestByHousehold[purchase.HouseholdID], accounting.PeriodFor(purchase.OccurredAt))
		}
	}
	firstOpenByRequest := make([]time.Time, len(requests))
	startByHousehold := make(map[gofrsuuid.UUID]time.Time, len(householdIDs))
	endByHousehold := make(map[gofrsuuid.UUID]time.Time, len(householdIDs))
	for index, key := range keys {
		target, _ := parsePeriod(key.period)
		firstOpen := target
		if run := latestRunByRequest[key]; run != nil {
			closedMonth, err := parsePeriod(run.Period)
			if err != nil {
				return nil, err
			}
			firstOpen = closedMonth.AddDate(0, 1, 0)
		} else if earliestByHousehold[key.householdID] != "" {
			firstOpen, err = parsePeriod(earliestByHousehold[key.householdID])
			if err != nil {
				return nil, err
			}
		}
		if firstOpen.After(target) {
			firstOpen = target
		}
		firstOpenByRequest[index] = firstOpen
		if current, ok := startByHousehold[key.householdID]; !ok || firstOpen.Before(current) {
			startByHousehold[key.householdID] = firstOpen
		}
		end := target.AddDate(0, 1, 0)
		if end.After(endByHousehold[key.householdID]) {
			endByHousehold[key.householdID] = end
		}
	}
	periodInputs, err := calculatePeriodsAccountingForHouseholds(ctx, db, startByHousehold, endByHousehold)
	if err != nil {
		return nil, err
	}
	results := make([][]domain.SettlementAmount, len(requests))
	for index, key := range keys {
		target, _ := parsePeriod(key.period)
		carry := make(map[string]int64)
		if run := latestRunByRequest[key]; run != nil {
			for memberID, amount := range carryByRun[run.ID] {
				carry[memberID.String()] = amount
			}
		}
		for month := firstOpenByRequest[index]; month.Before(target); month = month.AddDate(0, 1, 0) {
			for _, line := range periodInputs[key.householdID][month.Format("2006-01")] {
				value, ok := checkedAdd(carry[line.MemberID], line.AmountDueYen)
				if !ok {
					return nil, domain.ErrInvalidSettlement
				}
				carry[line.MemberID] = value
			}
		}
		current := periodInputs[key.householdID][key.period]
		for lineIndex := range current {
			current[lineIndex].CarryYen = carry[current[lineIndex].MemberID]
			amountDue, ok := checkedAdd(current[lineIndex].AmountDueYen, current[lineIndex].CarryYen)
			if !ok {
				return nil, domain.ErrInvalidSettlement
			}
			current[lineIndex].AmountDueYen = amountDue
		}
		results[index] = current
	}
	return results, nil
}
