package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
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
	if _, err := domain.Allocate(1, weights); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	householdUUID, err := gofrsuuid.FromString(input.HouseholdID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid household id", ErrBadInput)
	}
	executor := bobpgx.NewTx(tx, nil)
	householdRow, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID))), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return nil, err
	}
	version := householdRow.DefaultAllocationVersion
	for _, weight := range weights {
		poolUUID, parseErr := gofrsuuid.FromString(weight.PoolID)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: invalid pool id", ErrBadInput)
		}
		_, err := models.Pools.Query(sm.Where(psql.And(
			models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			models.Pools.Columns.ID.EQ(psql.Arg(poolUUID)),
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
	}
	newVersion := version + 1
	newVersionValue := newVersion
	for _, weight := range weights {
		poolUUID, _ := gofrsuuid.FromString(weight.PoolID)
		weightValue := weight.Value
		if _, err := models.AllocationDefaults.Insert(&models.AllocationDefaultSetter{
			HouseholdID: &householdUUID, Version: &newVersionValue, PoolID: &poolUUID, Weight: &weightValue,
		}).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	newRevision, updatedAt := householdRow.Revision+1, time.Now().UTC()
	if _, err := models.Households.Update((&models.HouseholdSetter{
		DefaultAllocationVersion: &newVersionValue, Revision: &newRevision, UpdatedAt: &updatedAt,
	}).UpdateMod(), um.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "allocation.default.updated", input.HouseholdID, newVersion, []string{fieldDefaultAllocation}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
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
	target := input.Target
	scopeCount := 0
	for _, selected := range []*string{target.ProductVariantID, target.ProductConceptID, target.CategoryID, target.Merchant} {
		if selected != nil {
			scopeCount++
		}
	}
	if scopeCount != 1 {
		return nil, fmt.Errorf("%w: select exactly one product, category, or merchant rule key", ErrBadInput)
	}
	weights, err := allocationWeights(input.Allocations)
	if err != nil {
		return nil, err
	}
	if _, err := domain.Allocate(1, weights); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	merchantKey := ""
	if target.Merchant != nil {
		merchantKey = normalizeMerchant(*target.Merchant)
		if merchantKey == "" || len(merchantKey) > 240 {
			return nil, fmt.Errorf("%w: invalid merchant rule key", ErrBadInput)
		}
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	householdUUID, err := gofrsuuid.FromString(input.HouseholdID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid household id", ErrBadInput)
	}
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID))), sm.ForUpdate()).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	for _, weight := range weights {
		poolUUID, parseErr := gofrsuuid.FromString(weight.PoolID)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: invalid pool id", ErrBadInput)
		}
		_, err := models.Pools.Query(sm.Where(psql.And(
			models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			models.Pools.Columns.ID.EQ(psql.Arg(poolUUID)),
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
	}
	for _, referenceID := range []*string{target.CategoryID, target.ProductConceptID, target.ProductVariantID} {
		if referenceID == nil {
			continue
		}
		if _, err := gofrsuuid.FromString(*referenceID); err != nil {
			return nil, fmt.Errorf("%w: invalid allocation rule reference id", ErrBadInput)
		}
	}
	if target.CategoryID != nil {
		id := generatedUUID(*target.CategoryID)
		if _, err := models.Categories.Query(sm.Where(psql.And(models.Categories.Columns.ID.EQ(psql.Arg(id)), models.Categories.Columns.HouseholdID.EQ(psql.Arg(householdUUID))))).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		} else if err != nil {
			return nil, err
		}
	}
	if target.ProductConceptID != nil {
		id := generatedUUID(*target.ProductConceptID)
		if _, err := models.ProductConcepts.Query(sm.Where(psql.And(models.ProductConcepts.Columns.ID.EQ(psql.Arg(id)), models.ProductConcepts.Columns.HouseholdID.EQ(psql.Arg(householdUUID))))).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		} else if err != nil {
			return nil, err
		}
	}
	if target.ProductVariantID != nil {
		id := generatedUUID(*target.ProductVariantID)
		if _, err := models.ProductVariants.Query(sm.Where(psql.And(models.ProductVariants.Columns.ID.EQ(psql.Arg(id)), models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdUUID))))).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		} else if err != nil {
			return nil, err
		}
	}
	var ruleRow *models.AllocationRule
	var ruleErr error
	if target.CategoryID != nil {
		id := generatedUUID(*target.CategoryID)
		ruleRow, ruleErr = models.AllocationRules.Query(sm.Where(psql.And(models.AllocationRules.Columns.HouseholdID.EQ(psql.Arg(householdUUID)), models.AllocationRules.Columns.CategoryID.EQ(psql.Arg(id)))), sm.ForUpdate()).One(ctx, executor)
	} else if target.Merchant != nil {
		ruleRow, ruleErr = models.AllocationRules.Query(sm.Where(psql.And(models.AllocationRules.Columns.HouseholdID.EQ(psql.Arg(householdUUID)), models.AllocationRules.Columns.MerchantKey.EQ(psql.Arg(merchantKey)))), sm.ForUpdate()).One(ctx, executor)
	} else if target.ProductConceptID != nil {
		id := generatedUUID(*target.ProductConceptID)
		ruleRow, ruleErr = models.AllocationRules.Query(sm.Where(psql.And(models.AllocationRules.Columns.HouseholdID.EQ(psql.Arg(householdUUID)), models.AllocationRules.Columns.ProductConceptID.EQ(psql.Arg(id)))), sm.ForUpdate()).One(ctx, executor)
	} else {
		id := generatedUUID(*target.ProductVariantID)
		ruleRow, ruleErr = models.AllocationRules.Query(sm.Where(psql.And(models.AllocationRules.Columns.HouseholdID.EQ(psql.Arg(householdUUID)), models.AllocationRules.Columns.ProductVariantID.EQ(psql.Arg(id)))), sm.ForUpdate()).One(ctx, executor)
	}
	ruleID, revision := "", int64(1)
	if errors.Is(ruleErr, sql.ErrNoRows) {
		ruleID = newID()
		ruleUUID := generatedUUID(ruleID)
		categoryValue, conceptValue, variantValue := sql.Null[gofrsuuid.UUID]{}, sql.Null[gofrsuuid.UUID]{}, sql.Null[gofrsuuid.UUID]{}
		merchantValue := sql.Null[string]{}
		if target.CategoryID != nil {
			categoryValue = sql.Null[gofrsuuid.UUID]{V: generatedUUID(*target.CategoryID), Valid: true}
		}
		if target.ProductConceptID != nil {
			conceptValue = sql.Null[gofrsuuid.UUID]{V: generatedUUID(*target.ProductConceptID), Valid: true}
		}
		if target.ProductVariantID != nil {
			variantValue = sql.Null[gofrsuuid.UUID]{V: generatedUUID(*target.ProductVariantID), Valid: true}
		}
		if target.Merchant != nil {
			merchantValue = sql.Null[string]{V: merchantKey, Valid: true}
		}
		if _, err := models.AllocationRules.Insert(&models.AllocationRuleSetter{
			ID: &ruleUUID, HouseholdID: &householdUUID, CategoryID: &categoryValue, MerchantKey: &merchantValue,
			ProductConceptID: &conceptValue, ProductVariantID: &variantValue, Revision: &revision,
		}).Exec(ctx, executor); err != nil {
			return nil, err
		}
	} else {
		if ruleErr != nil {
			return nil, ruleErr
		}
		ruleUUID := ruleRow.ID
		ruleID, revision = ruleUUID.String(), ruleRow.Revision+1
		updatedAt := time.Now().UTC()
		if _, err := models.AllocationRules.Update((&models.AllocationRuleSetter{Revision: &revision, UpdatedAt: &updatedAt}).UpdateMod(), um.Where(models.AllocationRules.Columns.ID.EQ(psql.Arg(ruleUUID)))).Exec(ctx, executor); err != nil {
			return nil, err
		}
		if _, err := models.AllocationRuleWeights.Delete(dm.Where(models.AllocationRuleWeights.Columns.RuleID.EQ(psql.Arg(ruleUUID)))).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	for _, weight := range weights {
		poolUUID, _ := gofrsuuid.FromString(weight.PoolID)
		ruleUUID := generatedUUID(ruleID)
		weightValue := weight.Value
		if _, err := models.AllocationRuleWeights.Insert(&models.AllocationRuleWeightSetter{HouseholdID: &householdUUID, RuleID: &ruleUUID, PoolID: &poolUUID, Weight: &weightValue}).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	changedFields := []string{fieldAllocations}
	if target.ProductVariantID != nil {
		changedFields = append(changedFields, fieldProductVariantID)
	} else if target.ProductConceptID != nil {
		changedFields = append(changedFields, "productConceptId")
	} else if target.CategoryID != nil {
		changedFields = append(changedFields, fieldCategoryID)
	} else {
		changedFields = append(changedFields, fieldMerchant)
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "allocation.rule.updated", ruleID, revision, changedFields, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadAllocationRule(ctx, r.DB, ruleID)
}

func normalizeMerchant(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}

func allocationRuleWeights(ctx context.Context, tx pgx.Tx, householdID string, categoryID, productVariantID *string, merchant string) ([]domain.Weight, *string, int64, error) {
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, nil, 0, err
	}
	executor := bobpgx.NewTx(tx, nil)
	var candidates []*models.AllocationRule
	appendRule := func(expression psql.Expression) error {
		rule, err := models.AllocationRules.Query(sm.Where(psql.And(models.AllocationRules.Columns.HouseholdID.EQ(psql.Arg(householdUUID)), expression))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		candidates = append(candidates, rule)
		return nil
	}
	var conceptID *gofrsuuid.UUID
	if productVariantID != nil {
		variantUUID, parseErr := gofrsuuid.FromString(*productVariantID)
		if parseErr != nil {
			return nil, nil, 0, parseErr
		}
		variant, variantErr := models.ProductVariants.Query(sm.Where(psql.And(
			models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			models.ProductVariants.Columns.ID.EQ(psql.Arg(variantUUID)),
		))).One(ctx, executor)
		if variantErr == nil {
			concept := variant.ConceptID
			conceptID = &concept
		} else if variantErr != nil && !errors.Is(variantErr, sql.ErrNoRows) {
			return nil, nil, 0, variantErr
		}
		if err := appendRule(models.AllocationRules.Columns.ProductVariantID.EQ(psql.Arg(variantUUID))); err != nil {
			return nil, nil, 0, err
		}
	}
	if conceptID != nil {
		if err := appendRule(models.AllocationRules.Columns.ProductConceptID.EQ(psql.Arg(*conceptID))); err != nil {
			return nil, nil, 0, err
		}
	}
	if categoryID != nil {
		categoryUUID, parseErr := gofrsuuid.FromString(*categoryID)
		if parseErr != nil {
			return nil, nil, 0, parseErr
		}
		if err := appendRule(models.AllocationRules.Columns.CategoryID.EQ(psql.Arg(categoryUUID))); err != nil {
			return nil, nil, 0, err
		}
	}
	if merchant != "" {
		if err := appendRule(models.AllocationRules.Columns.MerchantKey.EQ(psql.Arg(merchant))); err != nil {
			return nil, nil, 0, err
		}
	}
	if len(candidates) == 0 {
		return nil, nil, 0, nil
	}
	rule := candidates[0]
	weightsRows, err := models.AllocationRuleWeights.Query(sm.Where(models.AllocationRuleWeights.Columns.RuleID.EQ(psql.Arg(rule.ID))), sm.OrderBy(models.AllocationRuleWeights.Columns.PoolID)).All(ctx, executor)
	if err != nil {
		return nil, nil, 0, err
	}
	weights := make([]domain.Weight, 0, len(weightsRows))
	for _, row := range weightsRows {
		weights = append(weights, domain.Weight{PoolID: row.PoolID.String(), Value: row.Weight})
	}
	if len(weights) == 0 {
		return nil, nil, 0, errors.New("allocation rule has no weights")
	}
	ruleID := rule.ID.String()
	return weights, &ruleID, rule.Revision, nil
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
