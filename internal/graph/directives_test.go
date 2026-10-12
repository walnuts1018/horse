package graph

import (
	"slices"
	"testing"
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

	value, err := NodeIDDirective(t.Context(), globalIDs, nil, "Tag")
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

	value, err := NodeIDDirective(t.Context(), globalID, nil, "LoanBalance")
	if err != nil {
		t.Fatal(err)
	}
	if value != rawID {
		t.Fatalf("NodeIDDirective() = %#v, want %q", value, rawID)
	}
}
