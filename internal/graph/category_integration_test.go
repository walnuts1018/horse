package graph

import (
	"context"
	"os"
	"testing"

	"uuid"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestTopLevelCategoryNamesAreUniquePerHousehold(t *testing.T) {
	t.Parallel()
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
	subject := "category-integration:" + memberID
	displayName := "category integration"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &displayName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})

	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: displayName})
	resolver := &mutationResolver{Resolver: (&Resolver{DB: db})}
	household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "category integration"})
	if err != nil {
		t.Fatal(err)
	}
	householdUUID, err := gofrsuuid.FromString(household.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})

	if _, err := resolver.createCategory(ctx, model.CreateCategoryInput{HouseholdID: household.ID, Name: testFoodCategoryName}); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.createCategory(ctx, model.CreateCategoryInput{HouseholdID: household.ID, Name: testFoodCategoryName}); err == nil {
		t.Fatal("database must reject duplicate top-level category names within a household")
	}
}

func TestCategoryNameUpdateRecordsOnlyChangedNames(t *testing.T) {
	t.Parallel()
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
	subject := "category-name-update:" + memberID
	displayName := "category name update"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &displayName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})

	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: displayName})
	resolver := &mutationResolver{Resolver: (&Resolver{DB: db})}
	household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "category name update"})
	if err != nil {
		t.Fatal(err)
	}
	householdUUID, err := gofrsuuid.FromString(household.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})

	category, err := resolver.createCategory(ctx, model.CreateCategoryInput{HouseholdID: household.ID, Name: "Food"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := resolver.UpdateCategoryName(ctx, model.UpdateCategoryNameInput{CategoryID: category.ID, Name: "  Groceries  "})
	if err != nil {
		t.Fatal(err)
	}
	success, ok := result.(*model.UpdateCategoryNameSuccess)
	if !ok {
		t.Fatalf("category name update result = %T; want UpdateCategoryNameSuccess", result)
	}
	updated := success.Result
	if updated.Name != "Groceries" {
		t.Fatalf("updated category name = %q; want Groceries", updated.Name)
	}
	if _, err := resolver.UpdateCategoryName(ctx, model.UpdateCategoryNameInput{CategoryID: category.ID, Name: "Groceries"}); err != nil {
		t.Fatal(err)
	}

	categoryUUID, err := gofrsuuid.FromString(category.ID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := models.HouseholdEvents.Query(sm.Where(psql.And(
		models.HouseholdEvents.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.HouseholdEvents.Columns.EntityID.EQ(psql.Arg(categoryUUID)),
		models.HouseholdEvents.Columns.Type.EQ(psql.Arg("category.name_updated")),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("category.name_updated event count = %d; want 1", len(events))
	}
}
