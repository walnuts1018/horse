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
	PaidAmount    int64
	Items         []FundingExpenseItem
}

type FundingPayment struct {
	ID     string
	PoolID string
	Amount int64
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
		paidAmount := expense.PaidAmount
		if paidAmount == 0 {
			paidAmount = total
		}
		if paidAmount < 0 {
			return nil, ErrInvalidFundingExpense
		}
		if total == 0 && paidAmount == 0 {
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
					change, exists = subtractInt64(paidAmount, burden)
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
				adjustment.AdvanceCreditYen, exists = addInt64(adjustment.AdvanceCreditYen, paidAmount)
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
			baseline, err := Allocate(paidAmount, paymentShares)
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

// SplitFundingExpenseは購入の負担配賦を決済元の金額比で分割します。
func SplitFundingExpense(items []FundingExpenseItem, payments []FundingPayment) ([]FundingExpense, error) {
	if len(items) == 0 || len(payments) == 0 {
		return nil, ErrInvalidFundingExpense
	}
	weights := make([]Weight, 0, len(payments))
	seen := make(map[string]struct{}, len(payments))
	var paymentTotal int64
	for _, payment := range payments {
		if payment.ID == "" || payment.PoolID == "" || payment.Amount <= 0 {
			return nil, ErrInvalidFundingExpense
		}
		if _, exists := seen[payment.ID]; exists {
			return nil, ErrInvalidFundingExpense
		}
		seen[payment.ID] = struct{}{}
		weights = append(weights, Weight{PoolID: payment.ID, Value: payment.Amount})
		var ok bool
		paymentTotal, ok = addInt64(paymentTotal, payment.Amount)
		if !ok {
			return nil, ErrInvalidFundingExpense
		}
	}
	result := make([]FundingExpense, len(payments))
	for index, payment := range payments {
		result[index].PaymentPoolID = payment.PoolID
		result[index].PaidAmount = payment.Amount
	}
	var itemsTotal int64
	for _, item := range items {
		if item.Amount < 0 || len(item.Allocations) == 0 {
			return nil, ErrInvalidFundingExpense
		}
		var itemTotal int64
		perPayment := make([]map[string]int64, len(payments))
		for index := range perPayment {
			perPayment[index] = make(map[string]int64, len(item.Allocations))
		}
		for _, allocation := range item.Allocations {
			if allocation.PoolID == "" || allocation.Amount < 0 {
				return nil, ErrInvalidFundingExpense
			}
			var ok bool
			itemTotal, ok = addInt64(itemTotal, allocation.Amount)
			if !ok {
				return nil, ErrInvalidFundingExpense
			}
			parts, err := Allocate(allocation.Amount, weights)
			if err != nil {
				return nil, err
			}
			for _, part := range parts {
				for index, payment := range payments {
					if payment.ID == part.PoolID {
						perPayment[index][allocation.PoolID] = part.Amount
						break
					}
				}
			}
		}
		if itemTotal != item.Amount || item.Amount > paymentTotal {
			return nil, ErrInvalidFundingExpense
		}
		var ok bool
		itemsTotal, ok = addInt64(itemsTotal, item.Amount)
		if !ok {
			return nil, ErrInvalidFundingExpense
		}
		for index := range payments {
			allocations := make([]PoolAmount, 0, len(perPayment[index]))
			var amount int64
			for poolID, allocated := range perPayment[index] {
				allocations = append(allocations, PoolAmount{PoolID: poolID, Amount: allocated})
				var ok bool
				amount, ok = addInt64(amount, allocated)
				if !ok {
					return nil, ErrInvalidFundingExpense
				}
			}
			slices.SortFunc(allocations, func(left, right PoolAmount) int { return cmpString(left.PoolID, right.PoolID) })
			result[index].Items = append(result[index].Items, FundingExpenseItem{Amount: amount, Allocations: allocations})
		}
	}
	if itemsTotal != paymentTotal {
		return nil, ErrInvalidFundingExpense
	}
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
