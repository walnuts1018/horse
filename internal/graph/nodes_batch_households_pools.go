package graph

import (
	"context"

	"github.com/gofrs/uuid/v5"
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
	householdIDs := make([]uuid.UUID, 0, len(pools))
	seenHouseholds := make(map[uuid.UUID]struct{}, len(pools))
	for _, pool := range pools {
		if _, exists := seenHouseholds[pool.HouseholdID]; exists {
			continue
		}
		seenHouseholds[pool.HouseholdID] = struct{}{}
		householdIDs = append(householdIDs, pool.HouseholdID)
	}
	if len(householdIDs) == 0 {
		return nil
	}
	households, err := loadHouseholds(ctx, b.resolver.DB, householdIDs, householdFields{pools: true})
	if err != nil {
		return err
	}
	for _, pool := range pools {
		household := households[pool.HouseholdID]
		if household == nil {
			continue
		}
		value := findValue(household.PoolVariants, func(value model.PoolVariant) bool { return value.GetID() == pool.ID.String() })
		b.set(nodeTypePool, pool.ID.String(), value)
	}
	return nil
}
