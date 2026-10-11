package domain

import (
	"errors"
	"math/big"
	"slices"
)

var (
	ErrNegativeAmount = errors.New("amount must not be negative")
	ErrInvalidWeights = errors.New("allocation weights must have unique pools and positive weights")
)

type Weight struct {
	PoolID string
	Value  int64
}

type AllocationAmount struct {
	PoolID string
	Weight int64
	Amount int64
}

type allocationRemainder struct {
	index int
	value *big.Int
}

// Allocateは整数通貨単位を最大剰余法で配分します。
func Allocate(amount int64, weights []Weight) ([]AllocationAmount, error) {
	if amount < 0 {
		return nil, ErrNegativeAmount
	}
	if len(weights) == 0 {
		return nil, ErrInvalidWeights
	}

	seen := make(map[string]struct{}, len(weights))
	totalWeight := new(big.Int)
	for _, weight := range weights {
		if weight.PoolID == "" || weight.Value <= 0 {
			return nil, ErrInvalidWeights
		}
		if _, exists := seen[weight.PoolID]; exists {
			return nil, ErrInvalidWeights
		}
		seen[weight.PoolID] = struct{}{}
		totalWeight.Add(totalWeight, big.NewInt(weight.Value))
	}

	result := make([]AllocationAmount, len(weights))
	remainders := make([]allocationRemainder, 0, len(weights))
	allocated := int64(0)
	amountBig := big.NewInt(amount)
	for index, weight := range weights {
		numerator := new(big.Int).Mul(amountBig, big.NewInt(weight.Value))
		quotient, remainder := new(big.Int), new(big.Int)
		quotient.QuoRem(numerator, totalWeight, remainder)
		part := quotient.Int64()
		result[index] = AllocationAmount{PoolID: weight.PoolID, Weight: weight.Value, Amount: part}
		allocated += part
		remainders = append(remainders, allocationRemainder{index: index, value: remainder})
	}

	slices.SortFunc(remainders, func(a, b allocationRemainder) int {
		if comparison := b.value.Cmp(a.value); comparison != 0 {
			return comparison
		}
		if result[a.index].PoolID < result[b.index].PoolID {
			return -1
		}
		if result[a.index].PoolID > result[b.index].PoolID {
			return 1
		}
		return 0
	})
	for index := range amount - allocated {
		result[remainders[index].index].Amount++
	}
	slices.SortFunc(result, func(a, b AllocationAmount) int {
		if a.PoolID < b.PoolID {
			return -1
		}
		if a.PoolID > b.PoolID {
			return 1
		}
		return 0
	})
	return result, nil
}
