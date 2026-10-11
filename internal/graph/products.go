package graph

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/walnuts1018/horse/internal/graph/model"
)

type productDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func normalizeProductText(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}

func (r *mutationResolver) createProductConcept(ctx context.Context, input model.CreateProductConceptInput) (*model.ProductConcept, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(input.Name)
	normalized := normalizeProductText(name)
	if normalized == "" || len(name) > 240 {
		return nil, fmt.Errorf("%w: invalid product concept name", ErrBadInput)
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var householdID string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM households WHERE id=$1 FOR SHARE`, input.HouseholdID).Scan(&householdID); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	id := newID()
	if _, err := tx.Exec(ctx, `INSERT INTO product_concepts(id,household_id,name,normalized_name,category_id) VALUES ($1,$2,$3,$4,$5)`, id, input.HouseholdID, name, normalized, input.CategoryID); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "product.concept.created", id, 1, []string{"name", "categoryId"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadProductConcept(ctx, r.DB, id)
}

func (r *mutationResolver) createProductVariant(ctx context.Context, input model.CreateProductVariantInput) (*model.ProductVariant, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(input.Name)
	normalized := normalizeProductText(name)
	if normalized == "" || len(name) > 240 || input.Barcode != nil && strings.TrimSpace(*input.Barcode) == "" {
		return nil, fmt.Errorf("%w: invalid product variant", ErrBadInput)
	}
	brand, packageSize := trimOptional(input.Brand), trimOptional(input.PackageSize)
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var lockedHouseholdID string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM households WHERE id=$1 FOR SHARE`, input.HouseholdID).Scan(&lockedHouseholdID); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM product_concepts WHERE id=$1 AND household_id=$2 AND NOT archived)`, input.ConceptID, input.HouseholdID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	id := newID()
	if _, err := tx.Exec(ctx, `INSERT INTO product_variants(id,household_id,concept_id,name,normalized_name,brand,package_size,barcode) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, id, input.HouseholdID, input.ConceptID, name, normalized, brand, packageSize, nullableTrimmed(input.Barcode)); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "product.variant.created", id, 1, []string{"name", "conceptId"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadProductVariant(ctx, r.DB, id)
}

func (r *mutationResolver) setProductAlias(ctx context.Context, input model.SetProductAliasInput) (*model.ProductAlias, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	alias := strings.TrimSpace(input.Alias)
	merchantKey, normalizedAlias := normalizeMerchant(input.Merchant), normalizeProductText(alias)
	if alias == "" || len(alias) > 240 || merchantKey == "" || len(merchantKey) > 240 {
		return nil, fmt.Errorf("%w: invalid merchant product alias", ErrBadInput)
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var lockedHouseholdID string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM households WHERE id=$1 FOR UPDATE`, input.HouseholdID).Scan(&lockedHouseholdID); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	var variantExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM product_variants WHERE household_id=$1 AND id=$2 AND NOT archived)`, input.HouseholdID, input.VariantID).Scan(&variantExists); err != nil {
		return nil, err
	}
	if !variantExists {
		return nil, ErrNotFound
	}
	var aliasID string
	if err := upsertProductAlias(ctx, tx, input.HouseholdID, input.VariantID, merchantKey, alias, normalizedAlias, &aliasID); err != nil {
		return nil, err
	}
	var revision int64
	if err := tx.QueryRow(ctx, `SELECT revision FROM product_aliases WHERE id=$1`, aliasID).Scan(&revision); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "product.alias.learned", aliasID, revision, []string{"alias", "variantId", "merchant"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadProductAlias(ctx, r.DB, aliasID)
}

func upsertProductAlias(ctx context.Context, tx pgx.Tx, householdID, variantID, merchantKey, alias, normalizedAlias string, aliasID *string) error {
	return tx.QueryRow(ctx, `INSERT INTO product_aliases(id,household_id,variant_id,merchant_key,alias_text,normalized_alias) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (household_id,merchant_key,normalized_alias) DO UPDATE SET variant_id=EXCLUDED.variant_id,alias_text=EXCLUDED.alias_text,revision=product_aliases.revision+1,updated_at=now() RETURNING id::text`, newID(), householdID, variantID, merchantKey, alias, normalizedAlias).Scan(aliasID)
}

func trimOptional(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func nullableTrimmed(value *string) any {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

func loadProductConcept(ctx context.Context, db productDB, conceptID string) (*model.ProductConcept, error) {
	var concept model.ProductConcept
	var revision int64
	if err := db.QueryRow(ctx, `SELECT id::text,household_id::text,name,normalized_name,category_id::text,archived,revision FROM product_concepts WHERE id=$1`, conceptID).Scan(&concept.ID, &concept.HouseholdID, &concept.Name, &concept.NormalizedName, &concept.CategoryID, &concept.Archived, &revision); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	concept.Revision = int(revision)
	rows, err := db.Query(ctx, `SELECT id::text FROM product_variants WHERE household_id=$1 AND concept_id=$2 ORDER BY normalized_name,id`, concept.HouseholdID, conceptID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	concept.Variants = make([]*model.ProductVariant, 0, len(ids))
	for _, id := range ids {
		variant, err := loadProductVariant(ctx, db, id)
		if err != nil {
			return nil, err
		}
		concept.Variants = append(concept.Variants, variant)
	}
	return &concept, nil
}

func loadProductVariant(ctx context.Context, db productDB, variantID string) (*model.ProductVariant, error) {
	var variant model.ProductVariant
	var revision int64
	if err := db.QueryRow(ctx, `SELECT id::text,household_id::text,concept_id::text,name,normalized_name,brand,package_size,barcode,archived,revision FROM product_variants WHERE id=$1`, variantID).Scan(&variant.ID, &variant.HouseholdID, &variant.ConceptID, &variant.Name, &variant.NormalizedName, &variant.Brand, &variant.PackageSize, &variant.Barcode, &variant.Archived, &revision); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	variant.Revision = int(revision)
	rows, err := db.Query(ctx, `SELECT id::text FROM product_aliases WHERE household_id=$1 AND variant_id=$2 ORDER BY merchant_key,normalized_alias`, variant.HouseholdID, variant.ID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	variant.Aliases = make([]*model.ProductAlias, 0, len(ids))
	for _, id := range ids {
		alias, err := loadProductAlias(ctx, db, id)
		if err != nil {
			return nil, err
		}
		variant.Aliases = append(variant.Aliases, alias)
	}
	return &variant, nil
}

func loadProductAlias(ctx context.Context, db productDB, aliasID string) (*model.ProductAlias, error) {
	var alias model.ProductAlias
	var revision int64
	if err := db.QueryRow(ctx, `SELECT id::text,household_id::text,variant_id::text,merchant_key,alias_text,normalized_alias,revision FROM product_aliases WHERE id=$1`, aliasID).Scan(&alias.ID, &alias.HouseholdID, &alias.VariantID, &alias.MerchantKey, &alias.Alias, &alias.NormalizedAlias, &revision); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	alias.Revision = int(revision)
	return &alias, nil
}

func (r *queryResolver) productCatalog(ctx context.Context, householdID string) ([]*model.ProductConcept, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	rows, err := r.DB.Query(ctx, `SELECT id::text FROM product_concepts WHERE household_id=$1 ORDER BY normalized_name,id`, householdID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]*model.ProductConcept, 0, len(ids))
	for _, id := range ids {
		concept, err := loadProductConcept(ctx, r.DB, id)
		if err != nil {
			return nil, err
		}
		result = append(result, concept)
	}
	return result, nil
}

func (r *queryResolver) productSuggestions(ctx context.Context, householdID, query string, merchant *string, first *int) ([]*model.ProductSuggestion, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	searchKey := normalizeProductText(query)
	if searchKey == "" || len(searchKey) > 240 {
		return []*model.ProductSuggestion{}, nil
	}
	pageSize := 8
	if first != nil {
		pageSize = *first
	}
	if pageSize < 1 || pageSize > 20 {
		return nil, fmt.Errorf("%w: product suggestion limit must be between 1 and 20", ErrBadInput)
	}
	merchantKey := ""
	if merchant != nil {
		merchantKey = normalizeMerchant(*merchant)
	}
	pattern := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(searchKey) + "%"
	rows, err := r.DB.Query(ctx, `
		SELECT v.id::text,c.id::text,COALESCE(alias.alias_text,v.name),
			CASE
				WHEN alias.normalized_alias=$2 THEN 100
				WHEN v.normalized_name=$2 THEN 95
				WHEN c.normalized_name=$2 THEN 90
				WHEN alias.normalized_alias LIKE $2||'%' THEN 80
				WHEN v.normalized_name LIKE $2||'%' THEN 75
				WHEN c.normalized_name LIKE $2||'%' THEN 70
				ELSE 50
			END AS score
		FROM product_variants v
		JOIN product_concepts c ON c.household_id=v.household_id AND c.id=v.concept_id
		LEFT JOIN LATERAL (
			SELECT pa.alias_text,pa.normalized_alias FROM product_aliases pa
			WHERE pa.household_id=v.household_id AND pa.variant_id=v.id AND pa.merchant_key=$3
				AND $3<>'' AND pa.normalized_alias ILIKE $4 ESCAPE '\'
			ORDER BY (pa.normalized_alias=$2) DESC,pa.revision DESC LIMIT 1
		) alias ON true
		WHERE v.household_id=$1 AND NOT v.archived AND NOT c.archived
			AND (v.normalized_name ILIKE $4 ESCAPE '\' OR c.normalized_name ILIKE $4 ESCAPE '\' OR alias.normalized_alias IS NOT NULL)
		ORDER BY score DESC,c.normalized_name,v.normalized_name,v.id LIMIT $5`, householdID, searchKey, merchantKey, pattern, pageSize)
	if err != nil {
		return nil, err
	}
	type suggestionRow struct {
		variantID string
		conceptID string
		matched   string
		score     int
	}
	items := make([]suggestionRow, 0)
	for rows.Next() {
		var item suggestionRow
		if err := rows.Scan(&item.variantID, &item.conceptID, &item.matched, &item.score); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]*model.ProductSuggestion, 0, len(items))
	for _, item := range items {
		concept, err := loadProductConcept(ctx, r.DB, item.conceptID)
		if err != nil {
			return nil, err
		}
		variant, err := loadProductVariant(ctx, r.DB, item.variantID)
		if err != nil {
			return nil, err
		}
		result = append(result, &model.ProductSuggestion{Concept: concept, Variant: variant, MatchedName: item.matched, Score: item.score})
	}
	return result, nil
}
