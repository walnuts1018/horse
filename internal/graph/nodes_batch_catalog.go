package graph

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (b *nodeBatch) loadCatalogPlanningNodes(ctx context.Context) error {
	executor := b.executor
	households, idsByKind, indexesByKind := b.households, b.idsByKind, b.indexesByKind
	householdArgs, args, set := b.householdArgs, b.ids, b.set
	if values := idsByKind[nodeTypeAllocationRule]; len(values) > 0 {
		rules, err := models.AllocationRules.Query(sm.Where(psql.And(
			models.AllocationRules.Columns.HouseholdID.In(householdArgs...), models.AllocationRules.Columns.ID.In(args(nodeTypeAllocationRule)...),
		))).All(ctx, executor)
		if err != nil {
			return err
		}
		categoryIDs, conceptIDs, variantIDs := make([]bob.Expression, 0), make([]bob.Expression, 0), make([]bob.Expression, 0)
		ruleIDs := make([]bob.Expression, 0, len(rules))
		for _, rule := range rules {
			ruleIDs = append(ruleIDs, psql.Arg(rule.ID))
			if rule.CategoryID.Valid {
				categoryIDs = append(categoryIDs, psql.Arg(rule.CategoryID.V))
			}
			if rule.ProductConceptID.Valid {
				conceptIDs = append(conceptIDs, psql.Arg(rule.ProductConceptID.V))
			}
			if rule.ProductVariantID.Valid {
				variantIDs = append(variantIDs, psql.Arg(rule.ProductVariantID.V))
			}
		}
		categories := make(map[uuid.UUID]*models.Category)
		if len(categoryIDs) > 0 {
			rows, err := models.Categories.Query(sm.Where(psql.And(models.Categories.Columns.HouseholdID.In(householdArgs...), models.Categories.Columns.ID.In(categoryIDs...)))).All(ctx, executor)
			if err != nil {
				return err
			}
			for _, row := range rows {
				categories[row.ID] = row
			}
		}
		concepts := make(map[uuid.UUID]*models.ProductConcept)
		if len(conceptIDs) > 0 {
			rows, err := models.ProductConcepts.Query(sm.Where(psql.And(models.ProductConcepts.Columns.HouseholdID.In(householdArgs...), models.ProductConcepts.Columns.ID.In(conceptIDs...)))).All(ctx, executor)
			if err != nil {
				return err
			}
			for _, row := range rows {
				concepts[row.ID] = row
			}
		}
		variants := make(map[uuid.UUID]*models.ProductVariant)
		if len(variantIDs) > 0 {
			rows, err := models.ProductVariants.Query(sm.Where(psql.And(models.ProductVariants.Columns.HouseholdID.In(householdArgs...), models.ProductVariants.Columns.ID.In(variantIDs...)))).All(ctx, executor)
			if err != nil {
				return err
			}
			for _, row := range rows {
				variants[row.ID] = row
			}
		}
		var weights models.AllocationRuleWeightSlice
		if len(ruleIDs) > 0 {
			weights, err = models.AllocationRuleWeights.Query(sm.Where(models.AllocationRuleWeights.Columns.RuleID.In(ruleIDs...)), sm.OrderBy(models.AllocationRuleWeights.Columns.PoolID)).All(ctx, executor)
			if err != nil {
				return err
			}
			if err := weights.LoadPool(ctx, executor); err != nil {
				return err
			}
		}
		weightsByRule := make(map[uuid.UUID][]*models.AllocationRuleWeight)
		for _, weight := range weights {
			weightsByRule[weight.RuleID] = append(weightsByRule[weight.RuleID], weight)
		}
		for _, rule := range rules {
			value := &model.AllocationRule{ID: rule.ID.String(), HouseholdID: rule.HouseholdID.String(), Revision: int(rule.Revision), Allocations: make([]*model.Allocation, 0)}
			switch {
			case rule.CategoryID.Valid:
				category := categories[rule.CategoryID.V]
				if category == nil {
					return fmt.Errorf("allocation rule %s category is missing", rule.ID)
				}
				var parentID *string
				if category.ParentID.Valid {
					parentID = new(category.ParentID.V.String())
				}
				value.Target = &model.Category{ID: category.ID.String(), Name: category.Name, ParentID: parentID, Archived: category.Archived}
			case rule.ProductConceptID.Valid:
				concept := concepts[rule.ProductConceptID.V]
				if concept == nil {
					return fmt.Errorf("allocation rule %s product concept is missing", rule.ID)
				}
				var categoryID *string
				if concept.CategoryID.Valid {
					categoryID = new(concept.CategoryID.V.String())
				}
				value.Target = &model.ProductConcept{ID: concept.ID.String(), HouseholdID: concept.HouseholdID.String(), Name: concept.Name, NormalizedName: concept.NormalizedName, CategoryID: categoryID, Archived: concept.Archived, Revision: int(concept.Revision)}
			case rule.ProductVariantID.Valid:
				variant := variants[rule.ProductVariantID.V]
				if variant == nil {
					return fmt.Errorf("allocation rule %s product variant is missing", rule.ID)
				}
				var barcode *string
				if variant.Barcode.Valid {
					barcode = new(variant.Barcode.V)
				}
				value.Target = &model.ProductVariant{ID: variant.ID.String(), HouseholdID: variant.HouseholdID.String(), ConceptID: variant.ConceptID.String(), Name: variant.Name, NormalizedName: variant.NormalizedName, Brand: optionalProductAttribute(variant.Brand), PackageSize: optionalProductAttribute(variant.PackageSize), Barcode: barcode, Archived: variant.Archived, Revision: int(variant.Revision)}
			case rule.MerchantKey.Valid:
				value.Target = model.MerchantAllocationRuleTarget{Name: rule.MerchantKey.V}
			default:
				return fmt.Errorf("allocation rule %s has no target", rule.ID)
			}
			for _, weight := range weightsByRule[rule.ID] {
				if weight.R.Pool == nil {
					return fmt.Errorf("allocation rule weight %s has no pool", weight.PoolID)
				}
				value.Allocations = append(value.Allocations, &model.Allocation{PoolID: weight.PoolID.String(), PoolName: weight.R.Pool.Name, Weight: graphLong(weight.Weight)})
			}
			set(nodeTypeAllocationRule, value.ID, value)
		}
	}
	if len(idsByKind[nodeTypeProductConcept])+len(idsByKind[nodeTypeProductVariant])+len(idsByKind[nodeTypeProductAlias]) > 0 {
		conceptRows := models.ProductConceptSlice{}
		if len(idsByKind[nodeTypeProductConcept]) > 0 {
			rows, err := models.ProductConcepts.Query(sm.Where(psql.And(models.ProductConcepts.Columns.HouseholdID.In(householdArgs...), models.ProductConcepts.Columns.ID.In(args(nodeTypeProductConcept)...)))).All(ctx, executor)
			if err != nil {
				return err
			}
			conceptRows = rows
		}
		variantFilters := make([]bob.Expression, 0, 2)
		if len(idsByKind[nodeTypeProductVariant]) > 0 {
			variantFilters = append(variantFilters, models.ProductVariants.Columns.ID.In(args(nodeTypeProductVariant)...))
		}
		conceptIDs := make([]bob.Expression, 0, len(conceptRows))
		for _, concept := range conceptRows {
			conceptIDs = append(conceptIDs, psql.Arg(concept.ID))
		}
		if len(conceptIDs) > 0 {
			variantFilters = append(variantFilters, models.ProductVariants.Columns.ConceptID.In(conceptIDs...))
		}
		variantRows := models.ProductVariantSlice{}
		if len(variantFilters) > 0 {
			rows, err := models.ProductVariants.Query(sm.Where(psql.And(models.ProductVariants.Columns.HouseholdID.In(householdArgs...), psql.Or(variantFilters...))), sm.OrderBy(models.ProductVariants.Columns.NormalizedName), sm.OrderBy(models.ProductVariants.Columns.ID)).All(ctx, executor)
			if err != nil {
				return err
			}
			variantRows = rows
		}
		aliasFilters := make([]bob.Expression, 0, 2)
		if len(idsByKind[nodeTypeProductAlias]) > 0 {
			aliasFilters = append(aliasFilters, models.ProductAliases.Columns.ID.In(args(nodeTypeProductAlias)...))
		}
		variantIDs := make([]bob.Expression, 0, len(variantRows))
		for _, variant := range variantRows {
			variantIDs = append(variantIDs, psql.Arg(variant.ID))
		}
		if len(variantIDs) > 0 {
			aliasFilters = append(aliasFilters, models.ProductAliases.Columns.VariantID.In(variantIDs...))
		}
		aliasRows := models.ProductAliasSlice{}
		if len(aliasFilters) > 0 {
			rows, err := models.ProductAliases.Query(sm.Where(psql.And(models.ProductAliases.Columns.HouseholdID.In(householdArgs...), psql.Or(aliasFilters...))), sm.OrderBy(models.ProductAliases.Columns.MerchantKey), sm.OrderBy(models.ProductAliases.Columns.NormalizedAlias)).All(ctx, executor)
			if err != nil {
				return err
			}
			aliasRows = rows
		}
		aliasesByVariant := make(map[uuid.UUID][]*model.ProductAlias)
		aliasesByID := make(map[uuid.UUID]*model.ProductAlias)
		for _, row := range aliasRows {
			alias := &model.ProductAlias{ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), VariantID: row.VariantID.String(), MerchantKey: row.MerchantKey, Alias: row.AliasText, NormalizedAlias: row.NormalizedAlias, Revision: int(row.Revision)}
			aliasesByVariant[row.VariantID] = append(aliasesByVariant[row.VariantID], alias)
			aliasesByID[row.ID] = alias
		}
		variantsByConcept := make(map[uuid.UUID][]*model.ProductVariant)
		variantsByID := make(map[uuid.UUID]*model.ProductVariant)
		for _, row := range variantRows {
			variantAliases := make([]*model.ProductAlias, 0, len(aliasesByVariant[row.ID]))
			variantAliases = append(variantAliases, aliasesByVariant[row.ID]...)
			variant := &model.ProductVariant{ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), ConceptID: row.ConceptID.String(), Name: row.Name, NormalizedName: row.NormalizedName, Brand: optionalProductAttribute(row.Brand), PackageSize: optionalProductAttribute(row.PackageSize), Archived: row.Archived, Revision: int(row.Revision), Aliases: variantAliases}
			if row.Barcode.Valid {
				variant.Barcode = new(row.Barcode.V)
			}
			variantsByConcept[row.ConceptID] = append(variantsByConcept[row.ConceptID], variant)
			variantsByID[row.ID] = variant
		}
		for _, row := range conceptRows {
			conceptVariants := make([]*model.ProductVariant, 0, len(variantsByConcept[row.ID]))
			conceptVariants = append(conceptVariants, variantsByConcept[row.ID]...)
			value := &model.ProductConcept{ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), Name: row.Name, NormalizedName: row.NormalizedName, Archived: row.Archived, Revision: int(row.Revision), Variants: conceptVariants}
			if row.CategoryID.Valid {
				value.CategoryID = new(row.CategoryID.V.String())
			}
			set(nodeTypeProductConcept, value.ID, value)
		}
		for _, row := range variantRows {
			if _, requested := indexesByKind[nodeTypeProductVariant][row.ID]; requested {
				set(nodeTypeProductVariant, row.ID.String(), variantsByID[row.ID])
			}
		}
		for _, row := range aliasRows {
			if _, requested := indexesByKind[nodeTypeProductAlias][row.ID]; requested {
				set(nodeTypeProductAlias, row.ID.String(), aliasesByID[row.ID])
			}
		}
	}
	if values := idsByKind[nodeTypeTag]; len(values) > 0 {
		rows, err := models.Tags.Query(sm.Where(psql.And(
			models.Tags.Columns.HouseholdID.In(householdArgs...), models.Tags.Columns.ID.In(args(nodeTypeTag)...),
		))).All(ctx, executor)
		if err != nil {
			return err
		}
		for _, row := range rows {
			set(nodeTypeTag, row.ID.String(), tagModel(row))
		}
	}
	if values := idsByKind[nodeTypeRecurringExpense]; len(values) > 0 {
		rows, err := models.RecurringExpenses.Query(sm.Where(psql.And(
			models.RecurringExpenses.Columns.HouseholdID.In(householdArgs...), models.RecurringExpenses.Columns.ID.In(args(nodeTypeRecurringExpense)...),
		))).All(ctx, executor)
		if err != nil {
			return err
		}
		for _, row := range rows {
			set(nodeTypeRecurringExpense, row.ID.String(), recurringExpenseModel(row))
		}
	}
	if values := idsByKind[nodeTypeHouseholdInvitation]; len(values) > 0 {
		user, err := principal(ctx)
		if err != nil {
			return err
		}
		ownerHouseholdArgs := make([]bob.Expression, 0, len(households))
		for _, household := range households {
			if err := requireHouseholdOwner(ctx, executor, household.ID, user.MemberID); err != nil {
				if errors.Is(err, ErrNotFound) {
					continue
				}
				return err
			}
			ownerHouseholdArgs = append(ownerHouseholdArgs, psql.Arg(mustUUID(household.ID)))
		}
		if len(ownerHouseholdArgs) > 0 {
			rows, err := models.HouseholdInvitations.Query(sm.Where(psql.And(
				models.HouseholdInvitations.Columns.HouseholdID.In(ownerHouseholdArgs...), models.HouseholdInvitations.Columns.ID.In(args(nodeTypeHouseholdInvitation)...),
			))).All(ctx, executor)
			if err != nil {
				return err
			}
			now := time.Now()
			for _, row := range rows {
				set(nodeTypeHouseholdInvitation, row.ID.String(), householdInvitationModel(row, now))
			}
		}
	}
	return nil
}
