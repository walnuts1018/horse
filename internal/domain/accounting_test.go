package domain

import (
	"errors"
	"testing"
)

func TestCalculateExpenseAdjustmentsBalancesSharedPoolToFundingShares(t *testing.T) {
	rules := map[string]PoolFundingRule{
		"shared":     {PoolID: "shared", Kind: FundingPoolShared, SettlementPolicy: SettlementBalanceToStandard, FundingShares: []MemberWeight{{MemberID: "A", Weight: 60}, {MemberID: "B", Weight: 40}}},
		"personal-a": {PoolID: "personal-a", Kind: FundingPoolPersonal, OwnerMemberID: "A"},
		"personal-b": {PoolID: "personal-b", Kind: FundingPoolPersonal, OwnerMemberID: "B"},
	}
	got, err := CalculateExpenseAdjustments([]FundingExpense{{PaymentPoolID: "shared", Items: []FundingExpenseItem{{Amount: 1000, Allocations: []PoolAmount{{PoolID: "personal-a", Amount: 1000}}}}}}, rules)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{"A": 400, "B": -400}
	assertAdjustments(t, got, want, nil)
}

func TestCalculateExpenseAdjustmentsReimbursesPersonalSpendingFromSharedPool(t *testing.T) {
	rules := map[string]PoolFundingRule{
		"shared":     {PoolID: "shared", Kind: FundingPoolShared, SettlementPolicy: SettlementReimbursePersonal, FundingShares: []MemberWeight{{MemberID: "A", Weight: 3}, {MemberID: "B", Weight: 2}}},
		"personal-a": {PoolID: "personal-a", Kind: FundingPoolPersonal, OwnerMemberID: "A"},
		"personal-b": {PoolID: "personal-b", Kind: FundingPoolPersonal, OwnerMemberID: "B"},
	}
	got, err := CalculateExpenseAdjustments([]FundingExpense{{PaymentPoolID: "shared", Items: []FundingExpenseItem{{Amount: 1000, Allocations: []PoolAmount{{PoolID: "personal-a", Amount: 1000}}}}}}, rules)
	if err != nil {
		t.Fatal(err)
	}
	assertAdjustments(t, got, map[string]int64{"A": 1000}, nil)
}

func TestCalculateExpenseAdjustmentsCreditsPersonalAdvance(t *testing.T) {
	rules := map[string]PoolFundingRule{
		"personal-a": {PoolID: "personal-a", Kind: FundingPoolPersonal, OwnerMemberID: "A"},
		"personal-b": {PoolID: "personal-b", Kind: FundingPoolPersonal, OwnerMemberID: "B"},
	}
	got, err := CalculateExpenseAdjustments([]FundingExpense{{PaymentPoolID: "personal-a", Items: []FundingExpenseItem{{Amount: 500, Allocations: []PoolAmount{{PoolID: "personal-a", Amount: 300}, {PoolID: "personal-b", Amount: 200}}}}}}, rules)
	if err != nil {
		t.Fatal(err)
	}
	assertAdjustments(t, got, map[string]int64{"B": 200}, map[string]int64{"A": 200})
}

func TestCalculateExpenseAdjustmentsRejectsUnconfiguredOwnership(t *testing.T) {
	rules := map[string]PoolFundingRule{"personal": {PoolID: "personal", Kind: FundingPoolPersonal}}
	_, err := CalculateExpenseAdjustments([]FundingExpense{{PaymentPoolID: "personal", Items: []FundingExpenseItem{{Amount: 10, Allocations: []PoolAmount{{PoolID: "personal", Amount: 10}}}}}}, rules)
	if !errors.Is(err, ErrIncompleteFundingRule) {
		t.Fatalf("expected missing owner error, got %v", err)
	}
}

func assertAdjustments(t *testing.T, got []ExpenseAdjustment, adjustments, advances map[string]int64) {
	t.Helper()
	if len(got) != len(adjustments)+len(advances) {
		t.Fatalf("got %d members, want %d: %#v", len(got), len(adjustments)+len(advances), got)
	}
	for _, entry := range got {
		if entry.MonthlyAdjustmentYen != adjustments[entry.MemberID] || entry.AdvanceCreditYen != advances[entry.MemberID] {
			t.Errorf("member %s got adjustment %d and advance %d, want %d and %d", entry.MemberID, entry.MonthlyAdjustmentYen, entry.AdvanceCreditYen, adjustments[entry.MemberID], advances[entry.MemberID])
		}
	}
}
