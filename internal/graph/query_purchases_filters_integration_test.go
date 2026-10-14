package graph

import (
	"context"
	"errors"
	"os"
	"slices"
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
	const (
		alignedItemName   = "Aligned item"
		expensiveItemName = "Expensive item"
	)
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
	otherPool, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "Shared", Kind: model.PoolKindShared})
	if err != nil {
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
	matchedLater := makePurchase("Market", "Apple Juice", "2026-04-20T12:00:00Z", &category.ID, nil, &otherPool.ID)
	unmatched := makePurchase("Hardware", "Hammer", "2026-05-10T12:00:00Z", nil, nil, &otherPool.ID)
	amountPurchase, err := mutation.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID,
		OccurredAt: model.DateTime("2026-05-11T12:00:00Z"), Merchant: "Amount market",
		Items: []*model.PurchaseItemInput{
			{Name: "Small item", AmountYen: 100, CategoryID: &category.ID, Allocations: []*model.AllocationInput{{PoolID: pool.ID, Weight: 1}}},
			{Name: "Large item", AmountYen: 200},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	alignedPurchase, err := mutation.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID,
		OccurredAt: model.DateTime("2026-05-09T12:00:00Z"), Merchant: "Wanted aligned market",
		Items: []*model.PurchaseItemInput{{Name: alignedItemName, AmountYen: 200, CategoryID: &category.ID, Allocations: []*model.AllocationInput{{PoolID: pool.ID, Weight: 1}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantedItemName := "Wanted item"
	disjointSearchPurchase, err := mutation.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID,
		OccurredAt: model.DateTime("2026-05-13T12:00:00Z"), Merchant: "Different market",
		Items: []*model.PurchaseItemInput{
			{Name: wantedItemName, AmountYen: 100},
			{Name: expensiveItemName, AmountYen: 200, CategoryID: &category.ID, Allocations: []*model.AllocationInput{{PoolID: pool.ID, Weight: 1}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	belowMinimum, err := mutation.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID,
		OccurredAt: model.DateTime("2026-05-14T12:00:00Z"), Merchant: "Below minimum",
		Items: []*model.PurchaseItemInput{{Name: "Small item", AmountYen: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
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
	for _, target := range []struct {
		purchase  *model.Purchase
		itemIndex int
	}{
		{purchase: amountPurchase, itemIndex: 0},
		{purchase: alignedPurchase, itemIndex: 0},
		{purchase: disjointSearchPurchase, itemIndex: 1},
	} {
		purchase := target.purchase
		purchaseUUID, err := gofrsuuid.FromString(purchase.ID)
		if err != nil {
			t.Fatal(err)
		}
		purchaseItemUUID, err := gofrsuuid.FromString(purchase.Items[target.itemIndex].ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := models.PurchaseItemTags.Insert(&models.PurchaseItemTagSetter{HouseholdID: &householdUUID, PurchaseID: &purchaseUUID, ItemID: &purchaseItemUUID, TagID: &tagUUID}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
	}

	query := &queryResolver{Resolver: resolver}
	purchaseIDs := func(connection *model.PurchaseConnection) []string {
		ids := make([]string, len(connection.Edges))
		for index, edge := range connection.Edges {
			ids[index] = edge.Node.ID
		}
		return ids
	}
	queryPurchases := func(filter model.PurchaseFilter, after *string, first *int) (*model.PurchaseConnection, error) {
		return query.purchases(ctx, filter, purchasePagination{after: after, first: first})
	}
	first, err := queryPurchases(model.PurchaseFilter{HouseholdID: household.ID, Search: new("appl")}, nil, new(1))
	if err != nil {
		t.Fatal(err)
	}
	if first.TotalCount != 2 || len(first.Edges) != 1 || first.Edges[0].Node.ID != matchedLater.ID {
		t.Fatalf("search filter returned %#v; want the newest matching purchase", first)
	}
	if !first.PageInfo.HasNextPage || first.PageInfo.EndCursor == nil {
		t.Fatal("search filter must retain pagination after database filtering")
	}
	second, err := queryPurchases(model.PurchaseFilter{HouseholdID: household.ID, Search: new("appl")}, first.PageInfo.EndCursor, new(1))
	if err != nil || second.TotalCount != 2 || len(second.Edges) != 1 || second.Edges[0].Node.ID != matched.ID || second.PageInfo.HasNextPage {
		t.Fatalf("second filtered page returned %#v, error %v", second, err)
	}

	categoryResult, err := queryPurchases(model.PurchaseFilter{HouseholdID: household.ID, CategoryID: &category.ID}, nil, new(10))
	if err != nil || categoryResult.TotalCount != 5 || !slices.Equal(purchaseIDs(categoryResult), []string{disjointSearchPurchase.ID, amountPurchase.ID, alignedPurchase.ID, matchedLater.ID, matched.ID}) {
		t.Fatalf("category filter returned %#v, error %v", categoryResult, err)
	}
	uncategorizedResult, err := queryPurchases(model.PurchaseFilter{HouseholdID: household.ID, Uncategorized: new(true)}, nil, new(10))
	if err != nil || uncategorizedResult.TotalCount != 4 || !slices.Equal(purchaseIDs(uncategorizedResult), []string{belowMinimum.ID, disjointSearchPurchase.ID, amountPurchase.ID, unmatched.ID}) {
		t.Fatalf("uncategorized filter returned %#v, error %v", uncategorizedResult, err)
	}
	tagResult, err := queryPurchases(model.PurchaseFilter{HouseholdID: household.ID, TagID: &tag.ID}, nil, new(10))
	if err != nil || tagResult.TotalCount != 4 || !slices.Equal(purchaseIDs(tagResult), []string{disjointSearchPurchase.ID, amountPurchase.ID, alignedPurchase.ID, matched.ID}) {
		t.Fatalf("tag filter returned %#v, error %v", tagResult, err)
	}
	accountResult, err := queryPurchases(model.PurchaseFilter{HouseholdID: household.ID, PaymentAccountID: &account.ID}, nil, new(10))
	if err != nil || accountResult.TotalCount != 1 || accountResult.Edges[0].Node.ID != matched.ID {
		t.Fatalf("payment account filter returned %#v, error %v", accountResult, err)
	}
	dateResult, err := queryPurchases(model.PurchaseFilter{HouseholdID: household.ID, From: new(model.DateTime("2026-04-01T00:00:00Z")), To: new(model.DateTime("2026-04-15T00:00:00Z"))}, nil, new(10))
	if err != nil || dateResult.TotalCount != 1 || dateResult.Edges[0].Node.ID != matched.ID {
		t.Fatalf("date filter returned %#v, error %v", dateResult, err)
	}
	poolResult, err := queryPurchases(model.PurchaseFilter{HouseholdID: household.ID, PoolID: &pool.ID}, nil, new(10))
	if err != nil || poolResult.TotalCount != 5 || !slices.Equal(purchaseIDs(poolResult), []string{belowMinimum.ID, disjointSearchPurchase.ID, amountPurchase.ID, alignedPurchase.ID, matched.ID}) {
		t.Fatalf("pool filter returned %#v, error %v", poolResult, err)
	}
	minimumAmount := model.Long(150)
	purchaseAmountTarget := model.PurchaseAmountTargetPurchase
	purchaseAmountResult, err := queryPurchases(model.PurchaseFilter{HouseholdID: household.ID, MinimumAmount: &minimumAmount, AmountTarget: &purchaseAmountTarget}, nil, new(1))
	if err != nil || purchaseAmountResult.TotalCount != 3 || len(purchaseAmountResult.Edges) != 1 || purchaseAmountResult.Edges[0].Node.ID != disjointSearchPurchase.ID || purchaseAmountResult.Edges[0].Node.TotalYen != 300 {
		t.Fatalf("purchase amount filter returned %#v, error %v", purchaseAmountResult, err)
	}
	itemAmountTarget := model.PurchaseAmountTargetItem
	itemAmountResult, err := queryPurchases(model.PurchaseFilter{HouseholdID: household.ID, MinimumAmount: &minimumAmount, AmountTarget: &itemAmountTarget}, nil, new(1))
	if err != nil || itemAmountResult.TotalCount != 3 || len(itemAmountResult.Edges) != 1 || itemAmountResult.Edges[0].Node.ID != disjointSearchPurchase.ID || len(itemAmountResult.Edges[0].Node.Items) != 1 || itemAmountResult.Edges[0].Node.Items[0].Name != expensiveItemName {
		t.Fatalf("item amount filter returned %#v, error %v", itemAmountResult, err)
	}
	itemAttributeResult, err := queryPurchases(model.PurchaseFilter{HouseholdID: household.ID, CategoryID: &category.ID, TagID: &tag.ID, PoolID: &pool.ID, MinimumAmount: &minimumAmount, AmountTarget: &itemAmountTarget}, nil, new(10))
	if err != nil || itemAttributeResult.TotalCount != 2 || len(itemAttributeResult.Edges) != 2 || itemAttributeResult.Edges[0].Node.ID != disjointSearchPurchase.ID || itemAttributeResult.Edges[1].Node.ID != alignedPurchase.ID || len(itemAttributeResult.Edges[0].Node.Items) != 1 || itemAttributeResult.Edges[0].Node.Items[0].Name != expensiveItemName || len(itemAttributeResult.Edges[1].Node.Items) != 1 || itemAttributeResult.Edges[1].Node.Items[0].Name != alignedItemName {
		t.Fatalf("combined item amount and attributes returned %#v, error %v", itemAttributeResult, err)
	}
	searchTerm := "Wanted"
	itemSearchResult, err := queryPurchases(model.PurchaseFilter{HouseholdID: household.ID, Search: &searchTerm, CategoryID: &category.ID, TagID: &tag.ID, PoolID: &pool.ID, MinimumAmount: &minimumAmount, AmountTarget: &itemAmountTarget}, nil, new(10))
	if err != nil || itemSearchResult.TotalCount != 1 || len(itemSearchResult.Edges) != 1 || itemSearchResult.Edges[0].Node.ID != alignedPurchase.ID || len(itemSearchResult.Edges[0].Node.Items) != 1 || itemSearchResult.Edges[0].Node.Items[0].Name != alignedItemName {
		t.Fatalf("combined item search, amount, and attributes returned %#v, error %v", itemSearchResult, err)
	}
	trimmedItemSearchResult, err := query.Purchases(ctx, model.PurchaseFilter{
		HouseholdID: household.ID, Search: new(" Wanted "), CategoryID: &category.ID,
		TagID: &tag.ID, PoolID: &pool.ID, MinimumAmount: &minimumAmount, AmountTarget: &itemAmountTarget,
	}, nil, nil, new(10), nil)
	if err != nil || trimmedItemSearchResult.TotalCount != 1 || len(trimmedItemSearchResult.Edges) != 1 || trimmedItemSearchResult.Edges[0].Node.ID != alignedPurchase.ID || len(trimmedItemSearchResult.Edges[0].Node.Items) != 1 || trimmedItemSearchResult.Edges[0].Node.Items[0].Name != alignedItemName {
		t.Fatalf("trimmed item search returned %#v, error %v", trimmedItemSearchResult, err)
	}
	if _, err := queryPurchases(model.PurchaseFilter{HouseholdID: household.ID, CategoryID: &category.ID, Uncategorized: new(true)}, nil, new(10)); !errors.Is(err, ErrBadInput) {
		t.Fatalf("simultaneous category and uncategorized filters returned %v; want bad input", err)
	}
}
