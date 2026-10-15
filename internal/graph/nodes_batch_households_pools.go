package graph

import (
	"context"

	"github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (b *nodeBatch) loadHouseholdNodes(ctx context.Context) error {
	if len(b.idsByKind[nodeTypeHousehold]) == 0 {
		return nil
	}
	requested := make(map[uuid.UUID]struct{}, len(b.idsByKind[nodeTypeHousehold]))
	for _, id := range b.idsByKind[nodeTypeHousehold] {
		requested[id] = struct{}{}
	}
	authorizedIDs := make([]uuid.UUID, 0, len(requested))
	for _, household := range b.households {
		id := mustUUID(household.ID)
		if _, ok := requested[id]; ok {
			authorizedIDs = append(authorizedIDs, id)
		}
	}
	households, err := loadHouseholdSummaries(ctx, b.resolver.DB, authorizedIDs)
	if err != nil {
		return err
	}
	for id, household := range households {
		b.set(nodeTypeHousehold, id.String(), household)
	}
	return nil
}

func (b *nodeBatch) loadPoolNodes(ctx context.Context) error {
	if len(b.idsByKind[nodeTypePool]) == 0 {
		return nil
	}
	pools, err := models.Pools.Query(sm.Where(psql.And(
		models.Pools.Columns.HouseholdID.In(b.householdArgs...),
		models.Pools.Columns.ID.In(b.ids(nodeTypePool)...),
	))).All(ctx, b.executor)
	if err != nil {
		return err
	}
	if len(pools) == 0 {
		return nil
	}
	poolByID := make(map[uuid.UUID]*model.Pool, len(pools))
	for _, pool := range pools {
		poolByID[pool.ID] = &model.Pool{
			ID: pool.ID.String(), Name: pool.Name, Kind: model.PoolKind(pool.Kind),
			FundingRuleVersion: int(pool.FundingRuleVersion), FundingShares: make([]*model.PoolFundingShare, 0),
		}
	}

	// Poolノードの現在のデフォルト配分比率を選ぶため、Household全体ではなくバージョンだけを読み込む。
	households, err := models.Households.Query(
		sm.Where(models.Households.Columns.ID.In(b.householdArgs...)),
		sm.Columns(models.Households.Columns.Only("id", "default_allocation_version")),
	).All(ctx, b.executor)
	if err != nil {
		return err
	}
	allocationVersions := make(map[uuid.UUID]int64, len(households))
	for _, household := range households {
		allocationVersions[household.ID] = household.DefaultAllocationVersion
	}
	allocationFilters := make([]bob.Expression, 0, len(pools))
	for _, pool := range pools {
		version, ok := allocationVersions[pool.HouseholdID]
		if !ok {
			continue
		}
		allocationFilters = append(allocationFilters, psql.And(
			models.AllocationDefaults.Columns.HouseholdID.EQ(psql.Arg(pool.HouseholdID)),
			models.AllocationDefaults.Columns.PoolID.EQ(psql.Arg(pool.ID)),
			models.AllocationDefaults.Columns.Version.EQ(psql.Arg(version)),
		))
	}
	if len(allocationFilters) > 0 {
		defaults, err := models.AllocationDefaults.Query(sm.Where(psql.Or(allocationFilters...))).All(ctx, b.executor)
		if err != nil {
			return err
		}
		for _, allocation := range defaults {
			if pool := poolByID[allocation.PoolID]; pool != nil {
				pool.DefaultShareWeight = graphLong(allocation.Weight)
			}
		}
	}

	fundingRuleFilters := make([]bob.Expression, 0, len(pools))
	for _, pool := range pools {
		fundingRuleFilters = append(fundingRuleFilters, psql.And(
			models.PoolFundingRules.Columns.HouseholdID.EQ(psql.Arg(pool.HouseholdID)),
			models.PoolFundingRules.Columns.PoolID.EQ(psql.Arg(pool.ID)),
			models.PoolFundingRules.Columns.Version.EQ(psql.Arg(pool.FundingRuleVersion)),
		))
	}
	fundingRules, err := models.PoolFundingRules.Query(sm.Where(psql.Or(fundingRuleFilters...))).All(ctx, b.executor)
	if err != nil {
		return err
	}
	activeRules := make(map[uuid.UUID]*models.PoolFundingRule, len(pools))
	for _, rule := range fundingRules {
		pool := poolByID[rule.PoolID]
		if pool != nil && rule.Version == int32(pool.FundingRuleVersion) {
			activeRules[rule.PoolID] = rule
			if rule.OwnerMemberID.Valid {
				pool.OwnerMemberID = new(rule.OwnerMemberID.V.String())
			}
			if rule.SettlementPolicy.Valid {
				pool.SettlementPolicy = new(model.SharedPoolSettlementPolicy(rule.SettlementPolicy.V))
			}
		}
	}
	fundingShareFilters := make([]bob.Expression, 0, len(activeRules))
	for poolID, rule := range activeRules {
		fundingShareFilters = append(fundingShareFilters, psql.And(
			models.PoolFundingShares.Columns.HouseholdID.EQ(psql.Arg(rule.HouseholdID)),
			models.PoolFundingShares.Columns.PoolID.EQ(psql.Arg(poolID)),
			models.PoolFundingShares.Columns.Version.EQ(psql.Arg(rule.Version)),
		))
	}
	fundingShares := models.PoolFundingShareSlice{}
	if len(fundingShareFilters) > 0 {
		fundingShares, err = models.PoolFundingShares.Query(
			sm.Where(psql.Or(fundingShareFilters...)),
			sm.OrderBy(models.PoolFundingShares.Columns.PoolID),
			sm.OrderBy(models.PoolFundingShares.Columns.MemberID),
		).All(ctx, b.executor)
		if err != nil {
			return err
		}
	}
	memberIDs := make([]bob.Expression, 0)
	seenMembers := make(map[uuid.UUID]struct{})
	for _, share := range fundingShares {
		rule, pool := activeRules[share.PoolID], poolByID[share.PoolID]
		if rule == nil || pool == nil || share.Version != rule.Version {
			continue
		}
		if _, exists := seenMembers[share.MemberID]; !exists {
			seenMembers[share.MemberID] = struct{}{}
			memberIDs = append(memberIDs, psql.Arg(share.MemberID))
		}
	}
	memberNames := make(map[uuid.UUID]string, len(memberIDs))
	if len(memberIDs) > 0 {
		members, err := models.Members.Query(sm.Where(models.Members.Columns.ID.In(memberIDs...))).All(ctx, b.executor)
		if err != nil {
			return err
		}
		for _, member := range members {
			memberNames[member.ID] = member.DisplayName
		}
	}
	for _, share := range fundingShares {
		rule, pool := activeRules[share.PoolID], poolByID[share.PoolID]
		if rule != nil && pool != nil && share.Version == rule.Version {
			pool.FundingShares = append(pool.FundingShares, &model.PoolFundingShare{
				MemberID: share.MemberID.String(), MemberName: memberNames[share.MemberID], Weight: graphLong(share.Weight),
			})
		}
	}
	for _, pool := range pools {
		b.set(nodeTypePool, pool.ID.String(), poolVariant(poolByID[pool.ID]))
	}
	return nil
}
