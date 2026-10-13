package graph

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (r *queryResolver) settlement(ctx context.Context, householdID, period string) (*model.Settlement, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	if _, err := parseAccountingPeriod(period); err != nil {
		return nil, fmt.Errorf("%w: period must use YYYY-MM", ErrBadInput)
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	executor := queryExecutor(r.DB)
	runs, err := models.SettlementRuns.Query(sm.Where(psql.And(models.SettlementRuns.Columns.HouseholdID.EQ(psql.Arg(hhID)), models.SettlementRuns.Columns.Period.EQ(psql.Arg(period)), models.SettlementRuns.Columns.Status.EQ(psql.Arg("CLOSED")))), sm.OrderBy(models.SettlementRuns.Columns.Revision).Desc(), sm.Limit(1)).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	if len(runs) > 0 {
		run := runs[0]
		lines, err := models.SettlementLines.Query(sm.Where(models.SettlementLines.Columns.SettlementRunID.EQ(psql.Arg(run.ID)))).All(ctx, executor)
		if err != nil {
			return nil, err
		}
		members, err := models.HouseholdMembers.Query(sm.Where(models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(hhID)))).All(ctx, executor)
		if err != nil {
			return nil, err
		}
		if err := members.LoadMember(ctx, executor); err != nil {
			return nil, err
		}
		names := make(map[gofrsuuid.UUID]string, len(members))
		for _, member := range members {
			names[member.MemberID] = member.R.Member.DisplayName
		}
		slices.SortFunc(lines, func(a, b *models.SettlementLine) int { return cmp.Compare(names[a.MemberID], names[b.MemberID]) })
		result := &model.Settlement{HouseholdID: householdID, Period: model.YearMonth(period), Status: model.SettlementStatusClosed, Revision: int(run.Revision), Lines: make([]*model.SettlementLine, 0, len(lines))}
		created := timeString(run.CreatedAt)
		result.CreatedAt = &created
		for _, line := range lines {
			result.Lines = append(result.Lines, &model.SettlementLine{MemberID: line.MemberID.String(), MemberName: names[line.MemberID], ScheduledYen: graphLong(line.ScheduledYen), PaidYen: graphLong(line.PaidYen), CarryYen: graphLong(line.CarryYen), PriorBalanceYen: graphLong(line.CarryYen), MonthlyAdjustmentYen: graphLong(line.MonthlyAdjustmentYen), AdvanceCreditYen: graphLong(line.AdvanceCreditYen), AmountDueYen: graphLong(line.AmountDueYen)})
		}
		return result, nil
	}
	result := &model.Settlement{HouseholdID: householdID, Period: model.YearMonth(period), Status: model.SettlementStatusOpen, Revision: 0, Lines: make([]*model.SettlementLine, 0)}
	settlement, err := calculateSettlementPreview(ctx, queryExecutor(r.DB), householdID, period)
	if err != nil {
		return nil, err
	}
	for _, line := range settlement {
		result.Lines = append(result.Lines, &model.SettlementLine{MemberID: line.MemberID, MemberName: line.MemberName, ScheduledYen: graphLong(line.ScheduledYen), PaidYen: graphLong(line.PaidYen), CarryYen: graphLong(line.CarryYen), PriorBalanceYen: graphLong(line.CarryYen), MonthlyAdjustmentYen: graphLong(line.MonthlyAdjustmentYen), AdvanceCreditYen: graphLong(line.AdvanceCreditYen), AmountDueYen: graphLong(line.AmountDueYen)})
	}
	return result, nil
}
