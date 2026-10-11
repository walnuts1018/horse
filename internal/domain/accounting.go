package domain

import (
	"errors"
	"fmt"
	"slices"
)

const (
	FundingPoolPersonal = "PERSONAL"
	FundingPoolShared   = "SHARED"

	SettlementBalanceToStandard = "BALANCE_TO_STANDARD"
	SettlementReimbursePersonal = "REIMBURSE_PERSONAL"
)

var (
	ErrIncompleteFundingRule = errors.New("pool ownership or funding shares are not configured")
	ErrInvalidFundingExpense = errors.New("invalid expense funding allocation")
)

type MemberWeight struct {
	MemberID string
	Weight   int64
}

type PoolFundingRule struct {
	PoolID           string
	Kind             string
	Version          int
	OwnerMemberID    string
	SettlementPolicy string
	FundingShares    []MemberWeight
}

type PoolAmount struct {
	PoolID string
	Amount int64
}

type FundingExpenseItem struct {
	Amount      int64
	Allocations []PoolAmount
}

type FundingExpense struct {
	PaymentPoolID string
	Items         []FundingExpenseItem
}

type ExpenseAdjustment struct {
	MemberID             string
	MonthlyAdjustmentYen int64
	AdvanceCreditYen     int64
}

// CalculateExpenseAdjustmentsは購入の支払元と負担先をmember単位に集約します。
func CalculateExpenseAdjustments(expenses []FundingExpense, rules map[string]PoolFundingRule) ([]ExpenseAdjustment, error) {
	adjustments := make(map[string]ExpenseAdjustment)
	for _, expense := range expenses {
		paymentRule, exists := rules[expense.PaymentPoolID]
		if !exists || expense.PaymentPoolID == "" {
			return nil, ErrIncompleteFundingRule
		}
		responsibilities := make(map[string]int64)
		var total int64
		for _, item := range expense.Items {
			if item.Amount < 0 || len(item.Allocations) == 0 {
				return nil, ErrInvalidFundingExpense
			}
			var allocated int64
			for _, allocation := range item.Allocations {
				if allocation.PoolID == "" || allocation.Amount < 0 {
					return nil, ErrInvalidFundingExpense
				}
				allocated, exists = addInt64(allocated, allocation.Amount)
				if !exists {
					return nil, ErrInvalidFundingExpense
				}
				poolRule, ruleExists := rules[allocation.PoolID]
				if !ruleExists {
					return nil, fmt.Errorf("%w: pool %s", ErrIncompleteFundingRule, allocation.PoolID)
				}
				if paymentRule.Kind == FundingPoolShared && paymentRule.SettlementPolicy == SettlementReimbursePersonal && poolRule.Kind == FundingPoolShared {
					continue
				}
				shares, err := poolMemberShares(poolRule)
				if err != nil {
					return nil, fmt.Errorf("pool %s: %w", allocation.PoolID, err)
				}
				distributed, err := Allocate(allocation.Amount, shares)
				if err != nil {
					return nil, err
				}
				for _, part := range distributed {
					responsibilities[part.PoolID], exists = addInt64(responsibilities[part.PoolID], part.Amount)
					if !exists {
						return nil, ErrInvalidFundingExpense
					}
				}
				if paymentRule.Kind == FundingPoolShared && paymentRule.SettlementPolicy == SettlementReimbursePersonal && poolRule.Kind == FundingPoolPersonal {
					adjustment, exists := adjustments[poolRule.OwnerMemberID]
					if !exists {
						adjustment.MemberID = poolRule.OwnerMemberID
					}
					adjustment.MonthlyAdjustmentYen, exists = addInt64(adjustment.MonthlyAdjustmentYen, allocation.Amount)
					if !exists {
						return nil, ErrInvalidFundingExpense
					}
					adjustments[poolRule.OwnerMemberID] = adjustment
				}
			}
			if allocated != item.Amount {
				return nil, ErrInvalidFundingExpense
			}
			total, exists = addInt64(total, item.Amount)
			if !exists {
				return nil, ErrInvalidFundingExpense
			}
		}
		if total == 0 {
			continue
		}
		if paymentRule.Kind == FundingPoolPersonal {
			if paymentRule.OwnerMemberID == "" {
				return nil, fmt.Errorf("pool %s: %w", paymentRule.PoolID, ErrIncompleteFundingRule)
			}
			for memberID, burden := range responsibilities {
				adjustment, exists := adjustments[memberID]
				if !exists {
					adjustment.MemberID = memberID
				}
				change := -burden
				if memberID == paymentRule.OwnerMemberID {
					change, exists = subtractInt64(total, burden)
					if !exists {
						return nil, ErrInvalidFundingExpense
					}
				}
				if change > 0 {
					adjustment.AdvanceCreditYen, exists = addInt64(adjustment.AdvanceCreditYen, change)
				} else if change < 0 {
					adjustment.MonthlyAdjustmentYen, exists = subtractInt64(adjustment.MonthlyAdjustmentYen, change)
				}
				if !exists {
					return nil, ErrInvalidFundingExpense
				}
				adjustments[memberID] = adjustment
			}
			if _, exists := responsibilities[paymentRule.OwnerMemberID]; !exists {
				adjustment := adjustments[paymentRule.OwnerMemberID]
				adjustment.MemberID = paymentRule.OwnerMemberID
				adjustment.AdvanceCreditYen, exists = addInt64(adjustment.AdvanceCreditYen, total)
				if !exists {
					return nil, ErrInvalidFundingExpense
				}
				adjustments[paymentRule.OwnerMemberID] = adjustment
			}
			continue
		}
		if paymentRule.Kind != FundingPoolShared {
			return nil, fmt.Errorf("pool %s: %w", paymentRule.PoolID, ErrIncompleteFundingRule)
		}
		paymentShares, err := poolMemberShares(paymentRule)
		if err != nil {
			return nil, fmt.Errorf("pool %s: %w", paymentRule.PoolID, err)
		}
		switch paymentRule.SettlementPolicy {
		case SettlementBalanceToStandard:
			baseline, err := Allocate(total, paymentShares)
			if err != nil {
				return nil, err
			}
			for _, part := range baseline {
				adjustment := adjustments[part.PoolID]
				adjustment.MemberID = part.PoolID
				change, exists := subtractInt64(responsibilities[part.PoolID], part.Amount)
				if !exists {
					return nil, ErrInvalidFundingExpense
				}
				adjustment.MonthlyAdjustmentYen, exists = addInt64(adjustment.MonthlyAdjustmentYen, change)
				if !exists {
					return nil, ErrInvalidFundingExpense
				}
				adjustments[part.PoolID] = adjustment
				delete(responsibilities, part.PoolID)
			}
			for memberID, burden := range responsibilities {
				adjustment := adjustments[memberID]
				adjustment.MemberID = memberID
				adjustment.MonthlyAdjustmentYen, exists = addInt64(adjustment.MonthlyAdjustmentYen, burden)
				if !exists {
					return nil, ErrInvalidFundingExpense
				}
				adjustments[memberID] = adjustment
			}
		case SettlementReimbursePersonal:
		default:
			return nil, fmt.Errorf("pool %s: %w", paymentRule.PoolID, ErrIncompleteFundingRule)
		}
	}
	result := make([]ExpenseAdjustment, 0, len(adjustments))
	for _, adjustment := range adjustments {
		result = append(result, adjustment)
	}
	slices.SortFunc(result, func(left, right ExpenseAdjustment) int {
		return cmpString(left.MemberID, right.MemberID)
	})
	return result, nil
}

func poolMemberShares(rule PoolFundingRule) ([]Weight, error) {
	if rule.Kind == FundingPoolPersonal {
		if rule.OwnerMemberID == "" {
			return nil, ErrIncompleteFundingRule
		}
		return []Weight{{PoolID: rule.OwnerMemberID, Value: 1}}, nil
	}
	if rule.Kind != FundingPoolShared || len(rule.FundingShares) == 0 {
		return nil, ErrIncompleteFundingRule
	}
	weights := make([]Weight, 0, len(rule.FundingShares))
	for _, share := range rule.FundingShares {
		weights = append(weights, Weight{PoolID: share.MemberID, Value: share.Weight})
	}
	return weights, nil
}

func cmpString(left, right string) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}
