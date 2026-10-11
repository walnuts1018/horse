package graph

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/graph/model"
)

const maxMoney int64 = 1_000_000_000_000

func (r *mutationResolver) createHousehold(ctx context.Context, input model.CreateHouseholdInput) (*model.Household, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 120 || len(input.MemberNames) > 12 {
		return nil, fmt.Errorf("%w: invalid household name or member count", ErrBadInput)
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	householdID := newID()
	if _, err := tx.Exec(ctx, `INSERT INTO households(id, name) VALUES ($1, $2)`, householdID, name); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO household_members(household_id, member_id, role) VALUES ($1, $2, 'OWNER')`, householdID, user.MemberID); err != nil {
		return nil, err
	}
	for _, memberName := range input.MemberNames {
		memberName = strings.TrimSpace(memberName)
		if memberName == "" || len(memberName) > 120 {
			return nil, fmt.Errorf("%w: member names must not be empty", ErrBadInput)
		}
		placeholderID := newID()
		placeholderSubject := "invite:" + placeholderID
		if _, err := tx.Exec(ctx, `INSERT INTO members(id, oidc_subject, display_name) VALUES ($1, $2, $3)`, placeholderID, placeholderSubject, memberName); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO household_members(household_id, member_id) VALUES ($1, $2)`, householdID, placeholderID); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO household_events(household_id, type, entity_id, revision, changed_fields, actor_id) VALUES ($1, 'household.created', $1, 1, '[]'::jsonb, $2)`, householdID, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadHousehold(ctx, r.DB, householdID)
}

func (r *mutationResolver) createPool(ctx context.Context, input model.CreatePoolInput) (*model.Pool, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 120 || !input.Kind.IsValid() {
		return nil, fmt.Errorf("%w: invalid pool", ErrBadInput)
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var version int64
	if err := tx.QueryRow(ctx, `SELECT default_allocation_version FROM households WHERE id = $1 FOR UPDATE`, input.HouseholdID).Scan(&version); err != nil {
		return nil, err
	}
	poolID := newID()
	if _, err := tx.Exec(ctx, `INSERT INTO pools(id, household_id, name, kind) VALUES ($1, $2, $3, $4)`, poolID, input.HouseholdID, name, input.Kind); err != nil {
		return nil, err
	}
	newVersion := version + 1
	if _, err := tx.Exec(ctx, `INSERT INTO allocation_defaults(household_id, version, pool_id, weight) SELECT household_id, $3, pool_id, weight FROM allocation_defaults WHERE household_id = $1 AND version = $2`, input.HouseholdID, version, newVersion); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO allocation_defaults(household_id, version, pool_id, weight) VALUES ($1, $2, $3, 1)`, input.HouseholdID, newVersion, poolID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE households SET default_allocation_version = $2, revision = revision + 1, updated_at = now() WHERE id = $1`, input.HouseholdID, newVersion); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "pool.created", poolID, newVersion, []string{"name", "kind"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &model.Pool{ID: poolID, Name: name, Kind: input.Kind, DefaultShareWeight: 1}, nil
}

func (r *mutationResolver) createAccount(ctx context.Context, input model.CreateAccountInput) (*model.Account, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 120 || !input.Kind.IsValid() {
		return nil, fmt.Errorf("%w: invalid account", ErrBadInput)
	}
	id := newID()
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, `INSERT INTO accounts(id, household_id, pool_id, name, kind) VALUES ($1, $2, $3, $4, $5)`, id, input.HouseholdID, input.PoolID, name, input.Kind); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "account.created", id, 1, []string{"name", "kind", "poolId"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &model.Account{ID: id, Name: name, Kind: input.Kind, PoolID: input.PoolID}, nil
}

func (r *mutationResolver) createCategory(ctx context.Context, input model.CreateCategoryInput) (*model.Category, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 120 {
		return nil, fmt.Errorf("%w: invalid category", ErrBadInput)
	}
	id := newID()
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, `INSERT INTO categories(id, household_id, parent_id, name) VALUES ($1, $2, $3, $4)`, id, input.HouseholdID, input.ParentID, name); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "category.created", id, 1, []string{"name", "parentId"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &model.Category{ID: id, Name: name, ParentID: input.ParentID}, nil
}

func (r *mutationResolver) setCategoryArchived(ctx context.Context, categoryID string, archived bool) (*model.Category, error) {
	var householdID string
	if err := r.DB.QueryRow(ctx, `SELECT household_id::text FROM categories WHERE id = $1`, categoryID).Scan(&householdID); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	user, err := requireMember(ctx, r.DB, householdID)
	if err != nil {
		return nil, err
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var name string
	var parentID *string
	var currentArchived bool
	if err := tx.QueryRow(ctx, `SELECT name, parent_id::text, archived FROM categories WHERE id = $1 AND household_id = $2 FOR UPDATE`, categoryID, householdID).Scan(&name, &parentID, &currentArchived); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if currentArchived != archived {
		if _, err := tx.Exec(ctx, `UPDATE categories SET archived = $2 WHERE id = $1`, categoryID, archived); err != nil {
			return nil, err
		}
		eventType := "category.unarchived"
		if archived {
			eventType = "category.archived"
		}
		if err := writeEvent(ctx, tx, householdID, eventType, categoryID, 1, []string{"archived"}, user.MemberID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &model.Category{ID: categoryID, Name: name, ParentID: parentID, Archived: archived}, nil
}

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
	var version int64
	if err := tx.QueryRow(ctx, `SELECT default_allocation_version FROM households WHERE id = $1 FOR UPDATE`, input.HouseholdID).Scan(&version); err != nil {
		return nil, err
	}
	for _, weight := range weights {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pools WHERE household_id = $1 AND id = $2)`, input.HouseholdID, weight.PoolID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrNotFound
		}
	}
	newVersion := version + 1
	for _, weight := range weights {
		if _, err := tx.Exec(ctx, `INSERT INTO allocation_defaults(household_id, version, pool_id, weight) VALUES ($1, $2, $3, $4)`, input.HouseholdID, newVersion, weight.PoolID, weight.Value); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE households SET default_allocation_version = $2, revision = revision + 1, updated_at = now() WHERE id = $1`, input.HouseholdID, newVersion); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "allocation.default.updated", input.HouseholdID, newVersion, []string{"defaultAllocation"}, user.MemberID); err != nil {
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
	scopeCount := 0
	for _, selected := range []*string{input.ProductVariantID, input.ProductConceptID, input.CategoryID, input.Merchant} {
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
	if input.Merchant != nil {
		merchantKey = normalizeMerchant(*input.Merchant)
		if merchantKey == "" || len(merchantKey) > 240 {
			return nil, fmt.Errorf("%w: invalid merchant rule key", ErrBadInput)
		}
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var lockedHouseholdID string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM households WHERE id = $1 FOR UPDATE`, input.HouseholdID).Scan(&lockedHouseholdID); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	for _, weight := range weights {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pools WHERE household_id = $1 AND id = $2)`, input.HouseholdID, weight.PoolID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrNotFound
		}
	}
	for _, reference := range []struct {
		table string
		id    *string
	}{{"categories", input.CategoryID}, {"product_concepts", input.ProductConceptID}, {"product_variants", input.ProductVariantID}} {
		if reference.id == nil {
			continue
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+reference.table+` WHERE household_id=$1 AND id=$2)`, input.HouseholdID, *reference.id).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrNotFound
		}
	}
	var ruleID string
	var revision int64
	if input.CategoryID != nil {
		err = tx.QueryRow(ctx, `SELECT id::text, revision FROM allocation_rules WHERE household_id = $1 AND category_id = $2 FOR UPDATE`, input.HouseholdID, *input.CategoryID).Scan(&ruleID, &revision)
	} else if input.Merchant != nil {
		err = tx.QueryRow(ctx, `SELECT id::text, revision FROM allocation_rules WHERE household_id = $1 AND merchant_key = $2 FOR UPDATE`, input.HouseholdID, merchantKey).Scan(&ruleID, &revision)
	} else if input.ProductConceptID != nil {
		err = tx.QueryRow(ctx, `SELECT id::text,revision FROM allocation_rules WHERE household_id=$1 AND product_concept_id=$2 FOR UPDATE`, input.HouseholdID, *input.ProductConceptID).Scan(&ruleID, &revision)
	} else {
		err = tx.QueryRow(ctx, `SELECT id::text,revision FROM allocation_rules WHERE household_id=$1 AND product_variant_id=$2 FOR UPDATE`, input.HouseholdID, *input.ProductVariantID).Scan(&ruleID, &revision)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		ruleID, revision = newID(), 1
		if _, err := tx.Exec(ctx, `INSERT INTO allocation_rules(id,household_id,category_id,merchant_key,product_concept_id,product_variant_id,revision) VALUES ($1,$2,$3,$4,$5,$6,$7)`, ruleID, input.HouseholdID, input.CategoryID, nullableMerchantKey(input.Merchant, merchantKey), input.ProductConceptID, input.ProductVariantID, revision); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else {
		revision++
		if _, err := tx.Exec(ctx, `UPDATE allocation_rules SET revision = $2, updated_at = now() WHERE id = $1`, ruleID, revision); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM allocation_rule_weights WHERE rule_id = $1`, ruleID); err != nil {
			return nil, err
		}
	}
	for _, weight := range weights {
		if _, err := tx.Exec(ctx, `INSERT INTO allocation_rule_weights(household_id, rule_id, pool_id, weight) VALUES ($1,$2,$3,$4)`, input.HouseholdID, ruleID, weight.PoolID, weight.Value); err != nil {
			return nil, err
		}
	}
	changedFields := []string{"allocations"}
	if input.ProductVariantID != nil {
		changedFields = append(changedFields, "productVariantId")
	} else if input.ProductConceptID != nil {
		changedFields = append(changedFields, "productConceptId")
	} else if input.CategoryID != nil {
		changedFields = append(changedFields, "categoryId")
	} else {
		changedFields = append(changedFields, "merchant")
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

func nullableMerchantKey(merchant *string, normalized string) any {
	if merchant == nil {
		return nil
	}
	return normalized
}

func allocationRuleWeights(ctx context.Context, tx pgx.Tx, householdID string, categoryID, productVariantID *string, merchant string) ([]domain.Weight, *string, int64, error) {
	var ruleID string
	var revision int64
	err := tx.QueryRow(ctx, `SELECT ar.id::text,ar.revision FROM allocation_rules ar LEFT JOIN product_variants pv ON pv.household_id=ar.household_id AND pv.id=$3::uuid WHERE ar.household_id=$1 AND (($3::uuid IS NOT NULL AND ar.product_variant_id=$3::uuid) OR (pv.concept_id IS NOT NULL AND ar.product_concept_id=pv.concept_id) OR ($2::uuid IS NOT NULL AND ar.category_id=$2::uuid) OR ($4<>'' AND ar.merchant_key=$4)) ORDER BY CASE WHEN $3::uuid IS NOT NULL AND ar.product_variant_id=$3::uuid THEN 0 WHEN pv.concept_id IS NOT NULL AND ar.product_concept_id=pv.concept_id THEN 1 WHEN $2::uuid IS NOT NULL AND ar.category_id=$2::uuid THEN 2 WHEN $4<>'' AND ar.merchant_key=$4 THEN 3 ELSE 4 END,ar.id LIMIT 1`, householdID, categoryID, productVariantID, merchant).Scan(&ruleID, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, 0, nil
	}
	if err != nil {
		return nil, nil, 0, err
	}
	rows, err := tx.Query(ctx, `SELECT pool_id::text, weight FROM allocation_rule_weights WHERE rule_id = $1 ORDER BY pool_id`, ruleID)
	if err != nil {
		return nil, nil, 0, err
	}
	weights := make([]domain.Weight, 0)
	for rows.Next() {
		var weight domain.Weight
		if err := rows.Scan(&weight.PoolID, &weight.Value); err != nil {
			rows.Close()
			return nil, nil, 0, err
		}
		weights = append(weights, weight)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, 0, err
	}
	if len(weights) == 0 {
		return nil, nil, 0, errors.New("allocation rule has no weights")
	}
	return weights, &ruleID, revision, nil
}

func (r *mutationResolver) createPurchase(ctx context.Context, input model.CreatePurchaseInput) (*model.Purchase, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	occurredAt, err := parseTime(input.OccurredAt)
	if err != nil {
		return nil, err
	}
	if input.IdempotencyKey == "" || len(input.IdempotencyKey) > 200 || len(input.Items) == 0 || len(input.Items) > 200 {
		return nil, fmt.Errorf("%w: invalid purchase", ErrBadInput)
	}
	if input.SourceReference != nil && (input.SourceDocumentID == nil || len(*input.SourceReference) > 256 || strings.ContainsAny(*input.SourceReference, "\r\n")) {
		return nil, fmt.Errorf("%w: source reference requires a document and must be valid", ErrBadInput)
	}
	status := model.PurchaseStatusDraft
	if input.Status != nil {
		if *input.Status != model.PurchaseStatusDraft {
			return nil, fmt.Errorf("%w: purchases must be posted with postPurchase", ErrBadInput)
		}
		status = *input.Status
	}
	merchant := strings.TrimSpace(input.Merchant)
	if len(merchant) > 240 {
		return nil, fmt.Errorf("%w: merchant name is too long", ErrBadInput)
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var existingID *string
	err = tx.QueryRow(ctx, `SELECT resource_id::text FROM idempotency_keys WHERE household_id = $1 AND key = $2 AND operation = 'createPurchase'`, input.HouseholdID, input.IdempotencyKey).Scan(&existingID)
	if err == nil && existingID != nil {
		if err := tx.Rollback(ctx); err != nil {
			return nil, err
		}
		return loadPurchase(ctx, r.DB, *existingID)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	period := occurredAt.UTC().Format("2006-01")
	if _, err := tx.Exec(ctx, `INSERT INTO accounting_periods(household_id, period, status, revision) VALUES ($1, $2, 'OPEN', 0) ON CONFLICT (household_id, period) DO NOTHING`, input.HouseholdID, period); err != nil {
		return nil, err
	}
	var periodStatus string
	if err := tx.QueryRow(ctx, `SELECT status FROM accounting_periods WHERE household_id = $1 AND period = $2 FOR UPDATE`, input.HouseholdID, period).Scan(&periodStatus); err != nil {
		return nil, err
	}
	if periodStatus == "CLOSED" {
		return nil, fmt.Errorf("%w: accounting period is closed", ErrConflict)
	}
	if input.SourceDocumentID != nil {
		sourceReference := ""
		if input.SourceReference != nil {
			sourceReference = *input.SourceReference
		}
		if len(sourceReference) > 256 {
			return nil, fmt.Errorf("%w: source reference is too long", ErrBadInput)
		}
		var documentHouseholdID string
		err := tx.QueryRow(ctx, `SELECT household_id::text FROM source_documents WHERE id = $1 FOR UPDATE`, *input.SourceDocumentID).Scan(&documentHouseholdID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if documentHouseholdID != input.HouseholdID {
			return nil, ErrNotFound
		}
		var confirmedPurchaseID string
		err = tx.QueryRow(ctx, `SELECT purchase_id::text FROM document_confirmations WHERE source_document_id = $1 AND source_reference = $2`, *input.SourceDocumentID, sourceReference).Scan(&confirmedPurchaseID)
		if err == nil {
			if err := tx.Rollback(ctx); err != nil {
				return nil, err
			}
			return loadPurchase(ctx, r.DB, confirmedPurchaseID)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	var ruleVersion int64
	if err := tx.QueryRow(ctx, `SELECT default_allocation_version FROM households WHERE id = $1 FOR SHARE`, input.HouseholdID).Scan(&ruleVersion); err != nil {
		return nil, err
	}
	defaults, err := loadWeights(ctx, tx, input.HouseholdID, ruleVersion)
	if err != nil {
		return nil, err
	}
	if len(defaults) == 0 {
		return nil, fmt.Errorf("%w: configure a default allocation before adding purchases", ErrBadInput)
	}
	merchantRuleKey := normalizeMerchant(merchant)
	purchaseID := newID()
	if _, err := tx.Exec(ctx, `INSERT INTO purchases(id, household_id, occurred_at, merchant, payment_account_id, status, allocation_rule_version, created_by) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, purchaseID, input.HouseholdID, occurredAt, merchant, input.PaymentAccountID, status, ruleVersion, user.MemberID); err != nil {
		return nil, err
	}
	for _, itemInput := range input.Items {
		if itemInput == nil || strings.TrimSpace(itemInput.Name) == "" || len(itemInput.Name) > 240 || itemInput.AmountYen < 0 || int64(itemInput.AmountYen) > maxMoney {
			return nil, fmt.Errorf("%w: invalid purchase item", ErrBadInput)
		}
		categoryID := itemInput.CategoryID
		if itemInput.ProductVariantID != nil {
			var productCategoryID *string
			err := tx.QueryRow(ctx, `SELECT c.category_id::text FROM product_variants v JOIN product_concepts c ON c.id=v.concept_id WHERE v.id=$1 AND v.household_id=$2 AND NOT v.archived AND NOT c.archived`, *itemInput.ProductVariantID, input.HouseholdID).Scan(&productCategoryID)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, ErrNotFound
			}
			if err != nil {
				return nil, err
			}
			if categoryID == nil {
				categoryID = productCategoryID
			}
		}
		weights := defaults
		var appliedRuleID *string
		appliedRuleVersion := ruleVersion
		allocationOverride := false
		if len(itemInput.Allocations) > 0 {
			weights, err = allocationWeights(itemInput.Allocations)
			if err != nil {
				return nil, err
			}
			allocationOverride = true
		} else {
			ruleWeights, ruleID, revision, ruleErr := allocationRuleWeights(ctx, tx, input.HouseholdID, categoryID, itemInput.ProductVariantID, merchantRuleKey)
			if ruleErr != nil {
				return nil, ruleErr
			}
			if ruleID != nil {
				weights, appliedRuleID, appliedRuleVersion = ruleWeights, ruleID, revision
			}
		}
		allocated, err := domain.Allocate(int64(itemInput.AmountYen), weights)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrBadInput, err)
		}
		itemID := newID()
		if _, err := tx.Exec(ctx, `INSERT INTO purchase_items(id,household_id,purchase_id,name,amount_yen,category_id,product_variant_id,allocation_override) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, itemID, input.HouseholdID, purchaseID, strings.TrimSpace(itemInput.Name), int64(itemInput.AmountYen), categoryID, itemInput.ProductVariantID, allocationOverride); err != nil {
			return nil, err
		}
		for _, allocation := range allocated {
			if _, err := tx.Exec(ctx, `INSERT INTO item_allocations(household_id, purchase_id, item_id, pool_id, weight, amount_yen, rule_version, rule_id) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, input.HouseholdID, purchaseID, itemID, allocation.PoolID, allocation.Weight, allocation.Amount, appliedRuleVersion, appliedRuleID); err != nil {
				return nil, err
			}
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO idempotency_keys(household_id, key, operation, resource_id) VALUES ($1, $2, 'createPurchase', $3)`, input.HouseholdID, input.IdempotencyKey, purchaseID); err != nil {
		return nil, err
	}
	if input.SourceDocumentID != nil {
		sourceReference := ""
		if input.SourceReference != nil {
			sourceReference = *input.SourceReference
		}
		if _, err := tx.Exec(ctx, `INSERT INTO document_confirmations(source_document_id, source_reference, household_id, purchase_id, created_by) VALUES ($1, $2, $3, $4, $5)`, *input.SourceDocumentID, sourceReference, input.HouseholdID, purchaseID, user.MemberID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO evidence_links(household_id, source_document_id, purchase_id) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, input.HouseholdID, *input.SourceDocumentID, purchaseID); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO household_events(household_id, type, entity_id, revision, changed_fields, actor_id) VALUES ($1, 'purchase.created', $2, 1, '[]'::jsonb, $3)`, input.HouseholdID, purchaseID, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadPurchase(ctx, r.DB, purchaseID)
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

func lockAccountingPeriods(ctx context.Context, tx pgx.Tx, householdID string, periods ...string) error {
	slices.Sort(periods)
	periods = slices.Compact(periods)
	for _, period := range periods {
		if _, err := tx.Exec(ctx, `INSERT INTO accounting_periods(household_id,period,status,revision) VALUES ($1,$2,'OPEN',0) ON CONFLICT (household_id,period) DO NOTHING`, householdID, period); err != nil {
			return err
		}
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM accounting_periods WHERE household_id=$1 AND period=$2 FOR UPDATE`, householdID, period).Scan(&status); err != nil {
			return err
		}
		if status == "CLOSED" {
			return fmt.Errorf("%w: accounting period is closed", ErrConflict)
		}
	}
	return nil
}

func syncPostedPurchaseAccounting(ctx context.Context, tx pgx.Tx, purchaseID string) error {
	var householdID string
	var occurredAt time.Time
	var merchant string
	var paymentAccountID *string
	if err := tx.QueryRow(ctx, `SELECT household_id::text,occurred_at,merchant,payment_account_id::text FROM purchases WHERE id=$1`, purchaseID).Scan(&householdID, &occurredAt, &merchant, &paymentAccountID); err != nil {
		return err
	}
	var journalID string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM journals WHERE household_id=$1 AND source_kind='PURCHASE' AND source_id=$2`, householdID, purchaseID).Scan(&journalID); err != nil {
		return err
	}
	var total int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(amount_yen),0) FROM purchase_items WHERE purchase_id=$1`, purchaseID).Scan(&total); err != nil {
		return err
	}
	if total <= 0 {
		return fmt.Errorf("%w: purchase total must be greater than zero", ErrBadInput)
	}
	if _, err := tx.Exec(ctx, `UPDATE journals SET occurred_at=$2 WHERE id=$1`, journalID, occurredAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM journal_entries WHERE journal_id=$1`, journalID); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT pool_id::text,sum(amount_yen) FROM item_allocations WHERE purchase_id=$1 GROUP BY pool_id ORDER BY pool_id`, purchaseID)
	if err != nil {
		return err
	}
	type purchaseShare struct {
		poolID string
		amount int64
	}
	shares := make([]purchaseShare, 0)
	for rows.Next() {
		var share purchaseShare
		if err := rows.Scan(&share.poolID, &share.amount); err != nil {
			rows.Close()
			return err
		}
		shares = append(shares, share)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	lineNo := 1
	for _, share := range shares {
		if share.amount == 0 {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO journal_entries(journal_id,line_no,pool_id,debit_yen,memo) VALUES ($1,$2,$3,$4,'Purchase allocation')`, journalID, lineNo, share.poolID, share.amount); err != nil {
			return err
		}
		lineNo++
	}
	if _, err := tx.Exec(ctx, `INSERT INTO journal_entries(journal_id,line_no,account_id,credit_yen,memo) VALUES ($1,$2,$3,$4,'Payment')`, journalID, lineNo, paymentAccountID, total); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE ledger_entries SET occurred_at=$2,description=$3,amount_yen=$4 WHERE household_id=$5 AND source_purchase_id=$1`, purchaseID, occurredAt, merchant, total, householdID)
	return err
}

func (r *mutationResolver) updatePurchase(ctx context.Context, input model.UpdatePurchaseInput) (*model.Purchase, error) {
	var householdID, status string
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var observedHouseholdID string
	var observedOccurredAt time.Time
	err = tx.QueryRow(ctx, `SELECT household_id::text,occurred_at FROM purchases WHERE id=$1`, input.PurchaseID).Scan(&observedHouseholdID, &observedOccurredAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if _, err := requireMember(ctx, r.DB, observedHouseholdID); err != nil {
		return nil, err
	}
	targetOccurredAt := observedOccurredAt
	if input.Patch != nil && input.Patch.OccurredAt != nil {
		targetOccurredAt, err = parseTime(*input.Patch.OccurredAt)
		if err != nil {
			return nil, err
		}
	}
	if err := lockAccountingPeriods(ctx, tx, observedHouseholdID, observedOccurredAt.UTC().Format("2006-01"), targetOccurredAt.UTC().Format("2006-01")); err != nil {
		return nil, err
	}
	var revision int64
	var fieldRevisions []byte
	var currentOccurredAt time.Time
	err = tx.QueryRow(ctx, `SELECT household_id::text, status, occurred_at, revision, field_revisions FROM purchases WHERE id = $1 FOR UPDATE`, input.PurchaseID).Scan(&householdID, &status, &currentOccurredAt, &revision, &fieldRevisions)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if householdID != observedHouseholdID || !currentOccurredAt.Equal(observedOccurredAt) {
		return nil, ErrConflict
	}
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	var lockedHouseholdID string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM households WHERE id = $1 FOR SHARE`, householdID).Scan(&lockedHouseholdID); err != nil {
		return nil, err
	}
	if status != string(model.PurchaseStatusDraft) && status != string(model.PurchaseStatusPosted) {
		return nil, fmt.Errorf("%w: closed purchases require a correction", ErrConflict)
	}
	if input.BaseRevision > int(revision) {
		return nil, ErrConflict
	}
	if err := validateChangedFields(input.ChangedFields, "occurredAt", "merchant", "paymentAccountId"); err != nil {
		return nil, err
	}
	for _, field := range input.ChangedFields {
		switch field {
		case "occurredAt":
			if input.Patch == nil || input.Patch.OccurredAt == nil {
				return nil, fmt.Errorf("%w: changed field has no value", ErrBadInput)
			}
		case "merchant":
			if input.Patch == nil || input.Patch.Merchant == nil {
				return nil, fmt.Errorf("%w: changed field has no value", ErrBadInput)
			}
		}
	}
	if input.Patch != nil && (input.Patch.OccurredAt != nil && !slices.Contains(input.ChangedFields, "occurredAt") || input.Patch.Merchant != nil && !slices.Contains(input.ChangedFields, "merchant") || input.Patch.PaymentAccountID != nil && !slices.Contains(input.ChangedFields, "paymentAccountId")) {
		return nil, fmt.Errorf("%w: patch contains fields missing from changedFields", ErrBadInput)
	}
	conflict, err := valuesConflict(fieldRevisions, input.BaseRevision, input.ChangedFields)
	if err != nil {
		return nil, err
	}
	if conflict {
		return nil, ErrConflict
	}
	newRevision := revision + 1
	updatedFields, err := setFieldRevisions(fieldRevisions, input.ChangedFields, newRevision)
	if err != nil {
		return nil, err
	}
	var occurredAt any
	var merchant any
	var accountID any
	updateAccountID := slices.Contains(input.ChangedFields, "paymentAccountId")
	if input.Patch != nil {
		if slices.Contains(input.ChangedFields, "occurredAt") {
			parsed, err := parseTime(*input.Patch.OccurredAt)
			if err != nil {
				return nil, err
			}
			occurredAt = parsed
		}
		if slices.Contains(input.ChangedFields, "merchant") {
			value := strings.TrimSpace(*input.Patch.Merchant)
			if len(value) > 240 {
				return nil, fmt.Errorf("%w: merchant name is too long", ErrBadInput)
			}
			merchant = value
		}
		if updateAccountID && input.Patch.PaymentAccountID != nil {
			accountID = *input.Patch.PaymentAccountID
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE purchases SET occurred_at = COALESCE($2, occurred_at), merchant = COALESCE($3, merchant), payment_account_id = CASE WHEN $7 THEN $4::uuid ELSE payment_account_id END, revision = $5, field_revisions = $6, updated_at = now() WHERE id = $1`, input.PurchaseID, occurredAt, merchant, accountID, newRevision, updatedFields, updateAccountID); err != nil {
		return nil, err
	}
	if status == string(model.PurchaseStatusPosted) {
		if err := syncPostedPurchaseAccounting(ctx, tx, input.PurchaseID); err != nil {
			return nil, err
		}
	}
	if err := writeEvent(ctx, tx, householdID, "purchase.updated", input.PurchaseID, newRevision, input.ChangedFields, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadPurchase(ctx, r.DB, input.PurchaseID)
}

func (r *mutationResolver) updatePurchaseItem(ctx context.Context, input model.UpdatePurchaseItemInput) (*model.PurchaseItem, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var householdID, purchaseID, status, name, merchant string
	var amount int64
	var revision, ruleVersion int64
	var categoryID, productVariantID *string
	var allocationOverride bool
	var fieldRevisions []byte
	var observedOccurredAt time.Time
	err = tx.QueryRow(ctx, `SELECT i.household_id::text,i.purchase_id::text,p.status,p.occurred_at,i.name,i.amount_yen,i.category_id::text,i.product_variant_id::text,i.revision,i.field_revisions,p.allocation_rule_version,p.merchant,i.allocation_override FROM purchase_items i JOIN purchases p ON p.id=i.purchase_id WHERE i.id=$1`, input.ItemID).Scan(&householdID, &purchaseID, &status, &observedOccurredAt, &name, &amount, &categoryID, &productVariantID, &revision, &fieldRevisions, &ruleVersion, &merchant, &allocationOverride)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	if err := lockAccountingPeriods(ctx, tx, householdID, observedOccurredAt.UTC().Format("2006-01")); err != nil {
		return nil, err
	}
	var lockedOccurredAt time.Time
	err = tx.QueryRow(ctx, `SELECT p.status,p.occurred_at,i.name,i.amount_yen,i.category_id::text,i.product_variant_id::text,i.revision,i.field_revisions,p.allocation_rule_version,p.merchant,i.allocation_override FROM purchase_items i JOIN purchases p ON p.id=i.purchase_id WHERE i.id=$1 AND i.purchase_id=$2 FOR UPDATE OF i,p`, input.ItemID, purchaseID).Scan(&status, &lockedOccurredAt, &name, &amount, &categoryID, &productVariantID, &revision, &fieldRevisions, &ruleVersion, &merchant, &allocationOverride)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !lockedOccurredAt.Equal(observedOccurredAt) {
		return nil, ErrConflict
	}
	if status != string(model.PurchaseStatusDraft) && status != string(model.PurchaseStatusPosted) {
		return nil, fmt.Errorf("%w: closed purchases require a correction", ErrConflict)
	}
	if input.BaseRevision > int(revision) {
		return nil, ErrConflict
	}
	if err := validateChangedFields(input.ChangedFields, "name", "amountYen", "categoryId", "productVariantId", "allocations"); err != nil {
		return nil, err
	}
	for _, field := range input.ChangedFields {
		switch field {
		case "name":
			if input.Name == nil {
				return nil, fmt.Errorf("%w: changed field has no value", ErrBadInput)
			}
		case "amountYen":
			if input.AmountYen == nil {
				return nil, fmt.Errorf("%w: changed field has no value", ErrBadInput)
			}
		case "allocations":
			if input.Allocations == nil {
				return nil, fmt.Errorf("%w: changed field has no value", ErrBadInput)
			}
		}
	}
	if input.Name != nil && !slices.Contains(input.ChangedFields, "name") || input.AmountYen != nil && !slices.Contains(input.ChangedFields, "amountYen") || input.CategoryID != nil && !slices.Contains(input.ChangedFields, "categoryId") || input.ProductVariantID != nil && !slices.Contains(input.ChangedFields, "productVariantId") || input.Allocations != nil && !slices.Contains(input.ChangedFields, "allocations") {
		return nil, fmt.Errorf("%w: patch contains fields missing from changedFields", ErrBadInput)
	}
	if input.ProductLearning != nil && !slices.Contains(input.ChangedFields, "productVariantId") {
		return nil, fmt.Errorf("%w: product learning requires a product variant change", ErrBadInput)
	}
	conflict, err := valuesConflict(fieldRevisions, input.BaseRevision, input.ChangedFields)
	if err != nil {
		return nil, err
	}
	if conflict {
		return nil, ErrConflict
	}
	newName, newAmount, newCategory := name, amount, categoryID
	if input.Name != nil {
		newName = strings.TrimSpace(*input.Name)
		if newName == "" || len(newName) > 240 {
			return nil, fmt.Errorf("%w: invalid item name", ErrBadInput)
		}
	}
	if input.AmountYen != nil {
		newAmount = int64(*input.AmountYen)
		if newAmount < 0 || newAmount > maxMoney {
			return nil, fmt.Errorf("%w: invalid item amount", ErrBadInput)
		}
	}
	updateCategoryID := slices.Contains(input.ChangedFields, "categoryId")
	if updateCategoryID {
		newCategory = input.CategoryID
	}
	updateProductVariantID := slices.Contains(input.ChangedFields, "productVariantId")
	newProductVariantID := productVariantID
	if updateProductVariantID {
		newProductVariantID = input.ProductVariantID
	}
	learningScope := model.ProductLearningScopeItemOnly
	if input.ProductLearning != nil {
		learningScope = *input.ProductLearning
	}
	if !learningScope.IsValid() || input.VariantName != nil && learningScope != model.ProductLearningScopeVariantName {
		return nil, fmt.Errorf("%w: invalid product learning scope", ErrBadInput)
	}
	if updateProductVariantID && newProductVariantID != nil {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM product_variants v JOIN product_concepts c ON c.id=v.concept_id WHERE v.id=$1 AND v.household_id=$2 AND NOT v.archived AND NOT c.archived)`, *newProductVariantID, householdID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrNotFound
		}
	}
	if learningScope != model.ProductLearningScopeItemOnly && newProductVariantID == nil {
		return nil, fmt.Errorf("%w: product learning requires a linked variant", ErrBadInput)
	}
	var weights []domain.Weight
	var appliedRuleID *string
	var appliedRuleVersion = ruleVersion
	newAllocationOverride := allocationOverride
	if input.Allocations != nil {
		weights, err = allocationWeights(input.Allocations)
		newAllocationOverride = true
	} else if (updateCategoryID || updateProductVariantID) && !allocationOverride {
		weights, appliedRuleID, appliedRuleVersion, err = allocationRuleWeights(ctx, tx, householdID, newCategory, newProductVariantID, normalizeMerchant(merchant))
		if err == nil && appliedRuleID == nil {
			weights, err = loadWeights(ctx, tx, householdID, ruleVersion)
		}
	} else {
		rows, queryErr := tx.Query(ctx, `SELECT pool_id::text, weight, rule_id::text, rule_version FROM item_allocations WHERE item_id = $1 ORDER BY pool_id`, input.ItemID)
		if queryErr != nil {
			return nil, queryErr
		}
		for rows.Next() {
			var weight domain.Weight
			var ruleID *string
			var version int64
			if err := rows.Scan(&weight.PoolID, &weight.Value, &ruleID, &version); err != nil {
				rows.Close()
				return nil, err
			}
			if len(weights) == 0 {
				appliedRuleID, appliedRuleVersion = ruleID, version
			}
			weights = append(weights, weight)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if len(weights) == 0 {
			weights, err = loadWeights(ctx, tx, householdID, ruleVersion)
		}
	}
	if err != nil {
		return nil, err
	}
	allocated, err := domain.Allocate(newAmount, weights)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	newRevision := revision + 1
	updatedFields, err := setFieldRevisions(fieldRevisions, input.ChangedFields, newRevision)
	if err != nil {
		return nil, err
	}
	if learningScope == model.ProductLearningScopeVariantName {
		variantName := strings.TrimSpace(trimOptional(input.VariantName))
		variantKey := normalizeProductText(variantName)
		if variantKey == "" || len(variantName) > 240 {
			return nil, fmt.Errorf("%w: invalid normalized product name", ErrBadInput)
		}
		if _, err := tx.Exec(ctx, `UPDATE product_variants SET name=$2,normalized_name=$3,revision=revision+1,updated_at=now() WHERE id=$1 AND household_id=$4`, *newProductVariantID, variantName, variantKey, householdID); err != nil {
			return nil, err
		}
		var productRevision int64
		if err := tx.QueryRow(ctx, `SELECT revision FROM product_variants WHERE id=$1`, *newProductVariantID).Scan(&productRevision); err != nil {
			return nil, err
		}
		if err := writeEvent(ctx, tx, householdID, "product.variant.renamed", *newProductVariantID, productRevision, []string{"name"}, user.MemberID); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE purchase_items SET name=$2,amount_yen=$3,category_id=CASE WHEN $8 THEN $4::uuid ELSE category_id END,product_variant_id=CASE WHEN $9 THEN $10::uuid ELSE product_variant_id END,allocation_override=$5,revision=$6,field_revisions=$7,updated_at=now() WHERE id=$1`, input.ItemID, newName, newAmount, newCategory, newAllocationOverride, newRevision, updatedFields, updateCategoryID, updateProductVariantID, newProductVariantID); err != nil {
		return nil, err
	}
	if updateProductVariantID && newProductVariantID != nil {
		merchantKey := normalizeMerchant(merchant)
		if learningScope == model.ProductLearningScopeMerchantAlias {
			if merchantKey == "" {
				return nil, fmt.Errorf("%w: merchant-specific learning requires a merchant", ErrBadInput)
			}
			var aliasID string
			if err := upsertProductAlias(ctx, tx, householdID, *newProductVariantID, merchantKey, name, normalizeProductText(name), &aliasID); err != nil {
				return nil, err
			}
			var aliasRevision int64
			if err := tx.QueryRow(ctx, `SELECT revision FROM product_aliases WHERE id=$1`, aliasID).Scan(&aliasRevision); err != nil {
				return nil, err
			}
			if err := writeEvent(ctx, tx, householdID, "product.alias.learned", aliasID, aliasRevision, []string{"alias", "variantId", "merchant"}, user.MemberID); err != nil {
				return nil, err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO product_corrections(id,household_id,purchase_id,item_id,raw_name,previous_product_variant_id,product_variant_id,learning_scope,merchant_key,actor_id) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, newID(), householdID, purchaseID, input.ItemID, name, productVariantID, *newProductVariantID, learningScope, merchantKey, user.MemberID); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM item_allocations WHERE item_id = $1`, input.ItemID); err != nil {
		return nil, err
	}
	for _, allocation := range allocated {
		if _, err := tx.Exec(ctx, `INSERT INTO item_allocations(household_id, purchase_id, item_id, pool_id, weight, amount_yen, rule_version, rule_id) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, householdID, purchaseID, input.ItemID, allocation.PoolID, allocation.Weight, allocation.Amount, appliedRuleVersion, appliedRuleID); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE purchases SET revision = revision + 1, updated_at = now() WHERE id = $1`, purchaseID); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, householdID, "purchase.item.updated", input.ItemID, newRevision, input.ChangedFields, user.MemberID); err != nil {
		return nil, err
	}
	if status == string(model.PurchaseStatusPosted) {
		if err := syncPostedPurchaseAccounting(ctx, tx, purchaseID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	purchase, err := loadPurchase(ctx, r.DB, purchaseID)
	if err != nil {
		return nil, err
	}
	for _, item := range purchase.Items {
		if item.ID == input.ItemID {
			return item, nil
		}
	}
	return nil, ErrNotFound
}

func (r *mutationResolver) postPurchase(ctx context.Context, purchaseID string, expectedRevision int, idempotencyKey string) (*model.Purchase, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if idempotencyKey == "" || len(idempotencyKey) > 200 {
		return nil, fmt.Errorf("%w: idempotency key is required", ErrBadInput)
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var householdID string
	var occurredAt time.Time
	if err := tx.QueryRow(ctx, `SELECT household_id::text, occurred_at FROM purchases WHERE id = $1`, purchaseID).Scan(&householdID, &occurredAt); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	var existingID *string
	err = tx.QueryRow(ctx, `SELECT resource_id::text FROM idempotency_keys WHERE household_id = $1 AND key = $2 AND operation = 'postPurchase'`, householdID, idempotencyKey).Scan(&existingID)
	if err == nil && existingID != nil {
		if err := tx.Rollback(ctx); err != nil {
			return nil, err
		}
		return loadPurchase(ctx, r.DB, *existingID)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	period := occurredAt.UTC().Format("2006-01")
	if _, err := tx.Exec(ctx, `INSERT INTO accounting_periods(household_id, period, status, revision) VALUES ($1, $2, 'OPEN', 0) ON CONFLICT (household_id, period) DO NOTHING`, householdID, period); err != nil {
		return nil, err
	}
	var periodStatus string
	if err := tx.QueryRow(ctx, `SELECT status FROM accounting_periods WHERE household_id = $1 AND period = $2 FOR UPDATE`, householdID, period).Scan(&periodStatus); err != nil {
		return nil, err
	}
	if periodStatus == "CLOSED" {
		return nil, fmt.Errorf("%w: accounting period is closed", ErrConflict)
	}
	var status string
	var revision int64
	var paymentAccountID *string
	err = tx.QueryRow(ctx, `SELECT status, occurred_at, revision, payment_account_id::text FROM purchases WHERE id = $1 FOR UPDATE`, purchaseID).Scan(&status, &occurredAt, &revision, &paymentAccountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if occurredAt.UTC().Format("2006-01") != period {
		return nil, ErrConflict
	}
	if revision != int64(expectedRevision) {
		return nil, ErrConflict
	}
	if status != string(model.PurchaseStatusDraft) {
		return nil, fmt.Errorf("%w: purchase is already posted", ErrConflict)
	}
	var total int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(amount_yen), 0) FROM purchase_items WHERE purchase_id = $1`, purchaseID).Scan(&total); err != nil {
		return nil, err
	}
	if total <= 0 {
		return nil, fmt.Errorf("%w: purchase total must be greater than zero", ErrBadInput)
	}
	journalID, ledgerID := newID(), newID()
	if _, err := tx.Exec(ctx, `INSERT INTO journals(id, household_id, source_kind, source_id, idempotency_key, occurred_at, created_by) VALUES ($1, $2, 'PURCHASE', $3, $4, $5, $6)`, journalID, householdID, purchaseID, idempotencyKey, occurredAt, user.MemberID); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT pool_id::text, sum(amount_yen) FROM item_allocations WHERE purchase_id = $1 GROUP BY pool_id ORDER BY pool_id`, purchaseID)
	if err != nil {
		return nil, err
	}
	type purchaseShare struct {
		poolID string
		amount int64
	}
	shares := make([]purchaseShare, 0)
	for rows.Next() {
		var share purchaseShare
		if err := rows.Scan(&share.poolID, &share.amount); err != nil {
			rows.Close()
			return nil, err
		}
		shares = append(shares, share)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	lineNo := 1
	for _, share := range shares {
		if share.amount == 0 {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO journal_entries(journal_id, line_no, pool_id, debit_yen, memo) VALUES ($1, $2, $3, $4, 'Purchase allocation')`, journalID, lineNo, share.poolID, share.amount); err != nil {
			return nil, err
		}
		lineNo++
	}
	if _, err := tx.Exec(ctx, `INSERT INTO journal_entries(journal_id, line_no, account_id, credit_yen, memo) VALUES ($1, $2, $3, $4, 'Payment')`, journalID, lineNo, paymentAccountID, total); err != nil {
		return nil, err
	}
	var merchant string
	if err := tx.QueryRow(ctx, `SELECT merchant FROM purchases WHERE id = $1`, purchaseID).Scan(&merchant); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ledger_entries(id, household_id, journal_id, kind, occurred_at, description, amount_yen, pool_id, source_purchase_id) VALUES ($1, $2, $3, 'PURCHASE', $4, $5, $6, NULL, $7)`, ledgerID, householdID, journalID, occurredAt, merchant, total, purchaseID); err != nil {
		return nil, err
	}
	newRevision := revision + 1
	if _, err := tx.Exec(ctx, `UPDATE purchases SET status = 'POSTED', revision = $2, updated_at = now() WHERE id = $1`, purchaseID, newRevision); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO idempotency_keys(household_id, key, operation, resource_id) VALUES ($1, $2, 'postPurchase', $3)`, householdID, idempotencyKey, purchaseID); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, householdID, "purchase.posted", purchaseID, newRevision, []string{"status"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadPurchase(ctx, r.DB, purchaseID)
}

func (r *mutationResolver) recordFunding(ctx context.Context, input model.RecordFundingInput) (*model.LedgerEntry, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	if input.AmountYen <= 0 || int64(input.AmountYen) > maxMoney || input.IdempotencyKey == "" {
		return nil, fmt.Errorf("%w: invalid funding amount or idempotency key", ErrBadInput)
	}
	occurredAt, err := parseTime(input.OccurredAt)
	if err != nil {
		return nil, err
	}
	if err := verifyHouseholdMember(ctx, r.DB, input.HouseholdID, input.MemberID); err != nil {
		return nil, err
	}
	var poolExists bool
	if err := r.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pools WHERE household_id = $1 AND id = $2)`, input.HouseholdID, input.PoolID).Scan(&poolExists); err != nil {
		return nil, err
	}
	if !poolExists {
		return nil, ErrNotFound
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var existingID *string
	err = tx.QueryRow(ctx, `SELECT resource_id::text FROM idempotency_keys WHERE household_id = $1 AND key = $2 AND operation = 'recordFunding'`, input.HouseholdID, input.IdempotencyKey).Scan(&existingID)
	if err == nil && existingID != nil {
		if err := tx.Rollback(ctx); err != nil {
			return nil, err
		}
		return loadLedgerEntry(ctx, r.DB, *existingID)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	period := occurredAt.UTC().Format("2006-01")
	if _, err := tx.Exec(ctx, `INSERT INTO accounting_periods(household_id, period, status, revision) VALUES ($1, $2, 'OPEN', 0) ON CONFLICT (household_id, period) DO NOTHING`, input.HouseholdID, period); err != nil {
		return nil, err
	}
	var periodStatus string
	if err := tx.QueryRow(ctx, `SELECT status FROM accounting_periods WHERE household_id = $1 AND period = $2 FOR UPDATE`, input.HouseholdID, period).Scan(&periodStatus); err != nil {
		return nil, err
	}
	if periodStatus == "CLOSED" {
		return nil, fmt.Errorf("%w: accounting period is closed", ErrConflict)
	}
	var scheduleID *string
	err = tx.QueryRow(ctx, `SELECT id::text FROM funding_schedules WHERE household_id = $1 AND member_id = $2 AND pool_id = $3 AND active ORDER BY created_at DESC LIMIT 1`, input.HouseholdID, input.MemberID, input.PoolID).Scan(&scheduleID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	obligationID := newID()
	if _, err := tx.Exec(ctx, `INSERT INTO funding_obligations(id, household_id, schedule_id, member_id, pool_id, period, amount_yen, paid_yen) VALUES ($1, $2, $3, $4, $5, $6, 0, $7) ON CONFLICT (household_id, member_id, pool_id, period) DO UPDATE SET paid_yen = funding_obligations.paid_yen + EXCLUDED.paid_yen`, obligationID, input.HouseholdID, scheduleID, input.MemberID, input.PoolID, period, int64(input.AmountYen)); err != nil {
		return nil, err
	}
	journalID, ledgerID := newID(), newID()
	if _, err := tx.Exec(ctx, `INSERT INTO journals(id, household_id, source_kind, source_id, idempotency_key, occurred_at, created_by) VALUES ($1, $2, 'FUNDING', $3, $4, $5, $6)`, journalID, input.HouseholdID, ledgerID, input.IdempotencyKey, occurredAt, user.MemberID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO journal_entries(journal_id, line_no, pool_id, debit_yen, memo) VALUES ($1, 1, $2, $3, 'Pool funding'), ($1, 2, $2, $3, 'Member contribution')`, journalID, input.PoolID, int64(input.AmountYen)); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE journal_entries SET debit_yen = 0, credit_yen = $2 WHERE journal_id = $1 AND line_no = 2`, journalID, int64(input.AmountYen)); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ledger_entries(id, household_id, journal_id, kind, occurred_at, description, amount_yen, from_member_id, pool_id) VALUES ($1, $2, $3, 'FUNDING', $4, 'Pool funding', $5, $6, $7)`, ledgerID, input.HouseholdID, journalID, occurredAt, int64(input.AmountYen), input.MemberID, input.PoolID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO idempotency_keys(household_id, key, operation, resource_id) VALUES ($1, $2, 'recordFunding', $3)`, input.HouseholdID, input.IdempotencyKey, ledgerID); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "funding.recorded", ledgerID, 1, []string{"amountYen"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadLedgerEntry(ctx, r.DB, ledgerID)
}

func verifyHouseholdMember(ctx context.Context, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, householdID, memberID string) error {
	var exists bool
	if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM household_members WHERE household_id = $1 AND member_id = $2)`, householdID, memberID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func loadLedgerEntry(ctx context.Context, db *pgxpool.Pool, entryID string) (*model.LedgerEntry, error) {
	var result model.LedgerEntry
	var amount int64
	var occurredAt, createdAt time.Time
	err := db.QueryRow(ctx, `SELECT id::text, kind, occurred_at, description, amount_yen, from_member_id::text, to_member_id::text, pool_id::text, source_purchase_id::text, created_at FROM ledger_entries WHERE id = $1`, entryID).Scan(&result.ID, &result.Kind, &occurredAt, &result.Description, &amount, &result.FromMemberID, &result.ToMemberID, &result.PoolID, &result.SourcePurchaseID, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	result.AmountYen, result.Currency = graphLong(amount), "JPY"
	result.OccurredAt, result.CreatedAt = timeString(occurredAt), timeString(createdAt)
	return &result, nil
}

func (r *mutationResolver) recordTransfer(ctx context.Context, input model.RecordTransferInput) (*model.LedgerEntry, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	if input.AmountYen <= 0 || int64(input.AmountYen) > maxMoney || input.IdempotencyKey == "" || !input.Kind.IsValid() {
		return nil, fmt.Errorf("%w: invalid transfer", ErrBadInput)
	}
	if input.FromMemberID == input.ToMemberID {
		return nil, fmt.Errorf("%w: transfer participants must differ", ErrBadInput)
	}
	if err := verifyHouseholdMember(ctx, r.DB, input.HouseholdID, input.FromMemberID); err != nil {
		return nil, err
	}
	if err := verifyHouseholdMember(ctx, r.DB, input.HouseholdID, input.ToMemberID); err != nil {
		return nil, err
	}
	occurredAt, err := parseTime(input.OccurredAt)
	if err != nil {
		return nil, err
	}
	if input.Kind == model.TransferKindRepayment && input.LoanID == nil {
		return nil, fmt.Errorf("%w: repayment requires a loan", ErrBadInput)
	}
	if input.Kind != model.TransferKindRepayment && input.LoanID != nil {
		return nil, fmt.Errorf("%w: loanId is only valid for a repayment", ErrBadInput)
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var existingID *string
	err = tx.QueryRow(ctx, `SELECT resource_id::text FROM idempotency_keys WHERE household_id = $1 AND key = $2 AND operation = 'recordTransfer'`, input.HouseholdID, input.IdempotencyKey).Scan(&existingID)
	if err == nil && existingID != nil {
		if err := tx.Rollback(ctx); err != nil {
			return nil, err
		}
		return loadLedgerEntry(ctx, r.DB, *existingID)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	period := occurredAt.UTC().Format("2006-01")
	if _, err := tx.Exec(ctx, `INSERT INTO accounting_periods(household_id, period, status, revision) VALUES ($1, $2, 'OPEN', 0) ON CONFLICT (household_id, period) DO NOTHING`, input.HouseholdID, period); err != nil {
		return nil, err
	}
	var periodStatus string
	if err := tx.QueryRow(ctx, `SELECT status FROM accounting_periods WHERE household_id = $1 AND period = $2 FOR UPDATE`, input.HouseholdID, period).Scan(&periodStatus); err != nil {
		return nil, err
	}
	if periodStatus == "CLOSED" {
		return nil, fmt.Errorf("%w: accounting period is closed", ErrConflict)
	}
	var loanID any
	if input.Kind == model.TransferKindRepayment {
		var lenderID, borrowerID string
		var originalAmount int64
		if err := tx.QueryRow(ctx, `SELECT from_member_id::text, to_member_id::text, amount_yen FROM transfers WHERE id = $1 AND household_id = $2 AND kind = 'LOAN' FOR UPDATE`, *input.LoanID, input.HouseholdID).Scan(&lenderID, &borrowerID, &originalAmount); err != nil {
			return nil, ErrNotFound
		}
		if input.FromMemberID != borrowerID || input.ToMemberID != lenderID {
			return nil, fmt.Errorf("%w: repayment direction does not match the loan", ErrBadInput)
		}
		var repaid int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(amount_yen), 0) FROM transfers WHERE loan_id = $1 AND kind = 'REPAYMENT'`, *input.LoanID).Scan(&repaid); err != nil {
			return nil, err
		}
		if int64(input.AmountYen) > originalAmount-repaid {
			return nil, fmt.Errorf("%w: repayment exceeds the outstanding loan", ErrBadInput)
		}
		loanID = *input.LoanID
	}
	transferID, journalID, ledgerID := newID(), newID(), newID()
	if _, err := tx.Exec(ctx, `INSERT INTO journals(id, household_id, source_kind, source_id, idempotency_key, occurred_at, created_by) VALUES ($1, $2, 'TRANSFER', $3, $4, $5, $6)`, journalID, input.HouseholdID, transferID, input.IdempotencyKey, occurredAt, user.MemberID); err != nil {
		return nil, err
	}
	amount := int64(input.AmountYen)
	if _, err := tx.Exec(ctx, `INSERT INTO journal_entries(journal_id, line_no, member_id, debit_yen, memo) VALUES ($1, 1, $2, $3, $4), ($1, 2, $5, $3, $6)`, journalID, input.ToMemberID, amount, string(input.Kind), input.FromMemberID, string(input.Kind)); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE journal_entries SET debit_yen = 0, credit_yen = $2 WHERE journal_id = $1 AND line_no = 2`, journalID, amount); err != nil {
		return nil, err
	}
	description := strings.TrimSpace(input.Description)
	if description == "" {
		description = string(input.Kind)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ledger_entries(id, household_id, journal_id, kind, occurred_at, description, amount_yen, from_member_id, to_member_id) VALUES ($1, $2, $3, 'TRANSFER', $4, $5, $6, $7, $8)`, ledgerID, input.HouseholdID, journalID, occurredAt, description, amount, input.FromMemberID, input.ToMemberID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO transfers(id, household_id, ledger_entry_id, from_member_id, to_member_id, kind, amount_yen, description, loan_id) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, transferID, input.HouseholdID, ledgerID, input.FromMemberID, input.ToMemberID, input.Kind, amount, description, loanID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO idempotency_keys(household_id, key, operation, resource_id) VALUES ($1, $2, 'recordTransfer', $3)`, input.HouseholdID, input.IdempotencyKey, ledgerID); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "transfer.recorded", ledgerID, 1, []string{"amountYen", "kind"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadLedgerEntry(ctx, r.DB, ledgerID)
}

func (r *mutationResolver) setFundingSchedule(ctx context.Context, input model.SetFundingScheduleInput) (bool, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return false, err
	}
	if input.AmountYen < 0 || int64(input.AmountYen) > maxMoney || input.Cadence != "MONTHLY" {
		return false, fmt.Errorf("%w: funding schedules currently require MONTHLY cadence", ErrBadInput)
	}
	start, err := parseDate(input.StartsOn)
	if err != nil {
		return false, err
	}
	var end any
	if input.EndsOn != nil {
		parsed, err := parseDate(*input.EndsOn)
		if err != nil {
			return false, err
		}
		if parsed.Before(start) {
			return false, fmt.Errorf("%w: schedule end precedes start", ErrBadInput)
		}
		end = parsed
	}
	if err := verifyHouseholdMember(ctx, r.DB, input.HouseholdID, input.MemberID); err != nil {
		return false, err
	}
	var poolExists bool
	if err := r.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pools WHERE household_id = $1 AND id = $2)`, input.HouseholdID, input.PoolID).Scan(&poolExists); err != nil {
		return false, err
	}
	if !poolExists {
		return false, ErrNotFound
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, `UPDATE funding_schedules SET active = false WHERE household_id = $1 AND member_id = $2 AND pool_id = $3 AND active`, input.HouseholdID, input.MemberID, input.PoolID); err != nil {
		return false, err
	}
	scheduleID := newID()
	if _, err := tx.Exec(ctx, `INSERT INTO funding_schedules(id, household_id, member_id, pool_id, amount_yen, cadence, starts_on, ends_on) VALUES ($1, $2, $3, $4, $5, 'MONTHLY', $6, $7)`, scheduleID, input.HouseholdID, input.MemberID, input.PoolID, int64(input.AmountYen), start, end); err != nil {
		return false, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "funding.schedule.updated", scheduleID, 1, []string{"amountYen", "startsOn", "endsOn"}, user.MemberID); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (r *mutationResolver) closePeriod(ctx context.Context, input model.ClosePeriodInput) (*model.Settlement, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	periodStart, err := time.Parse("2006-01", input.Period)
	if err != nil {
		return nil, fmt.Errorf("%w: period must use YYYY-MM", ErrBadInput)
	}
	periodEnd := periodStart.AddDate(0, 1, 0)
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, `INSERT INTO accounting_periods(household_id, period, status, revision) VALUES ($1, $2, 'OPEN', 0) ON CONFLICT (household_id, period) DO NOTHING`, input.HouseholdID, input.Period); err != nil {
		return nil, err
	}
	var status string
	var revision int64
	if err := tx.QueryRow(ctx, `SELECT status, revision FROM accounting_periods WHERE household_id = $1 AND period = $2 FOR UPDATE`, input.HouseholdID, input.Period).Scan(&status, &revision); err != nil {
		return nil, err
	}
	if status == "CLOSED" {
		return nil, ErrConflict
	}
	if revision != int64(input.ExpectedRevision) {
		return nil, ErrConflict
	}
	var drafts int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM purchases WHERE household_id=$1 AND status='DRAFT' AND occurred_at >= $2 AND occurred_at < $3`, input.HouseholdID, periodStart, periodEnd).Scan(&drafts); err != nil {
		return nil, err
	}
	if drafts != 0 {
		return nil, fmt.Errorf("%w: post or remove draft purchases before closing the period", ErrConflict)
	}
	schedules, err := tx.Query(ctx, `SELECT id::text, member_id::text, pool_id::text, amount_yen FROM funding_schedules WHERE household_id = $1 AND active AND cadence = 'MONTHLY' AND starts_on < $3 AND (ends_on IS NULL OR ends_on >= $2)`, input.HouseholdID, periodStart, periodEnd)
	if err != nil {
		return nil, err
	}
	type schedule struct {
		id, memberID, poolID string
		amount               int64
	}
	items := make([]schedule, 0)
	for schedules.Next() {
		var item schedule
		if err := schedules.Scan(&item.id, &item.memberID, &item.poolID, &item.amount); err != nil {
			schedules.Close()
			return nil, err
		}
		items = append(items, item)
	}
	schedules.Close()
	if err := schedules.Err(); err != nil {
		return nil, err
	}
	for _, item := range items {
		if _, err := tx.Exec(ctx, `INSERT INTO funding_obligations(id, household_id, schedule_id, member_id, pool_id, period, amount_yen) VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (household_id, member_id, pool_id, period) DO UPDATE SET schedule_id = EXCLUDED.schedule_id, amount_yen = EXCLUDED.amount_yen`, newID(), input.HouseholdID, item.id, item.memberID, item.poolID, input.Period, item.amount); err != nil {
			return nil, err
		}
	}
	lines, err := calculateSettlement(ctx, tx, input.HouseholdID, input.Period)
	if err != nil {
		return nil, err
	}
	settlementID := newID()
	newRevision := revision + 1
	if _, err := tx.Exec(ctx, `INSERT INTO settlement_runs(id, household_id, period, revision, status, created_by) VALUES ($1, $2, $3, $4, 'CLOSED', $5)`, settlementID, input.HouseholdID, input.Period, newRevision, user.MemberID); err != nil {
		return nil, err
	}
	for _, line := range lines {
		if _, err := tx.Exec(ctx, `INSERT INTO settlement_lines(settlement_run_id, member_id, scheduled_yen, paid_yen, carry_yen, advance_credit_yen, amount_due_yen) VALUES ($1, $2, $3, $4, $5, $6, $7)`, settlementID, line.MemberID, int64(line.ScheduledYen), int64(line.PaidYen), int64(line.CarryYen), int64(line.AdvanceCreditYen), int64(line.AmountDueYen)); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE purchases SET status = 'CLOSED', revision = revision + 1, updated_at = now() WHERE household_id = $1 AND status = 'POSTED' AND occurred_at >= $2 AND occurred_at < $3`, input.HouseholdID, periodStart, periodEnd); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE accounting_periods SET status = 'CLOSED', revision = $3, closed_by = $4, closed_at = now() WHERE household_id = $1 AND period = $2`, input.HouseholdID, input.Period, newRevision, user.MemberID); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "period.closed", settlementID, newRevision, []string{"status"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return (&queryResolver{r.Resolver}).settlement(ctx, input.HouseholdID, input.Period)
}

func calculateSettlement(ctx context.Context, db interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, householdID, period string) ([]domain.SettlementAmount, error) {
	rows, err := db.Query(ctx, `SELECT m.id::text, m.display_name, COALESCE(cur.scheduled_yen, 0), COALESCE(cur.paid_yen, 0), COALESCE(prev.owed_yen, 0) FROM household_members hm JOIN members m ON m.id = hm.member_id LEFT JOIN (SELECT member_id, sum(amount_yen) scheduled_yen, sum(paid_yen) paid_yen FROM funding_obligations WHERE household_id = $1 AND period = $2 GROUP BY member_id) cur ON cur.member_id = m.id LEFT JOIN (SELECT member_id, sum(amount_yen - paid_yen) owed_yen FROM funding_obligations WHERE household_id = $1 AND period < $2 GROUP BY member_id) prev ON prev.member_id = m.id WHERE hm.household_id = $1 ORDER BY m.display_name`, householdID, period)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	inputs := make([]domain.SettlementInput, 0)
	for rows.Next() {
		var input domain.SettlementInput
		if err := rows.Scan(&input.MemberID, &input.MemberName, &input.ScheduledYen, &input.PaidYen, &input.PreviousOwedYen); err != nil {
			return nil, err
		}
		inputs = append(inputs, input)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return domain.CalculateSettlement(inputs)
}

func (r *mutationResolver) correctPurchase(ctx context.Context, input model.CorrectPurchaseInput) (*model.Purchase, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	delta := int64(input.AmountDeltaYen)
	if delta == 0 || delta > maxMoney || delta < -maxMoney || strings.TrimSpace(input.Reason) == "" || input.IdempotencyKey == "" {
		return nil, fmt.Errorf("%w: invalid correction", ErrBadInput)
	}
	weights, err := allocationWeights(input.Allocations)
	if err != nil {
		return nil, err
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var period string
	err = tx.QueryRow(ctx, `SELECT to_char(occurred_at AT TIME ZONE 'UTC', 'YYYY-MM') FROM purchases WHERE id = $1 AND household_id = $2`, input.PurchaseID, input.HouseholdID).Scan(&period)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO accounting_periods(household_id, period, status, revision) VALUES ($1, $2, 'OPEN', 0) ON CONFLICT (household_id, period) DO NOTHING`, input.HouseholdID, period); err != nil {
		return nil, err
	}
	var periodStatus string
	if err := tx.QueryRow(ctx, `SELECT status FROM accounting_periods WHERE household_id = $1 AND period = $2 FOR UPDATE`, input.HouseholdID, period).Scan(&periodStatus); err != nil {
		return nil, err
	}
	if periodStatus != "CLOSED" {
		return nil, fmt.Errorf("%w: close the period before correcting a purchase", ErrBadInput)
	}
	var status string
	var lockedPeriod string
	err = tx.QueryRow(ctx, `SELECT status, to_char(occurred_at AT TIME ZONE 'UTC', 'YYYY-MM') FROM purchases WHERE id = $1 AND household_id = $2 FOR UPDATE`, input.PurchaseID, input.HouseholdID).Scan(&status, &lockedPeriod)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if lockedPeriod != period {
		return nil, ErrConflict
	}
	if status == string(model.PurchaseStatusDraft) {
		return nil, fmt.Errorf("%w: corrections require a posted purchase", ErrBadInput)
	}
	var itemID any
	if input.ItemID != nil {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM purchase_items WHERE id = $1 AND purchase_id = $2)`, *input.ItemID, input.PurchaseID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrNotFound
		}
		itemID = *input.ItemID
	}
	var existingID *string
	err = tx.QueryRow(ctx, `SELECT resource_id::text FROM idempotency_keys WHERE household_id = $1 AND key = $2 AND operation = 'correctPurchase'`, input.HouseholdID, input.IdempotencyKey).Scan(&existingID)
	if err == nil && existingID != nil {
		if err := tx.Rollback(ctx); err != nil {
			return nil, err
		}
		return loadPurchase(ctx, r.DB, input.PurchaseID)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	shares, err := domain.Allocate(abs(delta), weights)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	correctionID, journalID, ledgerID := newID(), newID(), newID()
	if _, err := tx.Exec(ctx, `INSERT INTO journals(id, household_id, source_kind, source_id, idempotency_key, occurred_at, created_by) VALUES ($1, $2, 'CORRECTION', $3, $4, now(), $5)`, journalID, input.HouseholdID, correctionID, input.IdempotencyKey, user.MemberID); err != nil {
		return nil, err
	}
	lineNo := 1
	for _, share := range shares {
		if share.Amount == 0 {
			continue
		}
		if delta > 0 {
			if _, err := tx.Exec(ctx, `INSERT INTO journal_entries(journal_id, line_no, pool_id, debit_yen, memo) VALUES ($1, $2, $3, $4, $5)`, journalID, lineNo, share.PoolID, share.Amount, input.Reason); err != nil {
				return nil, err
			}
		} else if _, err := tx.Exec(ctx, `INSERT INTO journal_entries(journal_id, line_no, pool_id, credit_yen, memo) VALUES ($1, $2, $3, $4, $5)`, journalID, lineNo, share.PoolID, share.Amount, input.Reason); err != nil {
			return nil, err
		}
		lineNo++
	}
	if delta > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO journal_entries(journal_id, line_no, credit_yen, memo) VALUES ($1, $2, $3, 'Correction offset')`, journalID, lineNo, delta); err != nil {
			return nil, err
		}
	} else if _, err := tx.Exec(ctx, `INSERT INTO journal_entries(journal_id, line_no, debit_yen, memo) VALUES ($1, $2, $3, 'Correction offset')`, journalID, lineNo, -delta); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ledger_entries(id, household_id, journal_id, kind, occurred_at, description, amount_yen, source_purchase_id) VALUES ($1, $2, $3, 'CORRECTION', now(), $4, $5, $6)`, ledgerID, input.HouseholdID, journalID, input.Reason, abs(delta), input.PurchaseID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO corrections(id, household_id, purchase_id, item_id, amount_delta_yen, reason, journal_id, created_by) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, correctionID, input.HouseholdID, input.PurchaseID, itemID, delta, input.Reason, journalID, user.MemberID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO idempotency_keys(household_id, key, operation, resource_id) VALUES ($1, $2, 'correctPurchase', $3)`, input.HouseholdID, input.IdempotencyKey, ledgerID); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "purchase.corrected", input.PurchaseID, 1, []string{"correction"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadPurchase(ctx, r.DB, input.PurchaseID)
}

func abs(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func writeEvent(ctx context.Context, tx pgx.Tx, householdID, eventType, entityID string, revision int64, fields []string, actorID string) error {
	encoded, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO household_events(household_id, type, entity_id, revision, changed_fields, actor_id) VALUES ($1, $2, $3, $4, $5, $6)`, householdID, eventType, entityID, revision, encoded, actorID)
	return err
}

func setFieldRevisions(current []byte, fields []string, revision int64) ([]byte, error) {
	var revisions map[string]int64
	if len(current) == 0 {
		revisions = make(map[string]int64)
	} else if err := json.Unmarshal(current, &revisions); err != nil {
		return nil, err
	}
	for _, field := range fields {
		revisions[field] = revision
	}
	return json.Marshal(revisions)
}

func validateChangedFields(fields []string, allowed ...string) error {
	if len(fields) == 0 {
		return fmt.Errorf("%w: changedFields is required", ErrBadInput)
	}
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if !slices.Contains(allowed, field) {
			return fmt.Errorf("%w: invalid changed field", ErrBadInput)
		}
		if _, exists := seen[field]; exists {
			return fmt.Errorf("%w: duplicate changed field", ErrBadInput)
		}
		seen[field] = struct{}{}
	}
	return nil
}

func valuesConflict(fieldRevisions []byte, base int, fields []string) (bool, error) {
	if base < 1 {
		return true, nil
	}
	var revisions map[string]int64
	if len(fieldRevisions) == 0 {
		return false, nil
	}
	if err := json.Unmarshal(fieldRevisions, &revisions); err != nil {
		return false, err
	}
	for _, field := range fields {
		if revisions[field] > int64(base) {
			return true, nil
		}
	}
	return false, nil
}

func parseDate(value string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: date must use YYYY-MM-DD", ErrBadInput)
	}
	return parsed, nil
}
