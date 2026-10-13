package domain

import (
	"errors"
	"testing"
)

func TestAllocateLargestRemainder(t *testing.T) {
	t.Parallel()
	result, err := Allocate(1, []Weight{{PoolID: "b", Value: 1}, {PoolID: "a", Value: 1}, {PoolID: "c", Value: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 3 || result[0] != (AllocationAmount{PoolID: "a", Weight: 1, Amount: 1}) || result[1].Amount != 0 || result[2].Amount != 0 {
		t.Fatalf("unexpected allocation: %#v", result)
	}

	result, err = Allocate(101, []Weight{{PoolID: "a", Value: 3}, {PoolID: "b", Value: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if result[0].Amount != 61 || result[1].Amount != 40 {
		t.Fatalf("unexpected allocation: %#v", result)
	}
	if result[0].Amount+result[1].Amount != 101 {
		t.Fatalf("allocation lost minor units: %#v", result)
	}
}

func TestAllocateRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	_, err := Allocate(-1, []Weight{{PoolID: "a", Value: 1}})
	if !errors.Is(err, ErrNegativeAmount) {
		t.Fatalf("expected negative amount error, got %v", err)
	}
	_, err = Allocate(5, []Weight{{PoolID: "a", Value: 1}, {PoolID: "a", Value: 1}})
	if !errors.Is(err, ErrInvalidWeights) {
		t.Fatalf("expected invalid weights error, got %v", err)
	}
}
