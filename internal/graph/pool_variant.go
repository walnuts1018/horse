package graph

import "github.com/walnuts1018/horse/internal/graph/model"

func poolVariant(pool *model.Pool) model.PoolVariant {
	if pool == nil {
		return nil
	}
	switch pool.Kind {
	case model.PoolKindPersonal:
		if pool.OwnerMemberID == nil {
			return &model.UnconfiguredPersonalPool{Pool: pool}
		}
		return &model.PersonalPool{Pool: pool}
	case model.PoolKindShared:
		if pool.SettlementPolicy == nil || len(pool.FundingShares) == 0 {
			return &model.UnconfiguredSharedPool{Pool: pool}
		}
		return &model.SharedPool{Pool: pool}
	default:
		return nil
	}
}

func poolData(variant model.PoolVariant) *model.Pool {
	switch pool := variant.(type) {
	case *model.PersonalPool:
		return pool.Pool
	case *model.SharedPool:
		return pool.Pool
	case *model.UnconfiguredPersonalPool:
		return pool.Pool
	case *model.UnconfiguredSharedPool:
		return pool.Pool
	default:
		return nil
	}
}
