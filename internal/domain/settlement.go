package domain

import (
	"errors"
	"math"
)

var ErrInvalidSettlement = errors.New("invalid settlement amounts")

type SettlementInput struct {
	MemberID             string
	MemberName           string
	ScheduledYen         int64
	PaidYen              int64
	PreviousOwedYen      int64
	MonthlyAdjustmentYen int64
	AdvanceCreditYen     int64
}

type SettlementAmount struct {
	MemberID             string
	MemberName           string
	ScheduledYen         int64
	PaidYen              int64
	CarryYen             int64
	MonthlyAdjustmentYen int64
	AdvanceCreditYen     int64
	AmountDueYen         int64
}

func CalculateSettlement(inputs []SettlementInput) ([]SettlementAmount, error) {
	result := make([]SettlementAmount, len(inputs))
	for index, input := range inputs {
		if input.MemberID == "" || input.ScheduledYen < 0 || input.PaidYen < 0 || input.AdvanceCreditYen < 0 {
			return nil, ErrInvalidSettlement
		}
		carry := input.PreviousOwedYen
		amountDue, ok := addInt64(input.ScheduledYen, carry)
		if !ok {
			return nil, ErrInvalidSettlement
		}
		amountDue, ok = addInt64(amountDue, input.MonthlyAdjustmentYen)
		if !ok {
			return nil, ErrInvalidSettlement
		}
		amountDue, ok = subtractInt64(amountDue, input.PaidYen)
		if !ok {
			return nil, ErrInvalidSettlement
		}
		amountDue, ok = subtractInt64(amountDue, input.AdvanceCreditYen)
		if !ok {
			return nil, ErrInvalidSettlement
		}
		result[index] = SettlementAmount{
			MemberID: input.MemberID, MemberName: input.MemberName, ScheduledYen: input.ScheduledYen,
			PaidYen: input.PaidYen, CarryYen: carry, MonthlyAdjustmentYen: input.MonthlyAdjustmentYen, AdvanceCreditYen: input.AdvanceCreditYen, AmountDueYen: amountDue,
		}
	}
	return result, nil
}

func addInt64(left, right int64) (int64, bool) {
	if right > 0 && left > math.MaxInt64-right || right < 0 && left < math.MinInt64-right {
		return 0, false
	}
	return left + right, true
}

func subtractInt64(left, right int64) (int64, bool) {
	if right > 0 && left < math.MinInt64+right || right < 0 && left > math.MaxInt64+right {
		return 0, false
	}
	return left - right, true
}
