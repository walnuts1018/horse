package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type productDB = bob.Executor

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
	householdUUID, _ := gofrsuuid.FromString(input.HouseholdID)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID))), sm.ForShare()).One(ctx, bobpgx.NewTx(tx, nil)); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	id := newID()
	idUUID, _ := gofrsuuid.FromString(id)
	var categoryID *gofrsuuid.UUID
	if input.CategoryID != nil {
		categoryValue, parseErr := gofrsuuid.FromString(*input.CategoryID)
		if parseErr != nil {
			return nil, parseErr
		}
		categoryID = &categoryValue
	}
	categoryValue := sql.Null[gofrsuuid.UUID]{}
	if categoryID != nil {
		categoryValue = sql.Null[gofrsuuid.UUID]{V: *categoryID, Valid: true}
	}
	conceptFields := &models.ProductConceptSetter{ID: &idUUID, HouseholdID: &householdUUID, Name: &name, NormalizedName: &normalized, CategoryID: &categoryValue}
	if _, err := models.ProductConcepts.Insert(conceptFields).Exec(ctx, bobpgx.NewTx(tx, nil)); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "product.concept.created", id, 1, []string{"name", "categoryId"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadProductConcept(ctx, bobpgx.NewPool(r.DB), id)
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
	householdUUID, _ := gofrsuuid.FromString(input.HouseholdID)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID))), sm.ForShare()).One(ctx, bobpgx.NewTx(tx, nil)); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	conceptUUID, _ := gofrsuuid.FromString(input.ConceptID)
	if _, err := models.ProductConcepts.Query(sm.Where(psql.And(models.ProductConcepts.Columns.ID.EQ(psql.Arg(conceptUUID)), models.ProductConcepts.Columns.HouseholdID.EQ(psql.Arg(householdUUID)), psql.Not(models.ProductConcepts.Columns.Archived)))).One(ctx, bobpgx.NewTx(tx, nil)); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	id := newID()
	idUUID, _ := gofrsuuid.FromString(id)
	barcode := nullableTrimmed(input.Barcode)
	barcodeValue := sql.Null[string]{}
	if barcodeString, ok := barcode.(string); ok {
		barcodeValue = sql.Null[string]{V: barcodeString, Valid: true}
	}
	conceptVariant := &models.ProductVariantSetter{ID: &idUUID, HouseholdID: &householdUUID, ConceptID: &conceptUUID, Name: &name, NormalizedName: &normalized, Brand: &brand, PackageSize: &packageSize, Barcode: &barcodeValue}
	if _, err := models.ProductVariants.Insert(conceptVariant).Exec(ctx, bobpgx.NewTx(tx, nil)); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "product.variant.created", id, 1, []string{"name", "conceptId"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadProductVariant(ctx, bobpgx.NewPool(r.DB), id)
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
	householdUUID, _ := gofrsuuid.FromString(input.HouseholdID)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID))), sm.ForUpdate()).One(ctx, bobpgx.NewTx(tx, nil)); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	variantUUID, _ := gofrsuuid.FromString(input.VariantID)
	if _, err := models.ProductVariants.Query(sm.Where(psql.And(models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdUUID)), models.ProductVariants.Columns.ID.EQ(psql.Arg(variantUUID)), psql.Not(models.ProductVariants.Columns.Archived)))).One(ctx, bobpgx.NewTx(tx, nil)); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	var aliasID string
	if err := upsertProductAlias(ctx, tx, input.HouseholdID, input.VariantID, merchantKey, alias, normalizedAlias, &aliasID); err != nil {
		return nil, err
	}
	aliasUUID, _ := gofrsuuid.FromString(aliasID)
	aliasRow, err := models.ProductAliases.Query(sm.Where(models.ProductAliases.Columns.ID.EQ(psql.Arg(aliasUUID)))).One(ctx, bobpgx.NewTx(tx, nil))
	if err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "product.alias.learned", aliasID, aliasRow.Revision, []string{"alias", "variantId", "merchant"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadProductAlias(ctx, bobpgx.NewPool(r.DB), aliasID)
}

func upsertProductAlias(ctx context.Context, tx pgx.Tx, householdID, variantID, merchantKey, alias, normalizedAlias string, aliasID *string) error {
	idValue, _ := gofrsuuid.FromString(newID())
	householdValue, _ := gofrsuuid.FromString(householdID)
	variantValue, _ := gofrsuuid.FromString(variantID)
	row, err := models.ProductAliases.Insert(
		&models.ProductAliasSetter{ID: &idValue, HouseholdID: &householdValue, VariantID: &variantValue, MerchantKey: &merchantKey, AliasText: &alias, NormalizedAlias: &normalizedAlias},
		im.OnConflict("household_id", "merchant_key", "normalized_alias").DoUpdate(
			im.SetCol("variant_id").To(im.Excluded("variant_id")),
			im.SetCol("alias_text").To(im.Excluded("alias_text")),
			im.SetCol("revision").To(models.ProductAliases.Columns.Revision.Plus(psql.Arg(1))),
			im.SetCol("updated_at").To(psql.F("now")),
		),
	).One(ctx, bobpgx.NewTx(tx, nil))
	if err != nil {
		return err
	}
	*aliasID = row.ID.String()
	return nil
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
	conceptUUID, _ := gofrsuuid.FromString(conceptID)
	row, err := models.ProductConcepts.Query(sm.Where(models.ProductConcepts.Columns.ID.EQ(psql.Arg(conceptUUID)))).One(ctx, db)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	concept := model.ProductConcept{ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), Name: row.Name, NormalizedName: row.NormalizedName, Archived: row.Archived, Revision: int(row.Revision)}
	if row.CategoryID.Valid {
		categoryID := row.CategoryID.V.String()
		concept.CategoryID = &categoryID
	}
	rows, err := models.ProductVariants.Query(sm.Where(psql.And(models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(row.HouseholdID)), models.ProductVariants.Columns.ConceptID.EQ(psql.Arg(conceptUUID)))), sm.OrderBy(models.ProductVariants.Columns.NormalizedName), sm.OrderBy(models.ProductVariants.Columns.ID)).All(ctx, db)
	if err != nil {
		return nil, err
	}
	concept.Variants = make([]*model.ProductVariant, 0, len(rows))
	for _, variantRow := range rows {
		variant, err := loadProductVariant(ctx, db, variantRow.ID.String())
		if err != nil {
			return nil, err
		}
		concept.Variants = append(concept.Variants, variant)
	}
	return &concept, nil
}

func loadProductVariant(ctx context.Context, db productDB, variantID string) (*model.ProductVariant, error) {
	variantUUID, _ := gofrsuuid.FromString(variantID)
	row, err := models.ProductVariants.Query(sm.Where(models.ProductVariants.Columns.ID.EQ(psql.Arg(variantUUID)))).One(ctx, db)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	variant := model.ProductVariant{ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), ConceptID: row.ConceptID.String(), Name: row.Name, NormalizedName: row.NormalizedName, Brand: row.Brand, PackageSize: row.PackageSize, Archived: row.Archived, Revision: int(row.Revision)}
	if row.Barcode.Valid {
		variant.Barcode = &row.Barcode.V
	}
	rows, err := models.ProductAliases.Query(sm.Where(psql.And(models.ProductAliases.Columns.HouseholdID.EQ(psql.Arg(row.HouseholdID)), models.ProductAliases.Columns.VariantID.EQ(psql.Arg(row.ID)))), sm.OrderBy(models.ProductAliases.Columns.MerchantKey), sm.OrderBy(models.ProductAliases.Columns.NormalizedAlias)).All(ctx, db)
	if err != nil {
		return nil, err
	}
	variant.Aliases = make([]*model.ProductAlias, 0, len(rows))
	for _, aliasRow := range rows {
		alias, err := loadProductAlias(ctx, db, aliasRow.ID.String())
		if err != nil {
			return nil, err
		}
		variant.Aliases = append(variant.Aliases, alias)
	}
	return &variant, nil
}

func loadProductAlias(ctx context.Context, db productDB, aliasID string) (*model.ProductAlias, error) {
	aliasUUID, _ := gofrsuuid.FromString(aliasID)
	row, err := models.ProductAliases.Query(sm.Where(models.ProductAliases.Columns.ID.EQ(psql.Arg(aliasUUID)))).One(ctx, db)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	return &model.ProductAlias{ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), VariantID: row.VariantID.String(), MerchantKey: row.MerchantKey, Alias: row.AliasText, NormalizedAlias: row.NormalizedAlias, Revision: int(row.Revision)}, nil
}

func (r *queryResolver) productCatalog(ctx context.Context, householdID string) ([]*model.ProductConcept, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	householdUUID, _ := gofrsuuid.FromString(householdID)
	rows, err := models.ProductConcepts.Query(sm.Where(models.ProductConcepts.Columns.HouseholdID.EQ(psql.Arg(householdUUID))), sm.OrderBy(models.ProductConcepts.Columns.NormalizedName), sm.OrderBy(models.ProductConcepts.Columns.ID)).All(ctx, bobpgx.NewPool(r.DB))
	if err != nil {
		return nil, err
	}
	result := make([]*model.ProductConcept, 0, len(rows))
	for _, conceptRow := range rows {
		concept, err := loadProductConcept(ctx, bobpgx.NewPool(r.DB), conceptRow.ID.String())
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
	householdUUID, _ := gofrsuuid.FromString(householdID)
	variantRows, err := models.ProductVariants.Query(
		sm.Where(psql.And(models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdUUID)), psql.Not(models.ProductVariants.Columns.Archived))),
		sm.OrderBy(models.ProductVariants.Columns.NormalizedName), sm.OrderBy(models.ProductVariants.Columns.ID),
	).All(ctx, bobpgx.NewPool(r.DB))
	if err != nil {
		return nil, err
	}
	type suggestion struct {
		item                                *model.ProductSuggestion
		conceptName, variantName, variantID string
	}
	items := make([]suggestion, 0, len(variantRows))
	for _, variantRow := range variantRows {
		variant, err := loadProductVariant(ctx, bobpgx.NewPool(r.DB), variantRow.ID.String())
		if err != nil {
			return nil, err
		}
		concept, err := loadProductConcept(ctx, bobpgx.NewPool(r.DB), variant.ConceptID)
		if err != nil {
			return nil, err
		}
		if concept.Archived {
			continue
		}
		matchedAlias := (*model.ProductAlias)(nil)
		if merchantKey != "" {
			for _, alias := range variant.Aliases {
				matchesSearch := alias.MerchantKey == merchantKey && strings.Contains(alias.NormalizedAlias, searchKey)
				isExactMatch := alias.NormalizedAlias == searchKey
				previousIsExactMatch := matchedAlias != nil && matchedAlias.NormalizedAlias == searchKey
				if matchesSearch && (matchedAlias == nil || isExactMatch && !previousIsExactMatch || isExactMatch == previousIsExactMatch && alias.Revision > matchedAlias.Revision) {
					matchedAlias = alias
				}
			}
		}
		if !strings.Contains(variant.NormalizedName, searchKey) && !strings.Contains(concept.NormalizedName, searchKey) && matchedAlias == nil {
			continue
		}
		matched, score := variant.Name, 50
		if matchedAlias != nil {
			matched = matchedAlias.Alias
		}
		switch {
		case matchedAlias != nil && matchedAlias.NormalizedAlias == searchKey:
			score = 100
		case variant.NormalizedName == searchKey:
			score = 95
		case concept.NormalizedName == searchKey:
			score = 90
		case matchedAlias != nil && strings.HasPrefix(matchedAlias.NormalizedAlias, searchKey):
			score = 80
		case strings.HasPrefix(variant.NormalizedName, searchKey):
			score = 75
		case strings.HasPrefix(concept.NormalizedName, searchKey):
			score = 70
		}
		items = append(items, suggestion{item: &model.ProductSuggestion{Concept: concept, Variant: variant, MatchedName: matched, Score: score}, conceptName: concept.NormalizedName, variantName: variant.NormalizedName, variantID: variant.ID})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].item.Score != items[j].item.Score {
			return items[i].item.Score > items[j].item.Score
		}
		if items[i].conceptName != items[j].conceptName {
			return items[i].conceptName < items[j].conceptName
		}
		if items[i].variantName != items[j].variantName {
			return items[i].variantName < items[j].variantName
		}
		return items[i].variantID < items[j].variantID
	})
	if len(items) > pageSize {
		items = items[:pageSize]
	}
	result := make([]*model.ProductSuggestion, 0, len(items))
	for _, item := range items {
		result = append(result, item.item)
	}
	return result, nil
}
