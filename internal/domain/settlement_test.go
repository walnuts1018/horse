package domain

import (
	"errors"
	"math"
	"testing"
)

func TestCalculateSettlementCarriesUnpaidAndKeepsOverpayment(t *testing.T) {
	result, err := CalculateSettlement([]SettlementInput{
		{MemberID: "a", ScheduledYen: 6000, PaidYen: 6000},
		{MemberID: "b", ScheduledYen: 4000, PaidYen: 0, PreviousOwedYen: 4000},
		{MemberID: "c", ScheduledYen: 4000, PaidYen: 6000, AdvanceCreditYen: 1000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result[0].AmountDueYen != 0 || result[1].AmountDueYen != 8000 || result[2].AmountDueYen != -3000 {
		t.Fatalf("unexpected settlement: %#v", result)
	}
}

func TestCalculateSettlementRejectsOverflow(t *testing.T) {
	_, err := CalculateSettlement([]SettlementInput{{MemberID: "a", ScheduledYen: math.MaxInt64, PreviousOwedYen: 1}})
	if !errors.Is(err, ErrInvalidSettlement) {
		t.Fatalf("expected overflow error, got %v", err)
	}
}
