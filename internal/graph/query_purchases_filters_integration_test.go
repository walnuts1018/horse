package graph

import (
	"context"
	"errors"
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

func TestPurchasesApplyFiltersBeforePagination(t *testing.T) {
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

	memberUUID, err := gofrsuuid.FromString(uuid.NewV7().String())
	if err != nil {
		t.Fatal(err)
	}
	subject := "purchase-filter-integration:" + memberUUID.String()
	displayName := "purchase filter integration"
	executor := bobpgx.NewPool(db)
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &displayName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(context.WithoutCancel(t.Context()), executor)
	})
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberUUID.String(), Subject: subject, DisplayName: displayName})
	resolver := &Resolver{DB: db}
	mutation := &mutationResolver{Resolver: resolver}
	household, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "purchase filter integration"})
	if err != nil {
		t.Fatal(err)
	}
	householdUUID, err := gofrsuuid.FromString(household.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(context.WithoutCancel(t.Context()), executor)
	})
	category, err := mutation.createCategory(ctx, model.CreateCategoryInput{HouseholdID: household.ID, Name: "Groceries"})
	if err != nil {
		t.Fatal(err)
	}
	account, err := mutation.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Cash", Kind: model.AccountKindCash})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "Personal", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.setDefaultAllocation(ctx, model.SetDefaultAllocationInput{HouseholdID: household.ID, Allocations: []*model.AllocationInput{{PoolID: pool.ID, Weight: 1}}}); err != nil {
		t.Fatal(err)
	}
	tag, err := mutation.createTag(ctx, model.CreateTagInput{HouseholdID: household.ID, Name: "weekly"})
	if err != nil {
		t.Fatal(err)
	}
	makePurchase := func(merchant, itemName, date string, categoryID, accountID, poolID *string) *model.Purchase {
		t.Helper()
		input := model.CreatePurchaseInput{
			IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID,
			OccurredAt: model.DateTime(date), Merchant: merchant,
			Items: []*model.PurchaseItemInput{{Name: itemName, AmountYen: 100, CategoryID: categoryID}},
		}
		if poolID != nil {
			input.Items[0].Allocations = []*model.AllocationInput{{PoolID: *poolID, Weight: 1}}
		}
		if accountID != nil {
			input.PaymentAllocations = []*model.PurchasePaymentInput{{PaymentAccountID: *accountID, AmountYen: 100}}
		}
		purchase, err := mutation.createPurchase(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		return purchase
	}
	matched := makePurchase("Market", "Apple", "2026-04-10T12:00:00Z", &category.ID, &account.ID, &pool.ID)
	matchedLater := makePurchase("Market", "Apple Juice", "2026-04-20T12:00:00Z", &category.ID, nil, nil)
	unmatched := makePurchase("Hardware", "Hammer", "2026-05-10T12:00:00Z", nil, nil, nil)
	matchedUUID, err := gofrsuuid.FromString(matched.ID)
	if err != nil {
		t.Fatal(err)
	}
	itemUUID, err := gofrsuuid.FromString(matched.Items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	tagUUID, err := gofrsuuid.FromString(tag.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := models.PurchaseItemTags.Insert(&models.PurchaseItemTagSetter{HouseholdID: &householdUUID, PurchaseID: &matchedUUID, ItemID: &itemUUID, TagID: &tagUUID}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}

	query := &queryResolver{Resolver: resolver}
	first, err := query.purchases(ctx, household.ID, nil, nil, nil, nil, new("appl"), nil, nil, nil, nil, nil, nil, nil, new(1), nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.TotalCount != 2 || len(first.Edges) != 1 || first.Edges[0].Node.ID != matchedLater.ID {
		t.Fatalf("search filter returned %#v; want the newest matching purchase", first)
	}
	if !first.PageInfo.HasNextPage || first.PageInfo.EndCursor == nil {
		t.Fatal("search filter must retain pagination after database filtering")
	}
	second, err := query.purchases(ctx, household.ID, nil, nil, nil, nil, new("appl"), nil, nil, nil, nil, nil, first.PageInfo.EndCursor, nil, new(1), nil)
	if err != nil || second.TotalCount != 2 || len(second.Edges) != 1 || second.Edges[0].Node.ID != matched.ID || second.PageInfo.HasNextPage {
		t.Fatalf("second filtered page returned %#v, error %v", second, err)
	}

	categoryResult, err := query.purchases(ctx, household.ID, nil, nil, nil, nil, nil, &category.ID, nil, nil, nil, nil, nil, nil, new(10), nil)
	if err != nil || categoryResult.TotalCount != 2 || len(categoryResult.Edges) != 2 {
		t.Fatalf("category filter returned %#v, error %v", categoryResult, err)
	}
	uncategorizedResult, err := query.purchases(ctx, household.ID, nil, nil, nil, nil, nil, nil, new(true), nil, nil, nil, nil, nil, new(10), nil)
	if err != nil || uncategorizedResult.TotalCount != 1 || uncategorizedResult.Edges[0].Node.ID != unmatched.ID {
		t.Fatalf("uncategorized filter returned %#v, error %v", uncategorizedResult, err)
	}
	tagResult, err := query.purchases(ctx, household.ID, nil, nil, nil, nil, nil, nil, nil, &tag.ID, nil, nil, nil, nil, new(10), nil)
	if err != nil || tagResult.TotalCount != 1 || tagResult.Edges[0].Node.ID != matched.ID {
		t.Fatalf("tag filter returned %#v, error %v", tagResult, err)
	}
	accountResult, err := query.purchases(ctx, household.ID, nil, nil, nil, nil, nil, nil, nil, nil, &account.ID, nil, nil, nil, new(10), nil)
	if err != nil || accountResult.TotalCount != 1 || accountResult.Edges[0].Node.ID != matched.ID {
		t.Fatalf("payment account filter returned %#v, error %v", accountResult, err)
	}
	dateResult, err := query.purchases(ctx, household.ID, new("2026-04-01T00:00:00Z"), new("2026-04-15T00:00:00Z"), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, new(10), nil)
	if err != nil || dateResult.TotalCount != 1 || dateResult.Edges[0].Node.ID != matched.ID {
		t.Fatalf("date filter returned %#v, error %v", dateResult, err)
	}
	poolResult, err := query.purchases(ctx, household.ID, nil, nil, nil, nil, nil, nil, nil, nil, nil, &pool.ID, nil, nil, new(10), nil)
	if err != nil || poolResult.TotalCount != 1 || len(poolResult.Edges) != 1 || poolResult.Edges[0].Node.ID != matched.ID {
		t.Fatalf("pool filter returned %#v, error %v", poolResult, err)
	}
	if _, err := query.purchases(ctx, household.ID, nil, nil, nil, nil, nil, &category.ID, new(true), nil, nil, nil, nil, nil, new(10), nil); !errors.Is(err, ErrBadInput) {
		t.Fatalf("simultaneous category and uncategorized filters returned %v; want bad input", err)
	}
}
