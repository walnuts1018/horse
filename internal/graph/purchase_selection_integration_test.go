package graph

import (
	"context"
	"os"
	"testing"

	"uuid"

	"github.com/99designs/gqlgen/graphql"
	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestLoadPurchasesRespectsPaymentAllocationSelection(t *testing.T) {
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
	executor := bobpgx.NewPool(db)

	memberID, err := gofrsuuid.FromString(uuid.NewV7().String())
	if err != nil {
		t.Fatal(err)
	}
	subject := "purchase-selection-integration:" + memberID.String()
	displayName := "purchase selection integration"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberID, OidcSubject: &subject, DisplayName: &displayName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberID)))).Exec(context.WithoutCancel(t.Context()), executor)
	})
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID.String(), Subject: subject, DisplayName: displayName})
	mutation := &mutationResolver{Resolver: &Resolver{DB: db}}
	household, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "purchase selection integration"})
	if err != nil {
		t.Fatal(err)
	}
	householdID, err := gofrsuuid.FromString(household.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).Exec(context.WithoutCancel(t.Context()), executor)
	})
	pool, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "Personal", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.setDefaultAllocation(ctx, model.SetDefaultAllocationInput{HouseholdID: household.ID, Allocations: []*model.AllocationInput{{PoolID: pool.ID, Weight: 1}}}); err != nil {
		t.Fatal(err)
	}
	account, err := mutation.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Cash", Kind: model.AccountKindCash, PoolID: &pool.ID})
	if err != nil {
		t.Fatal(err)
	}
	purchase, err := mutation.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID,
		OccurredAt: model.DateTime("2026-05-10T12:00:00Z"), Merchant: "Market",
		Items:              []*model.PurchaseItemInput{{Name: "Bread", AmountYen: 100}},
		PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: account.ID, AmountYen: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	purchaseID, err := gofrsuuid.FromString(purchase.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseID)))).One(ctx, queryExecutor(db))
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name                string
		selection           string
		wantPaymentAccounts int
	}{
		{name: "without payment allocations", selection: "query { purchases { edges { node { id } } } }", wantPaymentAccounts: 0},
		{name: "with payment allocations", selection: "query { purchases { edges { node { paymentAllocations { amountYen } } } } }", wantPaymentAccounts: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			purchaseCtx := purchaseSelectionContext(t, ctx, test.selection)
			loaded, err := loadPurchases(purchaseCtx, db, models.PurchaseSlice{stored})
			if err != nil {
				t.Fatal(err)
			}
			if got := len(stored.R.PurchasePayments); got != test.wantPaymentAccounts {
				t.Fatalf("loaded payment rows = %d, want %d", got, test.wantPaymentAccounts)
			}
			if got := len(loaded[0].PaymentAllocations); got != test.wantPaymentAccounts {
				t.Fatalf("payment allocations = %d, want %d", got, test.wantPaymentAccounts)
			}
		})
	}
}

func TestPurchaseSelectionControlsEvidenceLoading(t *testing.T) {
	ctx := t.Context()
	for _, test := range []struct {
		name            string
		selection       string
		wantEvidence    bool
		wantSourceKinds bool
	}{
		{name: "no evidence fields", selection: "query { purchases { edges { node { id } } } }"},
		{name: "has evidence", selection: "query { purchases { edges { node { hasEvidence } } } }", wantEvidence: true},
		{name: "source kinds", selection: "query { purchases { edges { node { sourceKinds } } } }", wantEvidence: true, wantSourceKinds: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			purchaseCtx := purchaseSelectionContext(t, ctx, test.selection)
			if got := shouldLoadPurchaseEvidence(purchaseCtx); got != test.wantEvidence {
				t.Fatalf("shouldLoadPurchaseEvidence() = %t, want %t", got, test.wantEvidence)
			}
			if got := shouldLoadPurchaseSourceKinds(purchaseCtx); got != test.wantSourceKinds {
				t.Fatalf("shouldLoadPurchaseSourceKinds() = %t, want %t", got, test.wantSourceKinds)
			}
		})
	}
}

func TestPurchaseSelectionControlsItemRelationshipLoading(t *testing.T) {
	ctx := t.Context()
	for _, test := range []struct {
		name            string
		selection       string
		wantAllocations bool
		wantItemTags    bool
	}{
		{name: "item scalar fields only", selection: "query { purchases { edges { node { items { id } } } } }"},
		{name: "allocations", selection: "query { purchases { edges { node { items { allocations { amountYen } } } } } }", wantAllocations: true},
		{name: "tags", selection: "query { purchases { edges { node { items { tags { id } } } } } }", wantItemTags: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			purchaseCtx := purchaseSelectionContext(t, ctx, test.selection)
			if got := shouldLoadPurchaseItemAllocations(purchaseCtx); got != test.wantAllocations {
				t.Fatalf("shouldLoadPurchaseItemAllocations() = %t, want %t", got, test.wantAllocations)
			}
			if got := shouldLoadPurchaseItemTags(purchaseCtx); got != test.wantItemTags {
				t.Fatalf("shouldLoadPurchaseItemTags() = %t, want %t", got, test.wantItemTags)
			}
		})
	}
}

func purchaseSelectionContext(t *testing.T, ctx context.Context, query string) context.Context {
	t.Helper()
	doc, err := parser.ParseQuery(&ast.Source{Input: query})
	if err != nil {
		t.Fatal(err)
	}
	field := doc.Operations[0].SelectionSet[0].(*ast.Field)
	ctx = graphql.WithOperationContext(ctx, &graphql.OperationContext{Doc: doc, Variables: map[string]any{}})
	return graphql.WithFieldContext(ctx, &graphql.FieldContext{Field: graphql.CollectedField{Field: field, Selections: field.SelectionSet}})
}
