package domain

import (
	"errors"
	"fmt"
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

func FuzzAllocateProperties(f *testing.F) {
	f.Add(int64(101), []byte{0, 1, 2, 3})
	f.Add(int64(0), []byte{1, 0})
	f.Add(int64(10), []byte{2, 255})
	f.Add(int64(10), []byte{3, 255})
	f.Add(int64(10), []byte{4, 255})
	f.Add(int64(10), []byte{5, 255})
	f.Fuzz(func(t *testing.T, rawAmount int64, data []byte) {
		amount := int64(uint64(rawAmount) % 1_000_001)
		weights := make([]Weight, 1+len(data)%8)
		for index := range weights {
			value := int64(1)
			if len(data) > 0 {
				value += int64(data[index%len(data)])
			}
			weights[index] = Weight{PoolID: fmt.Sprintf("pool-%d", index), Value: value}
		}

		mode := byte(0)
		if len(data) > 0 {
			mode = data[0] % 6
		}
		switch mode {
		case 1:
			weights = nil
		case 2:
			weights[0].Value = 0
		case 3:
			if len(weights) == 1 {
				weights = append(weights, Weight{PoolID: "duplicate", Value: 1})
			}
			weights[1].PoolID = weights[0].PoolID
		case 4:
			weights[0].PoolID = ""
		case 5:
			amount = -1
		}

		if mode != 0 {
			_, err := Allocate(amount, weights)
			wantErr := ErrInvalidWeights
			if mode == 5 {
				wantErr = ErrNegativeAmount
			}
			if !errors.Is(err, wantErr) {
				t.Fatalf("invalid input was accepted or returned the wrong error: got %v, want %v", err, wantErr)
			}
			return
		}

		result, err := Allocate(amount, weights)
		if err != nil {
			t.Fatalf("Allocate(%d, %#v): %v", amount, weights, err)
		}
		var total int64
		for _, allocation := range result {
			if allocation.Amount < 0 {
				t.Fatalf("negative allocation: %#v", allocation)
			}
			total += allocation.Amount
		}
		if total != amount {
			t.Fatalf("allocation total = %d, want %d: %#v", total, amount, result)
		}
		repeated, err := Allocate(amount, weights)
		if err != nil {
			t.Fatalf("repeated allocation failed: %v", err)
		}
		if len(repeated) != len(result) {
			t.Fatalf("repeated allocation length = %d, want %d", len(repeated), len(result))
		}
		for index := range result {
			if repeated[index] != result[index] {
				t.Fatalf("allocation changed between calls: first %#v, second %#v", result, repeated)
			}
		}
	})
}
