package graph

import (
	"context"
	"errors"
	"os"
	"testing"

	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestBulkUpdatePurchasesMergesDisjointFieldsAndRollsBackConflicts(t *testing.T) {
	databaseURL := os.Getenv("HORSE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("HORSE_TEST_DATABASE_URL is not set")
	}
	ctx := t.Context()
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	memberID := uuid.NewV7().String()
	memberUUID := generatedUUID(memberID)
	subject, name := "bulk-update-purchase:"+memberID, "Bulk update integration"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &name}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(context.Background(), bobpgx.NewPool(db))
	}()
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: name})
	resolver := &mutationResolver{Resolver: &Resolver{DB: db}}
	household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "Bulk update integration"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(generatedUUID(household.ID))))).Exec(context.Background(), bobpgx.NewPool(db))
	}()
	createPurchase := func(merchant string) *model.Purchase {
		t.Helper()
		purchase, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{
			IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID,
			OccurredAt: "2026-05-10T12:00:00Z", Merchant: merchant,
			Items: []*model.PurchaseItemInput{{Name: "Test item", AmountYen: 100}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return purchase
	}
	first, second := createPurchase("First"), createPurchase("Second")
	changedMerchant := "Updated first"
	first, err = resolver.updatePurchase(ctx, model.UpdatePurchaseInput{
		PurchaseID: first.ID, BaseRevision: first.Revision,
		Patch: &model.PurchasePatch{Merchant: &changedMerchant}, ChangedFields: []string{"merchant"},
	})
	if err != nil {
		t.Fatal(err)
	}
	newDate := "2026-05-11T12:00:00Z"
	updated, err := resolver.bulkUpdatePurchases(ctx, model.BulkUpdatePurchasesInput{
		Targets: []*model.PurchaseUpdateTargetInput{
			{PurchaseID: first.ID, BaseRevision: 1, Patch: &model.BulkPurchasePatch{OccurredAt: &newDate}},
			{PurchaseID: second.ID, BaseRevision: 1, Patch: &model.BulkPurchasePatch{OccurredAt: &newDate}},
		},
		ChangedFields: []string{"occurredAt"},
	})
	if err != nil || len(updated) != 2 {
		t.Fatalf("bulk update should merge stale disjoint fields: purchases=%#v error=%v", updated, err)
	}
	if updated[0].Merchant != changedMerchant {
		t.Fatalf("stale bulk update replaced a disjoint merchant change: got %q", updated[0].Merchant)
	}
	first = updated[0]
	second = updated[1]
	secondDate := "2026-05-12T12:00:00Z"
	second, err = resolver.updatePurchase(ctx, model.UpdatePurchaseInput{
		PurchaseID: second.ID, BaseRevision: second.Revision,
		Patch: &model.PurchasePatch{OccurredAt: &secondDate}, ChangedFields: []string{"occurredAt"},
	})
	if err != nil {
		t.Fatal(err)
	}
	atomicMerchant := "Must roll back"
	conflictingDate := "2026-05-13T12:00:00Z"
	_, err = resolver.bulkUpdatePurchases(ctx, model.BulkUpdatePurchasesInput{
		Targets: []*model.PurchaseUpdateTargetInput{
			{PurchaseID: first.ID, BaseRevision: first.Revision, Patch: &model.BulkPurchasePatch{Merchant: &atomicMerchant, OccurredAt: &newDate}},
			{PurchaseID: second.ID, BaseRevision: second.Revision, Patch: &model.BulkPurchasePatch{Merchant: &atomicMerchant, OccurredAt: &conflictingDate}},
		},
		ChangedFields: []string{"merchant", "occurredAt"},
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("bulk update should report the overlapping stale field, got %v", err)
	}
	persistedFirst, err := loadPurchase(ctx, db, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persistedFirst.Merchant != changedMerchant {
		t.Fatalf("conflicting bulk update partially changed the first purchase: merchant=%q", persistedFirst.Merchant)
	}
}
