package graph

import (
	"context"
	"slices"
	"testing"

	"github.com/walnuts1018/horse/internal/graph/model"
)

func TestNodeIDDirectiveDecodesList(t *testing.T) {
	globalIDs := make([]string, 2)
	want := []string{"123e4567-e89b-42d3-a456-426614174000", "123e4567-e89b-42d3-a456-426614174001"}
	for i := range want {
		id, err := encodeNodeID("Tag", want[i])
		if err != nil {
			t.Fatal(err)
		}
		globalIDs[i] = id
	}

	value, err := NodeIDDirective(t.Context(), map[string]any{"ids": globalIDs}, func(context.Context) (any, error) {
		return globalIDs, nil
	}, "Tag")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := value.([]string)
	if !ok || !slices.Equal(got, want) {
		t.Fatalf("NodeIDDirective() = %#v, want %#v", value, want)
	}
}

func TestNodeIDDirectiveDecodesLoanBalance(t *testing.T) {
	const rawID = "123e4567-e89b-42d3-a456-426614174000"
	globalID, err := encodeNodeID("LoanBalance", rawID)
	if err != nil {
		t.Fatal(err)
	}

	value, err := NodeIDDirective(t.Context(), map[string]any{"id": globalID}, func(context.Context) (any, error) {
		return globalID, nil
	}, "LoanBalance")
	if err != nil {
		t.Fatal(err)
	}
	if value != rawID {
		t.Fatalf("NodeIDDirective() = %#v, want %q", value, rawID)
	}
}

func TestNodeReferenceResolversReturnGlobalIDs(t *testing.T) {
	const householdID = "123e4567-e89b-42d3-a456-426614174000"
	const lenderID = "123e4567-e89b-42d3-a456-426614174001"
	const borrowerID = "123e4567-e89b-42d3-a456-426614174002"

	tagHouseholdID, err := (&Resolver{}).Tag().HouseholdID(t.Context(), &model.Tag{HouseholdID: householdID})
	if err != nil {
		t.Fatal(err)
	}
	wantHouseholdID, err := encodeNodeID("Household", householdID)
	if err != nil {
		t.Fatal(err)
	}
	if tagHouseholdID != wantHouseholdID {
		t.Fatalf("Tag.householdId = %q, want %q", tagHouseholdID, wantHouseholdID)
	}

	loanBalance := &model.LoanBalance{LenderMemberID: lenderID, BorrowerMemberID: borrowerID}
	lenderGlobalID, err := (&Resolver{}).LoanBalance().LenderMemberID(t.Context(), loanBalance)
	if err != nil {
		t.Fatal(err)
	}
	borrowerGlobalID, err := (&Resolver{}).LoanBalance().BorrowerMemberID(t.Context(), loanBalance)
	if err != nil {
		t.Fatal(err)
	}
	wantLenderGlobalID, err := encodeNodeID("Member", lenderID)
	if err != nil {
		t.Fatal(err)
	}
	wantBorrowerGlobalID, err := encodeNodeID("Member", borrowerID)
	if err != nil {
		t.Fatal(err)
	}
	if lenderGlobalID != wantLenderGlobalID || borrowerGlobalID != wantBorrowerGlobalID {
		t.Fatalf("LoanBalance member IDs = %q, %q; want %q, %q", lenderGlobalID, borrowerGlobalID, wantLenderGlobalID, wantBorrowerGlobalID)
	}

	settlement := &model.Settlement{HouseholdID: householdID, Period: "2026-10"}
	settlementGlobalID, err := (&Resolver{}).Settlement().ID(t.Context(), settlement)
	if err != nil {
		t.Fatal(err)
	}
	decodedKind, payload, err := decodeGlobalNodeID(settlementGlobalID)
	if err != nil {
		t.Fatal(err)
	}
	decodedHouseholdID, decodedPeriod, err := decodeSettlementNodeID(payload)
	if err != nil {
		t.Fatal(err)
	}
	if decodedKind != "Settlement" || decodedHouseholdID != householdID || decodedPeriod != settlement.Period {
		t.Fatalf("Settlement node ID decoded to %q, %q, %q", decodedKind, decodedHouseholdID, decodedPeriod)
	}
}
