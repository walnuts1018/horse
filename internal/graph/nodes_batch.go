package graph

import (
	"context"

	"github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/walnuts1018/horse/internal/graph/model"
)

type nodeBatch struct {
	resolver      *queryResolver
	households    []*model.Household
	idsByKind     map[string][]uuid.UUID
	indexesByKind map[string]map[uuid.UUID][]int
	result        map[int]model.Node
	executor      bob.Executor
	householdArgs []bob.Expression
}

func (r *queryResolver) loadBatchNodesByDomain(ctx context.Context, households []*model.Household, idsByKind map[string][]uuid.UUID, indexesByKind map[string]map[uuid.UUID][]int) (map[int]model.Node, error) {
	batch := &nodeBatch{
		resolver: r, households: households, idsByKind: idsByKind, indexesByKind: indexesByKind,
		result: make(map[int]model.Node), executor: queryExecutor(r.DB), householdArgs: make([]bob.Expression, 0, len(households)),
	}
	for _, household := range households {
		batch.householdArgs = append(batch.householdArgs, psql.Arg(mustUUID(household.ID)))
	}
	if len(batch.householdArgs) == 0 {
		return batch.result, nil
	}
	for _, load := range []func(context.Context) error{batch.loadPurchaseNodes, batch.loadFinancialNodes, batch.loadCatalogPlanningNodes} {
		if err := load(ctx); err != nil {
			return nil, err
		}
	}
	return batch.result, nil
}

func (b *nodeBatch) ids(kind string) []bob.Expression {
	values := make([]bob.Expression, 0, len(b.idsByKind[kind]))
	for _, id := range b.idsByKind[kind] {
		values = append(values, psql.Arg(id))
	}
	return values
}

func (b *nodeBatch) set(kind, id string, node model.Node) {
	parsedID, err := uuid.FromString(id)
	if err != nil {
		return
	}
	for _, index := range b.indexesByKind[kind][parsedID] {
		b.result[index] = node
	}
}
