package graph

import (
	"context"
	"fmt"
	"os"
	"testing"

	"uuid"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestProductCatalogAndSuggestionsPreserveExpectedOrdering(t *testing.T) {
	databaseURL := os.Getenv("HORSE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("HORSE_TEST_DATABASE_URL is not set")
	}
	ctx := t.Context()
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	memberID := uuid.NewV7().String()
	memberUUID, err := gofrsuuid.FromString(memberID)
	if err != nil {
		t.Fatal(err)
	}
	subject := "product-catalog-integration:" + memberID
	displayName := "product catalog integration"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &displayName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(context.Background(), bobpgx.NewPool(db))
	})

	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: displayName})
	resolver := &queryResolver{Resolver: (&Resolver{DB: db})}
	household, err := (&mutationResolver{Resolver: resolver.Resolver}).createHousehold(ctx, model.CreateHouseholdInput{Name: "product catalog integration"})
	if err != nil {
		t.Fatal(err)
	}
	householdUUID, err := gofrsuuid.FromString(household.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(context.Background(), bobpgx.NewPool(db))
	})

	mutation := &mutationResolver{Resolver: resolver.Resolver}
	for _, name := range []string{"Apple", "Miso", "Tea"} {
		if _, err := mutation.createProductConcept(ctx, model.CreateProductConceptInput{HouseholdID: household.ID, Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	pageSize := 2
	firstPage, err := resolver.productCatalog(ctx, household.ID, nil, &pageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPage.Edges) != 2 || !firstPage.PageInfo.HasNextPage || firstPage.PageInfo.HasPreviousPage || firstPage.PageInfo.EndCursor == nil {
		t.Fatalf("first page has invalid pagination metadata: %#v", firstPage.PageInfo)
	}
	if firstPage.Edges[0].Node.Name != "Apple" || firstPage.Edges[1].Node.Name != "Miso" {
		t.Fatalf("first page is not in stable name order: %#v", firstPage.Edges)
	}

	secondPage, err := resolver.productCatalog(ctx, household.ID, firstPage.PageInfo.EndCursor, &pageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondPage.Edges) != 1 || secondPage.PageInfo.HasNextPage || !secondPage.PageInfo.HasPreviousPage || secondPage.Edges[0].Node.Name != "Tea" {
		t.Fatalf("second page does not continue after the cursor: page=%#v info=%#v", secondPage.Edges, secondPage.PageInfo)
	}

	suggestionConcept, err := mutation.createProductConcept(ctx, model.CreateProductConceptInput{HouseholdID: household.ID, Name: "Household products"})
	if err != nil {
		t.Fatal(err)
	}
	suggestionConceptUUID, err := gofrsuuid.FromString(suggestionConcept.ID)
	if err != nil {
		t.Fatal(err)
	}
	suggestionConceptRow := &models.ProductConcept{ID: suggestionConceptUUID, HouseholdID: householdUUID}
	variantSetters := make([]*models.ProductVariantSetter, 0, 501)
	for i := range 500 {
		id, err := gofrsuuid.FromString(uuid.NewV7().String())
		if err != nil {
			t.Fatal(err)
		}
		name := fmt.Sprintf("aa%03dmilk", i)
		variantSetters = append(variantSetters, &models.ProductVariantSetter{ID: &id, Name: &name, NormalizedName: &name})
	}
	exactID, err := gofrsuuid.FromString(uuid.NewV7().String())
	if err != nil {
		t.Fatal(err)
	}
	exactName := "milk"
	variantSetters = append(variantSetters, &models.ProductVariantSetter{ID: &exactID, Name: &exactName, NormalizedName: &exactName})
	if err := suggestionConceptRow.InsertProductVariants(ctx, bobpgx.NewPool(db), variantSetters...); err != nil {
		t.Fatal(err)
	}
	suggestions, err := resolver.productSuggestions(ctx, household.ID, "milk", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(suggestions) == 0 || suggestions[0].Variant.Name != "milk" || suggestions[0].Score != 95 {
		t.Fatalf("exact product match was omitted behind partial matches: %#v", suggestions)
	}
}
