package graph

import (
	"context"
	"os"
	"sync/atomic"
	"testing"

	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestPurchaseLoaderQueryCountDoesNotScaleWithPurchaseCount(t *testing.T) {
	databaseURL := os.Getenv("HORSE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("HORSE_TEST_DATABASE_URL is not set")
	}
	ctx := t.Context()
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	queryCounter := &purchaseLoaderQueryCounter{}
	config.ConnConfig.Tracer = queryCounter
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	executor := bobpgx.NewPool(db)

	memberID := generatedUUID(uuid.NewV7().String())
	subject := "purchase-loader-query-count:" + memberID.String()
	displayName := "purchase loader query count"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberID, OidcSubject: &subject, DisplayName: &displayName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberID)))).Exec(context.WithoutCancel(t.Context()), executor)
	})
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID.String(), Subject: subject, DisplayName: displayName})
	mutation := &mutationResolver{Resolver: &Resolver{DB: db}}
	household, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "purchase loader query count"})
	if err != nil {
		t.Fatal(err)
	}
	householdID := generatedUUID(household.ID)
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
	tag, err := mutation.createTag(ctx, model.CreateTagInput{HouseholdID: household.ID, Name: "loader query count"})
	if err != nil {
		t.Fatal(err)
	}
	tagID := generatedUUID(tag.ID)
	for range 5 {
		documentID := generatedUUID(uuid.NewV7().String())
		digest, objectKey := documentID.String(), "query-count/"+documentID.String()
		mimeType, sourceKind := "image/jpeg", "receipt"
		if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{
			ID: &documentID, HouseholdID: &householdID, Sha256: &digest, ObjectKey: &objectKey,
			MimeType: &mimeType, SourceKind: &sourceKind, CreatedBy: &memberID,
		}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
		sourceDocumentID := documentID.String()
		purchase, err := mutation.createPurchase(ctx, model.CreatePurchaseInput{
			IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID,
			OccurredAt: model.DateTime("2026-10-10T12:00:00Z"), Merchant: "Query count market", SourceDocumentID: &sourceDocumentID,
			Items: []*model.PurchaseItemInput{{Name: "Bread", AmountYen: 100}},
		})
		if err != nil {
			t.Fatal(err)
		}
		purchaseID := generatedUUID(purchase.ID)
		item, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseID)))).One(ctx, queryExecutor(db))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := models.PurchaseItemTags.Insert(&models.PurchaseItemTagSetter{
			HouseholdID: &householdID, PurchaseID: &purchaseID, ItemID: &item.ID, TagID: &tagID,
		}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
	}

	selection := "query { purchases { edges { node { id hasEvidence sourceKinds items { id allocations { poolId poolName amountYen } tags { id name } } } } } }"
	queryResolver := &queryResolver{Resolver: &Resolver{DB: db}}
	measure := func(first int) (int64, *model.PurchaseConnection) {
		t.Helper()
		queryCounter.count.Store(0)
		purchaseCtx := purchaseSelectionContext(t, ctx, selection)
		connection, err := queryResolver.purchases(purchaseCtx, model.PurchaseFilter{HouseholdID: household.ID}, purchasePagination{first: &first})
		if err != nil {
			t.Fatal(err)
		}
		return queryCounter.count.Load(), connection
	}

	singleQueryCount, singlePurchase := measure(1)
	multipleQueryCount, multiplePurchases := measure(5)
	t.Logf("purchase query count for one and five parents: %d and %d", singleQueryCount, multipleQueryCount)
	if singleQueryCount != 15 {
		t.Fatalf("purchase query count for one parent = %d, want 15", singleQueryCount)
	}
	if multipleQueryCount != singleQueryCount {
		t.Fatalf("purchase query count scaled with parent count: one=%d five=%d", singleQueryCount, multipleQueryCount)
	}
	if len(singlePurchase.Edges) != 1 || len(multiplePurchases.Edges) != 5 {
		t.Fatalf("purchase page sizes = %d and %d, want 1 and 5", len(singlePurchase.Edges), len(multiplePurchases.Edges))
	}
	for _, edge := range multiplePurchases.Edges {
		if !edge.Node.HasEvidence || len(edge.Node.SourceKinds) != 1 || edge.Node.SourceKinds[0] != model.SourceKindReceipt {
			t.Fatalf("evidence fields were not loaded: hasEvidence=%t sourceKinds=%v", edge.Node.HasEvidence, edge.Node.SourceKinds)
		}
		if len(edge.Node.Items) != 1 || len(edge.Node.Items[0].Allocations) != 1 || len(edge.Node.Items[0].Tags) != 1 {
			t.Fatalf("nested item relations were not loaded: items=%d", len(edge.Node.Items))
		}
	}
}

type purchaseLoaderQueryCounter struct {
	count atomic.Int64
}

func (counter *purchaseLoaderQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	counter.count.Add(1)
	return ctx
}

func (*purchaseLoaderQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
