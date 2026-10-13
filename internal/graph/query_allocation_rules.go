package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (r *queryResolver) allocationRules(ctx context.Context, householdID string, after, before *string, first, last *int) (*model.AllocationRuleConnection, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	pageSize, backward, err := pageDirection(after, before, first, last)
	if err != nil {
		return nil, err
	}
	parsedID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	executor := queryExecutor(r.DB)
	filters := []bob.Expression{models.AllocationRules.Columns.HouseholdID.EQ(psql.Arg(parsedID))}
	count, err := models.AllocationRules.Query(sm.Where(filters[0])).Count(ctx, executor)
	if err != nil {
		return nil, err
	}
	for _, boundary := range []struct {
		cursor   *string
		isBefore bool
	}{{cursor: after}, {cursor: before, isBefore: true}} {
		id, err := cursorID(boundary.cursor, "AllocationRule")
		if err != nil {
			return nil, err
		}
		if id == nil {
			continue
		}
		parsedCursorID, err := gofrsuuid.FromString(*id)
		if err != nil {
			return nil, fmt.Errorf("parse allocation rule cursor: %w", err)
		}
		cursor, err := models.AllocationRules.Query(sm.Where(psql.And(
			models.AllocationRules.Columns.ID.EQ(psql.Arg(parsedCursorID)),
			models.AllocationRules.Columns.HouseholdID.EQ(psql.Arg(parsedID)),
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		filters = append(filters, allocationRuleCursorFilter(cursor, boundary.isBefore))
	}
	var rules []*models.AllocationRule
	if backward {
		rules, err = models.AllocationRules.Query(
			sm.Where(psql.And(filters...)),
			sm.OrderBy(models.AllocationRules.Columns.CategoryID).Desc().NullsFirst(),
			sm.OrderBy(models.AllocationRules.Columns.MerchantKey).Desc().NullsFirst(),
			sm.OrderBy(models.AllocationRules.Columns.ID).Desc(),
			sm.Limit(pageSize+1),
		).All(ctx, executor)
	} else {
		rules, err = models.AllocationRules.Query(
			sm.Where(psql.And(filters...)),
			sm.OrderBy(models.AllocationRules.Columns.CategoryID).NullsLast(),
			sm.OrderBy(models.AllocationRules.Columns.MerchantKey).NullsLast(),
			sm.OrderBy(models.AllocationRules.Columns.ID),
			sm.Limit(pageSize+1),
		).All(ctx, executor)
	}
	if err != nil {
		return nil, err
	}
	hasNextPage := len(rules) > pageSize
	if hasNextPage {
		rules = rules[:pageSize]
	}
	if backward {
		slices.Reverse(rules)
	}
	connection := &model.AllocationRuleConnection{Edges: make([]*model.AllocationRuleEdge, 0, len(rules)), TotalCount: int(count), PageInfo: &model.PageInfo{HasNextPage: backward && before != nil && *before != "" || !backward && hasNextPage, HasPreviousPage: backward && hasNextPage || !backward && after != nil && *after != ""}}
	for _, rule := range rules {
		loaded, err := loadAllocationRule(ctx, r.DB, rule.ID.String())
		if err != nil {
			return nil, err
		}
		connection.Edges = append(connection.Edges, &model.AllocationRuleEdge{Cursor: encodeCursor("AllocationRule", rule.ID.String()), Node: loaded})
	}
	if len(rules) > 0 {
		start, end := encodeCursor("AllocationRule", rules[0].ID.String()), encodeCursor("AllocationRule", rules[len(rules)-1].ID.String())
		connection.PageInfo.StartCursor, connection.PageInfo.EndCursor = &start, &end
	}
	return connection, nil
}

func allocationRuleCursorFilter(cursor *models.AllocationRule, isBefore bool) bob.Expression {
	if cursor.CategoryID.Valid {
		if isBefore {
			return psql.Or(
				models.AllocationRules.Columns.CategoryID.LT(psql.Arg(cursor.CategoryID.V)),
				psql.And(models.AllocationRules.Columns.CategoryID.EQ(psql.Arg(cursor.CategoryID.V)), models.AllocationRules.Columns.ID.LT(psql.Arg(cursor.ID))),
			)
		}
		return psql.Or(
			models.AllocationRules.Columns.CategoryID.GT(psql.Arg(cursor.CategoryID.V)),
			psql.And(models.AllocationRules.Columns.CategoryID.EQ(psql.Arg(cursor.CategoryID.V)), models.AllocationRules.Columns.ID.GT(psql.Arg(cursor.ID))),
			models.AllocationRules.Columns.CategoryID.IsNull(),
		)
	}
	if cursor.MerchantKey.Valid {
		if isBefore {
			return psql.And(
				models.AllocationRules.Columns.CategoryID.IsNull(),
				models.AllocationRules.Columns.MerchantKey.IsNotNull(),
				psql.Or(
					models.AllocationRules.Columns.MerchantKey.LT(psql.Arg(cursor.MerchantKey.V)),
					psql.And(models.AllocationRules.Columns.MerchantKey.EQ(psql.Arg(cursor.MerchantKey.V)), models.AllocationRules.Columns.ID.LT(psql.Arg(cursor.ID))),
				),
			)
		}
		return psql.And(
			models.AllocationRules.Columns.CategoryID.IsNull(),
			psql.Or(
				models.AllocationRules.Columns.MerchantKey.GT(psql.Arg(cursor.MerchantKey.V)),
				psql.And(models.AllocationRules.Columns.MerchantKey.EQ(psql.Arg(cursor.MerchantKey.V)), models.AllocationRules.Columns.ID.GT(psql.Arg(cursor.ID))),
				models.AllocationRules.Columns.MerchantKey.IsNull(),
			),
		)
	}
	if isBefore {
		return psql.And(
			models.AllocationRules.Columns.CategoryID.IsNull(),
			models.AllocationRules.Columns.MerchantKey.IsNull(),
			models.AllocationRules.Columns.ID.LT(psql.Arg(cursor.ID)),
		)
	}
	return psql.And(
		models.AllocationRules.Columns.CategoryID.IsNull(),
		models.AllocationRules.Columns.MerchantKey.IsNull(),
		models.AllocationRules.Columns.ID.GT(psql.Arg(cursor.ID)),
	)
}

func loadAllocationRule(ctx context.Context, db *pgxpool.Pool, ruleID string) (*model.AllocationRule, error) {
	id, err := gofrsuuid.FromString(ruleID)
	if err != nil {
		return nil, fmt.Errorf("parse allocation rule id: %w", err)
	}
	executor := queryExecutor(db)
	rule, err := models.AllocationRules.Query(sm.Where(models.AllocationRules.Columns.ID.EQ(psql.Arg(id)))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	result := &model.AllocationRule{ID: rule.ID.String(), HouseholdID: rule.HouseholdID.String(), Revision: int(rule.Revision)}
	if rule.CategoryID.Valid {
		category, err := models.Categories.Query(sm.Where(psql.And(
			models.Categories.Columns.ID.EQ(psql.Arg(rule.CategoryID.V)),
			models.Categories.Columns.HouseholdID.EQ(psql.Arg(rule.HouseholdID)),
		))).One(ctx, executor)
		if err != nil {
			return nil, err
		}
		var parentID *string
		if category.ParentID.Valid {
			parentID = new(category.ParentID.V.String())
		}
		result.Target = &model.Category{ID: category.ID.String(), Name: category.Name, ParentID: parentID, Archived: category.Archived}
	} else if rule.ProductConceptID.Valid {
		concept, err := models.ProductConcepts.Query(sm.Where(psql.And(
			models.ProductConcepts.Columns.ID.EQ(psql.Arg(rule.ProductConceptID.V)),
			models.ProductConcepts.Columns.HouseholdID.EQ(psql.Arg(rule.HouseholdID)),
		))).One(ctx, executor)
		if err != nil {
			return nil, err
		}
		var categoryID *string
		if concept.CategoryID.Valid {
			categoryID = new(concept.CategoryID.V.String())
		}
		result.Target = &model.ProductConcept{
			ID: concept.ID.String(), HouseholdID: concept.HouseholdID.String(), Name: concept.Name,
			NormalizedName: concept.NormalizedName, CategoryID: categoryID, Archived: concept.Archived,
			Revision: int(concept.Revision),
		}
	} else if rule.ProductVariantID.Valid {
		variant, err := models.ProductVariants.Query(sm.Where(psql.And(
			models.ProductVariants.Columns.ID.EQ(psql.Arg(rule.ProductVariantID.V)),
			models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(rule.HouseholdID)),
		))).One(ctx, executor)
		if err != nil {
			return nil, err
		}
		var barcode *string
		if variant.Barcode.Valid {
			barcode = new(variant.Barcode.V)
		}
		result.Target = &model.ProductVariant{
			ID: variant.ID.String(), HouseholdID: variant.HouseholdID.String(), ConceptID: variant.ConceptID.String(),
			Name: variant.Name, NormalizedName: variant.NormalizedName, Brand: new(variant.Brand),
			PackageSize: new(variant.PackageSize), Barcode: barcode, Archived: variant.Archived,
			Revision: int(variant.Revision),
		}
	} else if rule.MerchantKey.Valid {
		result.Target = model.MerchantAllocationRuleTarget{Name: rule.MerchantKey.V}
	} else {
		return nil, fmt.Errorf("allocation rule %s has no target", rule.ID)
	}
	weights, err := models.AllocationRuleWeights.Query(sm.Where(models.AllocationRuleWeights.Columns.RuleID.EQ(psql.Arg(id))), sm.OrderBy(models.AllocationRuleWeights.Columns.PoolID)).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	if err := weights.LoadPool(ctx, executor); err != nil {
		return nil, err
	}
	result.Allocations = make([]*model.Allocation, 0, len(weights))
	for _, weight := range weights {
		if weight.R.Pool == nil {
			return nil, fmt.Errorf("allocation rule weight %s has no pool", weight.PoolID)
		}
		result.Allocations = append(result.Allocations, &model.Allocation{PoolID: weight.PoolID.String(), PoolName: weight.R.Pool.Name, Weight: graphLong(weight.Weight)})
	}
	return result, nil
}
