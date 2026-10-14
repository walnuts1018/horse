package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	productapp "github.com/walnuts1018/horse/internal/application/product"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
	productquery "github.com/walnuts1018/horse/internal/postgres/query"
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
	created, err := productapp.NewService(r.DB).CreateConcept(ctx, productapp.CreateConceptCommand{
		ActorID: user.MemberID, HouseholdID: input.HouseholdID, Name: input.Name, CategoryID: input.CategoryID,
	})
	if err != nil {
		return nil, err
	}
	return loadProductConcept(ctx, bobpgx.NewPool(r.DB), created.ID)
}

func (r *mutationResolver) createProductVariant(ctx context.Context, input model.CreateProductVariantInput) (*model.ProductVariant, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	created, err := productapp.NewService(r.DB).CreateVariant(ctx, productapp.CreateVariantCommand{
		ActorID: user.MemberID, HouseholdID: input.HouseholdID, ConceptID: input.ConceptID, Name: input.Name,
		Brand: input.Brand, PackageSize: input.PackageSize, Barcode: input.Barcode,
	})
	if err != nil {
		return nil, err
	}
	return loadProductVariant(ctx, bobpgx.NewPool(r.DB), created.ID)
}

func (r *mutationResolver) setProductAlias(ctx context.Context, input model.SetProductAliasInput) (*model.ProductAlias, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	alias, err := productapp.NewService(r.DB).SetAlias(ctx, productapp.SetAliasCommand{
		ActorID: user.MemberID, HouseholdID: input.HouseholdID, VariantID: input.VariantID, Merchant: input.Merchant, Alias: input.Alias,
	})
	if err != nil {
		return nil, err
	}
	return loadProductAlias(ctx, bobpgx.NewPool(r.DB), alias.ID)
}

func optionalProductAttribute(value string) *string {
	if value == "" {
		return nil
	}
	return &value
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

func (r *queryResolver) productCatalog(ctx context.Context, householdID string, after, before *string, first, last *int) (*model.ProductConceptConnection, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	pageSize, backward, err := pageDirection(first, last)
	if err != nil {
		return nil, err
	}
	householdUUID, _ := gofrsuuid.FromString(householdID)
	filters := []bob.Expression{models.ProductConcepts.Columns.HouseholdID.EQ(psql.Arg(householdUUID))}
	db := bobpgx.NewPool(r.DB)
	if after != nil && *after != "" {
		value, err := decodeCursor(*after, nodeTypeProductConcept)
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
	}
	if before != nil && *before != "" {
		value, err := decodeCursor(*before, nodeTypeProductConcept)
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
			models.ProductConcepts.Columns.NormalizedName.LT(psql.Arg(name)),
			psql.And(models.ProductConcepts.Columns.NormalizedName.EQ(psql.Arg(name)), models.ProductConcepts.Columns.ID.LT(psql.Arg(cursorUUID))),
		))
	}
	nameOrder := sm.OrderBy(models.ProductConcepts.Columns.NormalizedName)
	idOrder := sm.OrderBy(models.ProductConcepts.Columns.ID)
	if backward {
		nameOrder = nameOrder.Desc()
		idOrder = idOrder.Desc()
	}
	rows, err := models.ProductConcepts.Query(
		sm.Where(psql.And(filters...)),
		nameOrder,
		idOrder,
		sm.Limit(pageSize+1),
	).All(ctx, db)
	if err != nil {
		return nil, err
	}
	hasExtra := len(rows) > pageSize
	if hasExtra {
		rows = rows[:pageSize]
	}
	if backward {
		slices.Reverse(rows)
	}
	pageInfo := &model.PageInfo{HasPreviousPage: backward && hasExtra, HasNextPage: !backward && hasExtra}
	if !backward && after != nil && *after != "" {
		pageInfo.HasPreviousPage = true
	}
	if backward && before != nil && *before != "" {
		pageInfo.HasNextPage = true
	}
	connection := &model.ProductConceptConnection{
		Edges:    make([]*model.ProductConceptEdge, 0, len(rows)),
		PageInfo: pageInfo,
	}
	conceptIDs := make([]any, 0, len(rows))
	for _, conceptRow := range rows {
		conceptIDs = append(conceptIDs, conceptRow.ID)
	}
	variantsByConcept := make(map[gofrsuuid.UUID][]*model.ProductVariant, len(rows))
	if len(conceptIDs) > 0 {
		variantRows, err := models.ProductVariants.Query(
			sm.Where(psql.And(models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdUUID)), models.ProductVariants.Columns.ConceptID.In(psql.Arg(conceptIDs...)))),
			sm.OrderBy(models.ProductVariants.Columns.ConceptID),
			sm.OrderBy(models.ProductVariants.Columns.NormalizedName),
			sm.OrderBy(models.ProductVariants.Columns.ID),
		).All(ctx, db)
		if err != nil {
			return nil, err
		}
		variantIDs := make([]any, 0, len(variantRows))
		aliasesByVariant := make(map[gofrsuuid.UUID][]*model.ProductAlias, len(variantRows))
		for _, variantRow := range variantRows {
			variantIDs = append(variantIDs, variantRow.ID)
			aliasesByVariant[variantRow.ID] = make([]*model.ProductAlias, 0)
		}
		if len(variantIDs) > 0 {
			aliasRows, err := models.ProductAliases.Query(
				sm.Where(psql.And(models.ProductAliases.Columns.HouseholdID.EQ(psql.Arg(householdUUID)), models.ProductAliases.Columns.VariantID.In(psql.Arg(variantIDs...)))),
				sm.OrderBy(models.ProductAliases.Columns.VariantID),
				sm.OrderBy(models.ProductAliases.Columns.MerchantKey),
				sm.OrderBy(models.ProductAliases.Columns.NormalizedAlias),
			).All(ctx, db)
			if err != nil {
				return nil, err
			}
			for _, aliasRow := range aliasRows {
				aliasesByVariant[aliasRow.VariantID] = append(aliasesByVariant[aliasRow.VariantID], productAliasFromRow(aliasRow))
			}
		}
		for _, variantRow := range variantRows {
			variant := productVariantFromRow(variantRow)
			variant.Aliases = aliasesByVariant[variantRow.ID]
			if _, ok := variantsByConcept[variantRow.ConceptID]; !ok {
				variantsByConcept[variantRow.ConceptID] = make([]*model.ProductVariant, 0)
			}
			variantsByConcept[variantRow.ConceptID] = append(variantsByConcept[variantRow.ConceptID], variant)
		}
	}
	for _, conceptRow := range rows {
		concept := productConceptFromRow(conceptRow)
		if variants, ok := variantsByConcept[conceptRow.ID]; ok {
			concept.Variants = variants
		} else {
			concept.Variants = make([]*model.ProductVariant, 0)
		}
		cursor := encodeCursor(nodeTypeProductConcept, conceptRow.NormalizedName+"\x00"+conceptRow.ID.String())
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

func (r *queryResolver) productSuggestions(ctx context.Context, householdID, queryText string, merchant *string, first *int) ([]*model.ProductSuggestion, error) {
	user, err := requireMember(ctx, r.DB, householdID)
	if err != nil {
		return nil, err
	}
	searchKey := normalizeProductText(queryText)
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
	memberUUID, err := gofrsuuid.FromString(user.MemberID)
	if err != nil {
		return nil, fmt.Errorf("parse member id: %w", err)
	}
	suggestions, err := productquery.SearchProductSuggestions(ctx, bobpgx.NewPool(r.DB), householdUUID, memberUUID, searchKey, merchantKey, pageSize)
	if err != nil {
		return nil, err
	}
	variantsByID := make(map[gofrsuuid.UUID]*model.ProductVariant)
	variantsByConcept := make(map[gofrsuuid.UUID][]*model.ProductVariant, len(suggestions.VariantsByConcept))
	for conceptID, variantRows := range suggestions.VariantsByConcept {
		for _, variantRow := range variantRows {
			variant := productVariantFromRow(variantRow)
			for _, aliasRow := range suggestions.AliasesByVariant[variantRow.ID] {
				variant.Aliases = append(variant.Aliases, productAliasFromRow(aliasRow))
			}
			variantsByID[variantRow.ID] = variant
			variantsByConcept[conceptID] = append(variantsByConcept[conceptID], variant)
		}
	}
	conceptsByID := make(map[gofrsuuid.UUID]*model.ProductConcept)
	for _, suggestion := range suggestions.Items {
		if _, ok := conceptsByID[suggestion.Concept.ID]; !ok {
			concept := productConceptFromRow(suggestion.Concept)
			concept.Variants = variantsByConcept[suggestion.Concept.ID]
			conceptsByID[suggestion.Concept.ID] = concept
		}
	}
	result := make([]*model.ProductSuggestion, 0, len(suggestions.Items))
	for _, suggestion := range suggestions.Items {
		result = append(result, &model.ProductSuggestion{
			Concept:     conceptsByID[suggestion.Concept.ID],
			Variant:     variantsByID[suggestion.Variant.ID],
			MatchedName: suggestion.MatchedName,
			Score:       suggestion.Score,
		})
	}
	return result, nil
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
