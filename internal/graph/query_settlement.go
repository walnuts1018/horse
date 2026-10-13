package graph

import (
	"context"

	settlementapp "github.com/walnuts1018/horse/internal/application/settlement"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func (r *queryResolver) settlement(ctx context.Context, householdID, period string) (*model.Settlement, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	result, err := settlementapp.New(r.DB).Get(ctx, householdID, period)
	if err != nil {
		return nil, mapSettlementError(err)
	}
	return settlementModel(result), nil
}

func settlementModel(value settlementapp.Settlement) *model.Settlement {
	status := model.SettlementStatus(value.Status)
	result := &model.Settlement{
		HouseholdID: value.HouseholdID, Period: model.YearMonth(value.Period), Status: status,
		Revision: int(value.Revision), Lines: make([]*model.SettlementLine, 0, len(value.Lines)),
	}
	if value.CreatedAt != nil {
		createdAt := timeString(*value.CreatedAt)
		result.CreatedAt = &createdAt
	}
	for _, line := range value.Lines {
		result.Lines = append(result.Lines, &model.SettlementLine{
			MemberID: line.MemberID, MemberName: line.MemberName,
			ScheduledYen: graphLong(line.ScheduledYen), PaidYen: graphLong(line.PaidYen),
			CarryYen: graphLong(line.CarryYen), PriorBalanceYen: graphLong(line.CarryYen),
			MonthlyAdjustmentYen: graphLong(line.MonthlyAdjustmentYen), AdvanceCreditYen: graphLong(line.AdvanceCreditYen),
			AmountDueYen: graphLong(line.AmountDueYen),
		})
	}
	return result
}
