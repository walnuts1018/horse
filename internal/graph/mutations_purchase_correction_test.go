package graph

import (
	"testing"

	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/graph/model"
)

const (
	testPoolA = "pool-a"
	testPoolB = "pool-b"
	testPoolC = "pool-c"
)

func TestSameAllocationWeightsAcceptsEquivalentPurchaseRatios(t *testing.T) {
	t.Parallel()

	inputs := []*model.AllocationInput{
		{PoolID: testPoolA, Weight: 2},
		{PoolID: testPoolB, Weight: 3},
	}
	saved := []domain.Weight{
		{PoolID: testPoolA, Value: 200},
		{PoolID: testPoolB, Value: 300},
	}
	if !sameAllocationWeights(inputs, saved) {
		t.Fatal("equivalent allocation ratios should match")
	}
}

func TestSameAllocationWeightsRejectsDifferentOrInvalidAllocations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		inputs []*model.AllocationInput
		saved  []domain.Weight
	}{
		{
			name: "different ratio",
			inputs: []*model.AllocationInput{
				{PoolID: testPoolA, Weight: 2},
				{PoolID: testPoolB, Weight: 4},
			},
			saved: []domain.Weight{{PoolID: testPoolA, Value: 200}, {PoolID: testPoolB, Value: 300}},
		},
		{
			name:   "different pool",
			inputs: []*model.AllocationInput{{PoolID: testPoolC, Weight: 2}, {PoolID: testPoolB, Weight: 3}},
			saved:  []domain.Weight{{PoolID: testPoolA, Value: 200}, {PoolID: testPoolB, Value: 300}},
		},
		{
			name:   "duplicate input pool",
			inputs: []*model.AllocationInput{{PoolID: testPoolA, Weight: 2}, {PoolID: testPoolA, Weight: 3}},
			saved:  []domain.Weight{{PoolID: testPoolA, Value: 200}, {PoolID: testPoolB, Value: 300}},
		},
		{
			name:   "nonpositive saved weight",
			inputs: []*model.AllocationInput{{PoolID: testPoolA, Weight: 2}},
			saved:  []domain.Weight{{PoolID: testPoolA, Value: 0}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if sameAllocationWeights(test.inputs, test.saved) {
				t.Fatal("invalid or different allocation should not match")
			}
		})
	}
}
