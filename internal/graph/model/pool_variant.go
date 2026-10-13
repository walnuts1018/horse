package model

type Pool struct {
	ID                 string                      `json:"id"`
	Name               string                      `json:"name"`
	Kind               PoolKind                    `json:"kind"`
	DefaultShareWeight Long                        `json:"defaultShareWeight"`
	OwnerMemberID      *string                     `json:"ownerMemberId,omitempty"`
	FundingShares      []*PoolFundingShare         `json:"fundingShares"`
	SettlementPolicy   *SharedPoolSettlementPolicy `json:"settlementPolicy,omitempty"`
	FundingRuleVersion int                         `json:"fundingRuleVersion"`
}

func (Pool) IsNode() {}

type PoolVariant interface {
	IsNode()
	IsPool()
	GetID() string
	GetName() string
	GetDefaultShareWeight() Long
	GetFundingShares() []*PoolFundingShare
	GetFundingRuleVersion() int
}

type PersonalPool struct {
	*Pool
}

func (PersonalPool) IsPool()                                    {}
func (pool PersonalPool) GetID() string                         { return pool.Pool.ID }
func (pool PersonalPool) GetName() string                       { return pool.Pool.Name }
func (pool PersonalPool) GetDefaultShareWeight() Long           { return pool.Pool.DefaultShareWeight }
func (pool PersonalPool) GetFundingShares() []*PoolFundingShare { return pool.Pool.FundingShares }
func (pool PersonalPool) GetFundingRuleVersion() int            { return pool.Pool.FundingRuleVersion }

type SharedPool struct {
	*Pool
}

func (SharedPool) IsPool()                                    {}
func (pool SharedPool) GetID() string                         { return pool.Pool.ID }
func (pool SharedPool) GetName() string                       { return pool.Pool.Name }
func (pool SharedPool) GetDefaultShareWeight() Long           { return pool.Pool.DefaultShareWeight }
func (pool SharedPool) GetFundingShares() []*PoolFundingShare { return pool.Pool.FundingShares }
func (pool SharedPool) GetFundingRuleVersion() int            { return pool.Pool.FundingRuleVersion }

type UnconfiguredPersonalPool struct {
	*Pool
}

func (UnconfiguredPersonalPool) IsPool()              {}
func (pool UnconfiguredPersonalPool) GetID() string   { return pool.Pool.ID }
func (pool UnconfiguredPersonalPool) GetName() string { return pool.Pool.Name }
func (pool UnconfiguredPersonalPool) GetDefaultShareWeight() Long {
	return pool.Pool.DefaultShareWeight
}
func (pool UnconfiguredPersonalPool) GetFundingShares() []*PoolFundingShare {
	return pool.Pool.FundingShares
}
func (pool UnconfiguredPersonalPool) GetFundingRuleVersion() int { return pool.Pool.FundingRuleVersion }

type UnconfiguredSharedPool struct {
	*Pool
}

func (UnconfiguredSharedPool) IsPool()                          {}
func (pool UnconfiguredSharedPool) GetID() string               { return pool.Pool.ID }
func (pool UnconfiguredSharedPool) GetName() string             { return pool.Pool.Name }
func (pool UnconfiguredSharedPool) GetDefaultShareWeight() Long { return pool.Pool.DefaultShareWeight }
func (pool UnconfiguredSharedPool) GetFundingShares() []*PoolFundingShare {
	return pool.Pool.FundingShares
}
func (pool UnconfiguredSharedPool) GetFundingRuleVersion() int { return pool.Pool.FundingRuleVersion }
