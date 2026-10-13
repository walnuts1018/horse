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

func optionalProductAttribute(value string) *string {
	if value == "" {
		return nil
	}
	return &value
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
	variant := model.ProductVariant{ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), ConceptID: row.ConceptID.String(), Name: row.Name, NormalizedName: row.NormalizedName, Brand: optionalProductAttribute(row.Brand), PackageSize: optionalProductAttribute(row.PackageSize), Archived: row.Archived, Revision: int(row.Revision)}
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

func (r *queryResolver) productCatalog(ctx context.Context, householdID string, after *string, first *int) (*model.ProductConceptConnection, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	pageSize := 50
	if first != nil {
		pageSize = *first
	}
	if pageSize < 1 || pageSize > 100 {
		return nil, fmt.Errorf("%w: page size must be between 1 and 100", ErrBadInput)
	}
	householdUUID, _ := gofrsuuid.FromString(householdID)
	filters := []bob.Expression{models.ProductConcepts.Columns.HouseholdID.EQ(psql.Arg(householdUUID))}
	db := bobpgx.NewPool(r.DB)
	var previousPage bool
	if after != nil && *after != "" {
		value, err := decodeCursor(*after, "ProductConcept")
		if err != nil {
			return nil, err
		}
		name, id, ok := strings.Cut(value, "\x00")
		if !ok || name == "" || id == "" {
			return nil, fmt.Errorf("%w: invalid ProductConcept cursor", ErrBadInput)
		}
		cursorUUID, err := gofrsuuid.FromString(id)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid ProductConcept cursor", ErrBadInput)
		}
		filters = append(filters, psql.Or(
			models.ProductConcepts.Columns.NormalizedName.GT(psql.Arg(name)),
			psql.And(models.ProductConcepts.Columns.NormalizedName.EQ(psql.Arg(name)), models.ProductConcepts.Columns.ID.GT(psql.Arg(cursorUUID))),
		))
		previousPage = true
	}
	rows, err := models.ProductConcepts.Query(
		sm.Where(psql.And(filters...)),
		sm.OrderBy(models.ProductConcepts.Columns.NormalizedName),
		sm.OrderBy(models.ProductConcepts.Columns.ID),
		sm.Limit(pageSize+1),
	).All(ctx, db)
	if err != nil {
		return nil, err
	}
	hasNextPage := len(rows) > pageSize
	if hasNextPage {
		rows = rows[:pageSize]
	}
	connection := &model.ProductConceptConnection{
		Edges:    make([]*model.ProductConceptEdge, 0, len(rows)),
		PageInfo: &model.PageInfo{HasPreviousPage: previousPage, HasNextPage: hasNextPage},
	}
	for _, conceptRow := range rows {
		concept, err := loadProductConcept(ctx, db, conceptRow.ID.String())
		if err != nil {
			return nil, err
		}
		cursor := encodeCursor("ProductConcept", conceptRow.NormalizedName+"\x00"+conceptRow.ID.String())
		connection.Edges = append(connection.Edges, &model.ProductConceptEdge{Cursor: cursor, Node: concept})
	}
	if len(connection.Edges) > 0 {
		start := connection.Edges[0].Cursor
		end := connection.Edges[len(connection.Edges)-1].Cursor
		connection.PageInfo.StartCursor = &start
		connection.PageInfo.EndCursor = &end
	}
	return connection, nil
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
	db := bobpgx.NewPool(r.DB)
	searchPattern := "%" + escapeProductSearchPattern(searchKey) + "%"
	const candidateLimit = 500
	variantRows, err := models.ProductVariants.Query(
		sm.Where(psql.And(models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdUUID)), psql.Not(models.ProductVariants.Columns.Archived), models.ProductVariants.Columns.NormalizedName.Like(psql.Arg(searchPattern)))),
		sm.OrderBy(models.ProductVariants.Columns.NormalizedName), sm.OrderBy(models.ProductVariants.Columns.ID), sm.Limit(candidateLimit),
	).All(ctx, db)
	if err != nil {
		return nil, err
	}
	variantByID := make(map[gofrsuuid.UUID]*models.ProductVariant, len(variantRows))
	for _, variantRow := range variantRows {
		variantByID[variantRow.ID] = variantRow
	}
	conceptRows, err := models.ProductConcepts.Query(
		sm.Where(psql.And(models.ProductConcepts.Columns.HouseholdID.EQ(psql.Arg(householdUUID)), psql.Not(models.ProductConcepts.Columns.Archived), models.ProductConcepts.Columns.NormalizedName.Like(psql.Arg(searchPattern)))),
		sm.OrderBy(models.ProductConcepts.Columns.NormalizedName), sm.OrderBy(models.ProductConcepts.Columns.ID), sm.Limit(candidateLimit),
	).All(ctx, db)
	if err != nil {
		return nil, err
	}
	conceptIDs := make([]any, 0, len(conceptRows))
	for _, conceptRow := range conceptRows {
		conceptIDs = append(conceptIDs, conceptRow.ID)
	}
	if len(conceptIDs) > 0 {
		conceptVariantRows, err := models.ProductVariants.Query(
			sm.Where(psql.And(models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdUUID)), psql.Not(models.ProductVariants.Columns.Archived), models.ProductVariants.Columns.ConceptID.In(psql.Arg(conceptIDs...)))),
			sm.OrderBy(models.ProductVariants.Columns.NormalizedName), sm.OrderBy(models.ProductVariants.Columns.ID), sm.Limit(candidateLimit),
		).All(ctx, db)
		if err != nil {
			return nil, err
		}
		for _, variantRow := range conceptVariantRows {
			variantByID[variantRow.ID] = variantRow
		}
	}
	matchedAliases := make(map[gofrsuuid.UUID]*models.ProductAlias)
	if merchantKey != "" {
		aliasRows, err := models.ProductAliases.Query(
			sm.Where(psql.And(models.ProductAliases.Columns.HouseholdID.EQ(psql.Arg(householdUUID)), models.ProductAliases.Columns.MerchantKey.EQ(psql.Arg(merchantKey)), models.ProductAliases.Columns.NormalizedAlias.Like(psql.Arg(searchPattern)))),
			sm.OrderBy(models.ProductAliases.Columns.NormalizedAlias), sm.OrderBy(models.ProductAliases.Columns.ID), sm.Limit(candidateLimit),
		).All(ctx, db)
		if err != nil {
			return nil, err
		}
		aliasVariantIDs := make([]any, 0, len(aliasRows))
		for _, aliasRow := range aliasRows {
			aliasVariantIDs = append(aliasVariantIDs, aliasRow.VariantID)
			matchedAliases[aliasRow.VariantID] = preferredProductAlias(matchedAliases[aliasRow.VariantID], aliasRow, searchKey)
		}
		if len(aliasVariantIDs) > 0 {
			aliasVariantRows, err := models.ProductVariants.Query(
				sm.Where(psql.And(models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdUUID)), models.ProductVariants.Columns.ID.In(psql.Arg(aliasVariantIDs...)), psql.Not(models.ProductVariants.Columns.Archived))),
			).All(ctx, db)
			if err != nil {
				return nil, err
			}
			for _, variantRow := range aliasVariantRows {
				variantByID[variantRow.ID] = variantRow
			}
		}
	}
	conceptByID := make(map[gofrsuuid.UUID]*models.ProductConcept, len(conceptRows))
	for _, conceptRow := range conceptRows {
		conceptByID[conceptRow.ID] = conceptRow
	}
	conceptUUIDs := make([]any, 0, len(variantByID))
	for _, variantRow := range variantByID {
		conceptUUIDs = append(conceptUUIDs, variantRow.ConceptID)
	}
	if len(conceptUUIDs) > 0 {
		rows, err := models.ProductConcepts.Query(
			sm.Where(psql.And(models.ProductConcepts.Columns.HouseholdID.EQ(psql.Arg(householdUUID)), models.ProductConcepts.Columns.ID.In(psql.Arg(conceptUUIDs...)), psql.Not(models.ProductConcepts.Columns.Archived))),
		).All(ctx, db)
		if err != nil {
			return nil, err
		}
		for _, conceptRow := range rows {
			conceptByID[conceptRow.ID] = conceptRow
		}
	}
	type suggestion struct {
		variant                             *models.ProductVariant
		concept                             *models.ProductConcept
		score                               int
		conceptName, variantName, variantID string
		matchedName                         string
	}
	items := make([]suggestion, 0, len(variantByID))
	for _, variantRow := range variantByID {
		conceptRow := conceptByID[variantRow.ConceptID]
		if conceptRow == nil {
			continue
		}
		matchedAlias := matchedAliases[variantRow.ID]
		matchedName, score := variantRow.Name, 50
		if matchedAlias != nil {
			matchedName = matchedAlias.AliasText
		}
		switch {
		case matchedAlias != nil && matchedAlias.NormalizedAlias == searchKey:
			score = 100
		case variantRow.NormalizedName == searchKey:
			score = 95
		case conceptRow.NormalizedName == searchKey:
			score = 90
		case matchedAlias != nil && strings.HasPrefix(matchedAlias.NormalizedAlias, searchKey):
			score = 80
		case strings.HasPrefix(variantRow.NormalizedName, searchKey):
			score = 75
		case strings.HasPrefix(conceptRow.NormalizedName, searchKey):
			score = 70
		}
		items = append(items, suggestion{variant: variantRow, concept: conceptRow, score: score, conceptName: conceptRow.NormalizedName, variantName: variantRow.NormalizedName, variantID: variantRow.ID.String(), matchedName: matchedName})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
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
	if len(items) == 0 {
		return []*model.ProductSuggestion{}, nil
	}
	selectedConceptIDs := make(map[gofrsuuid.UUID]struct{}, len(items))
	for _, item := range items {
		selectedConceptIDs[item.concept.ID] = struct{}{}
	}
	selectedConceptUUIDs := make([]any, 0, len(selectedConceptIDs))
	for id := range selectedConceptIDs {
		selectedConceptUUIDs = append(selectedConceptUUIDs, id)
	}
	selectedVariantRows, err := models.ProductVariants.Query(
		sm.Where(psql.And(models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdUUID)), models.ProductVariants.Columns.ConceptID.In(psql.Arg(selectedConceptUUIDs...)))),
		sm.OrderBy(models.ProductVariants.Columns.NormalizedName), sm.OrderBy(models.ProductVariants.Columns.ID),
	).All(ctx, db)
	if err != nil {
		return nil, err
	}
	selectedVariantIDs := make([]any, 0, len(selectedVariantRows))
	for _, variantRow := range selectedVariantRows {
		selectedVariantIDs = append(selectedVariantIDs, variantRow.ID)
	}
	aliasesByVariant := make(map[gofrsuuid.UUID][]*model.ProductAlias, len(selectedVariantRows))
	if len(selectedVariantIDs) > 0 {
		aliasRows, err := models.ProductAliases.Query(
			sm.Where(psql.And(models.ProductAliases.Columns.HouseholdID.EQ(psql.Arg(householdUUID)), models.ProductAliases.Columns.VariantID.In(psql.Arg(selectedVariantIDs...)))),
			sm.OrderBy(models.ProductAliases.Columns.MerchantKey), sm.OrderBy(models.ProductAliases.Columns.NormalizedAlias),
		).All(ctx, db)
		if err != nil {
			return nil, err
		}
		for _, aliasRow := range aliasRows {
			aliasesByVariant[aliasRow.VariantID] = append(aliasesByVariant[aliasRow.VariantID], productAliasFromRow(aliasRow))
		}
	}
	variantsByID := make(map[gofrsuuid.UUID]*model.ProductVariant, len(selectedVariantRows))
	variantsByConcept := make(map[gofrsuuid.UUID][]*model.ProductVariant, len(selectedConceptUUIDs))
	for _, variantRow := range selectedVariantRows {
		variant := productVariantFromRow(variantRow)
		variant.Aliases = aliasesByVariant[variantRow.ID]
		variantsByID[variantRow.ID] = variant
		variantsByConcept[variantRow.ConceptID] = append(variantsByConcept[variantRow.ConceptID], variant)
	}
	conceptsByID := make(map[gofrsuuid.UUID]*model.ProductConcept, len(selectedConceptIDs))
	for _, item := range items {
		if _, ok := conceptsByID[item.concept.ID]; !ok {
			concept := productConceptFromRow(item.concept)
			concept.Variants = variantsByConcept[item.concept.ID]
			conceptsByID[item.concept.ID] = concept
		}
	}
	result := make([]*model.ProductSuggestion, 0, len(items))
	for _, item := range items {
		result = append(result, &model.ProductSuggestion{Concept: conceptsByID[item.concept.ID], Variant: variantsByID[item.variant.ID], MatchedName: item.matchedName, Score: item.score})
	}
	return result, nil
}

func escapeProductSearchPattern(value string) string {
	return strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(value)
}

func preferredProductAlias(current, candidate *models.ProductAlias, searchKey string) *models.ProductAlias {
	if current == nil {
		return candidate
	}
	candidateExact := candidate.NormalizedAlias == searchKey
	currentExact := current.NormalizedAlias == searchKey
	if candidateExact != currentExact {
		if candidateExact {
			return candidate
		}
		return current
	}
	if candidate.Revision > current.Revision || candidate.Revision == current.Revision && candidate.ID.String() < current.ID.String() {
		return candidate
	}
	return current
}

func productConceptFromRow(row *models.ProductConcept) *model.ProductConcept {
	concept := &model.ProductConcept{ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), Name: row.Name, NormalizedName: row.NormalizedName, Archived: row.Archived, Revision: int(row.Revision)}
	if row.CategoryID.Valid {
		categoryID := row.CategoryID.V.String()
		concept.CategoryID = &categoryID
	}
	return concept
}

func productVariantFromRow(row *models.ProductVariant) *model.ProductVariant {
	variant := &model.ProductVariant{ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), ConceptID: row.ConceptID.String(), Name: row.Name, NormalizedName: row.NormalizedName, Brand: optionalProductAttribute(row.Brand), PackageSize: optionalProductAttribute(row.PackageSize), Archived: row.Archived, Revision: int(row.Revision)}
	if row.Barcode.Valid {
		variant.Barcode = &row.Barcode.V
	}
	return variant
}

func productAliasFromRow(row *models.ProductAlias) *model.ProductAlias {
	return &model.ProductAlias{ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), VariantID: row.VariantID.String(), MerchantKey: row.MerchantKey, Alias: row.AliasText, NormalizedAlias: row.NormalizedAlias, Revision: int(row.Revision)}
}
