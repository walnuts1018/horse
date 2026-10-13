package graph

import (
	"context"
	"fmt"
	"strings"

	allocationapp "github.com/walnuts1018/horse/internal/application/allocation"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func (r *mutationResolver) setDefaultAllocation(ctx context.Context, input model.SetDefaultAllocationInput) (*model.Household, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	weights, err := allocationWeights(input.Allocations)
	if err != nil {
		return nil, err
	}
	commandWeights := make([]allocationapp.Weight, 0, len(weights))
	for _, weight := range weights {
		commandWeights = append(commandWeights, allocationapp.Weight{PoolID: weight.PoolID, Value: weight.Value})
	}
	if err := allocationapp.NewService(r.DB).SetDefault(ctx, allocationapp.SetDefaultCommand{
		ActorID: user.MemberID, HouseholdID: input.HouseholdID, Weights: commandWeights,
	}); err != nil {
		return nil, err
	}
	return loadHousehold(ctx, r.DB, input.HouseholdID)
}

func (r *mutationResolver) setAllocationRule(ctx context.Context, input model.SetAllocationRuleInput) (*model.AllocationRule, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	if input.Target == nil {
		return nil, fmt.Errorf("%w: allocation rule target is required", ErrBadInput)
	}
	weights, err := allocationWeights(input.Allocations)
	if err != nil {
		return nil, err
	}
	commandWeights := make([]allocationapp.Weight, 0, len(weights))
	for _, weight := range weights {
		commandWeights = append(commandWeights, allocationapp.Weight{PoolID: weight.PoolID, Value: weight.Value})
	}
	target := allocationapp.RuleTarget{
		CategoryID: input.Target.CategoryID, ProductConceptID: input.Target.ProductConceptID,
		ProductVariantID: input.Target.ProductVariantID, Merchant: input.Target.Merchant,
	}
	ruleID, err := allocationapp.NewService(r.DB).SetRule(ctx, allocationapp.SetRuleCommand{
		ActorID: user.MemberID, HouseholdID: input.HouseholdID, Target: target, Weights: commandWeights,
	})
	if err != nil {
		return nil, err
	}
	return loadAllocationRule(ctx, r.DB, ruleID)
}

func normalizeMerchant(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}

func allocationWeights(inputs []*model.AllocationInput) ([]domain.Weight, error) {
	weights := make([]domain.Weight, 0, len(inputs))
	for _, input := range inputs {
		if input == nil || input.Weight <= 0 {
			return nil, fmt.Errorf("%w: allocation weights must be positive", ErrBadInput)
		}
		weights = append(weights, domain.Weight{PoolID: input.PoolID, Value: int64(input.Weight)})
	}
	if _, err := domain.Allocate(1, weights); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	return weights, nil
}
