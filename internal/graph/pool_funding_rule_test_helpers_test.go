package graph

import "github.com/walnuts1018/horse/internal/graph/model"

func personalPoolFundingConfiguration(ownerMemberID string) *model.PoolFundingRuleConfigurationInput {
	return &model.PoolFundingRuleConfigurationInput{
		Personal: &model.PersonalPoolFundingRuleInput{OwnerMemberID: ownerMemberID},
	}
}

func sharedPoolFundingConfiguration(shares []*model.PoolFundingShareInput, policy model.SharedPoolSettlementPolicy) *model.PoolFundingRuleConfigurationInput {
	return &model.PoolFundingRuleConfigurationInput{
		Shared: &model.SharedPoolFundingRuleInput{FundingShares: shares, SettlementPolicy: policy},
	}
}
