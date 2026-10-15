package graph

import (
	"context"
	"fmt"

	settlementapp "github.com/walnuts1018/horse/internal/application/settlement"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func (r *queryResolver) settlement(ctx context.Context, householdID, period string) (model.SettlementVariant, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	result, err := settlementapp.New(r.DB).Get(ctx, householdID, period)
	if err != nil {
		return nil, mapSettlementError(err)
	}
	return settlementModel(result)
}

func settlementModel(value settlementapp.Settlement) (model.SettlementVariant, error) {
	id, err := encodeSettlementNodeID(value.HouseholdID, value.Period)
	if err != nil {
		return nil, err
	}
	result := &model.Settlement{
		ID:          id,
		HouseholdID: value.HouseholdID, Period: model.YearMonth(value.Period),
		Revision: int(value.Revision), Lines: make([]*model.SettlementLine, 0, len(value.Lines)),
	}
	for _, line := range value.Lines {
		lineModel := &model.SettlementLine{
			MemberID: line.MemberID, MemberName: line.MemberName,
			ScheduledYen: graphLong(line.ScheduledYen), PaidYen: graphLong(line.PaidYen),
			CarryYen: graphLong(line.CarryYen), PriorBalanceYen: graphLong(line.CarryYen),
			MonthlyAdjustmentYen: graphLong(line.MonthlyAdjustmentYen), AdvanceCreditYen: graphLong(line.AdvanceCreditYen),
			AmountDueYen: graphLong(line.AmountDueYen),
			Evidence:     make([]*model.SettlementEvidence, 0),
		}
		for _, evidence := range value.Evidence {
			if evidence.MemberID != line.MemberID {
				continue
			}
			item := &model.SettlementEvidence{Kind: evidence.Kind, Label: evidence.Label, AmountYen: graphLong(evidence.AmountYen), SourceID: evidence.SourceID}
			if evidence.PurchaseID != "" {
				purchaseID, err := encodeNodeID("Purchase", evidence.PurchaseID)
				if err != nil {
					return nil, err
				}
				item.PurchaseID = &purchaseID
			}
			if evidence.OccurredAt != nil {
				occurredAt := timeString(*evidence.OccurredAt)
				item.OccurredAt = &occurredAt
			}
			lineModel.Evidence = append(lineModel.Evidence, item)
		}
		result.Lines = append(result.Lines, lineModel)
	}
	switch value.Status {
	case settlementStatusOpen:
		return &model.OpenSettlement{Settlement: result}, nil
	case settlementStatusClosed:
		if value.CreatedAt == nil {
			return nil, fmt.Errorf("closed settlement %q has no created time", value.Period)
		}
		createdAt := timeString(*value.CreatedAt)
		result.CreatedAt = &createdAt
		return &model.ClosedSettlement{Settlement: result}, nil
	default:
		return nil, fmt.Errorf("unknown settlement status %q", value.Status)
	}
}
