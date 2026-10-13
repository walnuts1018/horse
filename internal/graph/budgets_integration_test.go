package graph

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

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

const testJanuaryFirst2026 = "2026-01-01"

func TestBudgetConnectionPaginatesOverlappingBudgetsWithinHousehold(t *testing.T) {
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
	subject := "budget-connection-integration:" + memberID
	displayName := "budget connection integration"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &displayName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})

	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: displayName})
	query := &queryResolver{Resolver: &Resolver{DB: db}}
	mutation := &mutationResolver{Resolver: query.Resolver}
	household, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "budget connection integration"})
	if err != nil {
		t.Fatal(err)
	}
	householdID, err := gofrsuuid.FromString(household.ID)
	if err != nil {
		t.Fatal(err)
	}
	otherHousehold, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "other budget connection integration"})
	if err != nil {
		t.Fatal(err)
	}
	otherHouseholdID, err := gofrsuuid.FromString(otherHousehold.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.WithoutCancel(t.Context())
		executor := bobpgx.NewPool(db)
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.In(psql.Arg(householdID), psql.Arg(otherHouseholdID)))).Exec(cleanupCtx, executor)
	})

	insertBudget := func(householdID gofrsuuid.UUID, name, startsOn string, endsOn *string) gofrsuuid.UUID {
		t.Helper()
		id := gofrsuuid.Must(gofrsuuid.NewV7())
		start, err := time.Parse(time.DateOnly, startsOn)
		if err != nil {
			t.Fatal(err)
		}
		var end sql.Null[time.Time]
		if endsOn != nil {
			parsedEnd, err := time.Parse(time.DateOnly, *endsOn)
			if err != nil {
				t.Fatal(err)
			}
			end = sql.Null[time.Time]{V: parsedEnd, Valid: true}
		}
		amount := int64(10000)
		if _, err := models.Budgets.Insert(&models.BudgetSetter{ID: &id, HouseholdID: &householdID, Name: &name, AmountYen: &amount, StartsOn: &start, EndsOn: &end}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
			t.Fatal(err)
		}
		return id
	}
	firstEnd := "2026-01-31"
	secondEnd := "2026-02-28"
	firstDuplicate := insertBudget(householdID, "A duplicate", "2025-12-01", &firstEnd)
	secondDuplicate := insertBudget(householdID, "A duplicate", testJanuaryFirst2026, &secondEnd)
	lastMatching := insertBudget(householdID, "B matching", "2026-01-15", nil)
	insertBudget(householdID, "Z excluded", "2026-02-01", nil)
	insertBudget(otherHouseholdID, "C outside household", "2025-12-01", nil)

	pageSize := 1
	firstPage, err := query.budgets(ctx, household.ID, "2026-01", nil, nil, &pageSize, nil)
	if err != nil {
		t.Fatal(err)
	}
	if firstPage.TotalCount != 3 || len(firstPage.Edges) != 1 || firstPage.Edges[0].Node.Name != "A duplicate" || !firstPage.PageInfo.HasNextPage || firstPage.PageInfo.HasPreviousPage {
		t.Fatalf("unexpected first budget page: %#v", firstPage)
	}
	secondPage, err := query.budgets(ctx, household.ID, "2026-01", firstPage.PageInfo.EndCursor, nil, &pageSize, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondPage.Edges) != 1 || secondPage.Edges[0].Node.Name != "A duplicate" || !secondPage.PageInfo.HasNextPage || !secondPage.PageInfo.HasPreviousPage {
		t.Fatalf("unexpected second budget page: %#v", secondPage)
	}
	thirdPage, err := query.budgets(ctx, household.ID, "2026-01", secondPage.PageInfo.EndCursor, nil, &pageSize, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(thirdPage.Edges) != 1 || thirdPage.Edges[0].Node.Name != "B matching" || thirdPage.PageInfo.HasNextPage || !thirdPage.PageInfo.HasPreviousPage {
		t.Fatalf("unexpected third budget page: %#v", thirdPage)
	}
	firstID, secondID := firstDuplicate.String(), secondDuplicate.String()
	if secondID < firstID {
		firstID, secondID = secondID, firstID
	}
	if firstPage.Edges[0].Cursor != encodeCursor(nodeTypeBudget, firstID) || secondPage.Edges[0].Cursor != encodeCursor(nodeTypeBudget, secondID) || thirdPage.Edges[0].Cursor != encodeCursor(nodeTypeBudget, lastMatching.String()) {
		t.Fatalf("budget cursor order did not preserve name and id ordering: %s, %s, %s", firstPage.Edges[0].Cursor, secondPage.Edges[0].Cursor, thirdPage.Edges[0].Cursor)
	}
	lastPage, err := query.budgets(ctx, household.ID, "2026-01", nil, thirdPage.PageInfo.StartCursor, nil, &pageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(lastPage.Edges) != 1 || lastPage.Edges[0].Cursor != secondPage.Edges[0].Cursor || !lastPage.PageInfo.HasNextPage || !lastPage.PageInfo.HasPreviousPage {
		t.Fatalf("unexpected backward page before duplicate-name boundary: %#v", lastPage)
	}
}

func TestBudgetConnectionLoadsPostedSpendingAndCorrections(t *testing.T) {
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
	memberUUID := generatedUUID(memberID)
	subject := "budget-actual-integration:" + memberID
	displayName := "budget actual integration"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &displayName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: displayName})
	resolver := &mutationResolver{Resolver: &Resolver{DB: db}}
	household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "budget actual integration"})
	if err != nil {
		t.Fatal(err)
	}
	householdUUID := generatedUUID(household.ID)
	t.Cleanup(func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})
	pool, err := resolver.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "Budget actual", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	account, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Budget actual card", Kind: model.AccountKindCard, PoolID: &pool.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{HouseholdID: household.ID, PoolID: pool.ID, Configuration: personalPoolFundingConfiguration(memberID), ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.setDefaultAllocation(ctx, model.SetDefaultAllocationInput{HouseholdID: household.ID, Allocations: []*model.AllocationInput{{PoolID: pool.ID, Weight: 1}}}); err != nil {
		t.Fatal(err)
	}
	category, err := resolver.createCategory(ctx, model.CreateCategoryInput{HouseholdID: household.ID, Name: "Food"})
	if err != nil {
		t.Fatal(err)
	}
	otherCategory, err := resolver.createCategory(ctx, model.CreateCategoryInput{HouseholdID: household.ID, Name: "Household"})
	if err != nil {
		t.Fatal(err)
	}
	categoryPurchase, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: "2026-01-15T12:00:00Z", Merchant: "Budget actual shop",
		PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: account.ID, AmountYen: 200}},
		Items:              []*model.PurchaseItemInput{{Name: "Food", AmountYen: 120, CategoryID: &category.ID}, {Name: "Household", AmountYen: 80, CategoryID: &otherCategory.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.postPurchase(ctx, categoryPurchase.ID, categoryPurchase.Revision, uuid.NewV7().String()); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.correctPurchase(ctx, model.CorrectPurchaseInput{
		HouseholdID:    household.ID,
		Target:         &model.CorrectPurchaseTargetInput{Item: &model.PurchaseCorrectionItemTargetInput{PurchaseID: categoryPurchase.ID, ItemID: categoryPurchase.Items[0].ID}},
		AmountDeltaYen: -20, Reason: "refund", IdempotencyKey: uuid.NewV7().String(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: "2026-01-16T12:00:00Z", Merchant: "Budget draft",
		PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: account.ID, AmountYen: 900}},
		Items:              []*model.PurchaseItemInput{{Name: "Draft", AmountYen: 900, CategoryID: &category.ID}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.setBudget(ctx, budgetWriteInput{HouseholdID: household.ID, Name: "All spending", AmountYen: 1000, StartsOn: testJanuaryFirst2026}); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.setBudget(ctx, budgetWriteInput{HouseholdID: household.ID, CategoryID: &category.ID, Name: testFoodCategoryName, AmountYen: 500, StartsOn: testJanuaryFirst2026}); err != nil {
		t.Fatal(err)
	}

	connection, err := (&queryResolver{Resolver: resolver.Resolver}).budgets(ctx, household.ID, "2026-01", nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	actualByName := make(map[string]int64, len(connection.Edges))
	for _, edge := range connection.Edges {
		actualByName[edge.Node.Name] = int64(edge.Node.ActualYen)
	}
	if actualByName["All spending"] != 180 || actualByName["Food"] != 100 {
		t.Fatalf("budget actuals = %#v, want all spending 180 and food 100", actualByName)
	}

	outsiderID := uuid.NewV7().String()
	outsiderSubject := "budget-outsider:" + outsiderID
	outsiderName := "budget outsider"
	outsiderUUID := generatedUUID(outsiderID)
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &outsiderUUID, OidcSubject: &outsiderSubject, DisplayName: &outsiderName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(outsiderUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})
	outsiderCtx := auth.WithPrincipal(ctx, auth.Principal{MemberID: outsiderID, Subject: outsiderSubject, DisplayName: outsiderName})
	if _, err := (&queryResolver{Resolver: resolver.Resolver}).budgets(outsiderCtx, household.ID, "2026-01", nil, nil, nil, nil); err == nil {
		t.Fatal("budget query succeeded for a non-member")
	}
}
