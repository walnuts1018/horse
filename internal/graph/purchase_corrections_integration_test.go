package graph

import (
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

func TestPurchaseCorrectionConnectionPagesDatabaseRowsAndRefundableAmounts(t *testing.T) {
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
	defer db.Close()
	memberID := uuid.NewV7().String()
	memberUUID := generatedUUID(memberID)
	subject, name := "correction-pages:"+memberID, "Correction pages"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &name}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: name})
	resolver := &mutationResolver{Resolver: &Resolver{DB: db}}
	household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "Correction pages"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(generatedUUID(household.ID))))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	pool, err := resolver.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "Refund pool", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	account, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Refund card", Kind: model.AccountKindCard, PoolID: &pool.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{HouseholdID: household.ID, PoolID: pool.ID, Configuration: personalPoolFundingConfiguration(memberID), ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.setDefaultAllocation(ctx, model.SetDefaultAllocationInput{HouseholdID: household.ID, Allocations: []*model.AllocationInput{{PoolID: pool.ID, Weight: 1}}}); err != nil {
		t.Fatal(err)
	}
	purchase, err := resolver.createAndPostPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: "2026-10-01T12:00:00Z", Merchant: "Correction shop",
		PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: account.ID, AmountYen: 1000}},
		Items:              []*model.PurchaseItemInput{{Name: "Refund item", AmountYen: 1000, Allocations: []*model.AllocationInput{{PoolID: pool.ID, Weight: 1}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	corrections := []model.CorrectPurchaseInput{
		{HouseholdID: household.ID, Target: &model.CorrectPurchaseTargetInput{Item: &model.PurchaseCorrectionItemTargetInput{PurchaseID: purchase.ID, ItemID: purchase.Items[0].ID}}, AmountDeltaYen: -100, Reason: "Item refund", IdempotencyKey: uuid.NewV7().String()},
		{HouseholdID: household.ID, Target: &model.CorrectPurchaseTargetInput{Purchase: &model.PurchaseCorrectionPurchaseTargetInput{PurchaseID: purchase.ID}}, AmountDeltaYen: -200, Reason: "Purchase refund", IdempotencyKey: uuid.NewV7().String()},
		{HouseholdID: household.ID, Target: &model.CorrectPurchaseTargetInput{Purchase: &model.PurchaseCorrectionPurchaseTargetInput{PurchaseID: purchase.ID}}, AmountDeltaYen: -50, Reason: "Additional refund", IdempotencyKey: uuid.NewV7().String()},
	}
	for _, input := range corrections {
		if _, err := resolver.correctPurchase(ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	pageSize := 2
	query := &queryResolver{Resolver: resolver.Resolver}
	purchaseNodeID, err := encodeNodeID(nodeTypePurchase, purchase.ID)
	if err != nil {
		t.Fatal(err)
	}
	firstPage, err := query.purchaseCorrections(ctx, purchaseNodeID, &pageSize, nil)
	if err != nil || len(firstPage.Edges) != pageSize || !firstPage.PageInfo.HasNextPage || firstPage.TotalCount != len(corrections) {
		t.Fatalf("first correction page is wrong: connection=%#v error=%v", firstPage, err)
	}
	secondPage, err := query.purchaseCorrections(ctx, purchaseNodeID, &pageSize, firstPage.PageInfo.EndCursor)
	if err != nil || len(secondPage.Edges) != 1 || secondPage.PageInfo.HasNextPage || secondPage.TotalCount != len(corrections) {
		t.Fatalf("second correction page is wrong: connection=%#v error=%v", secondPage, err)
	}
	legacyCursor := encodeCursor(nodeTypePurchaseCorrection, firstPage.Edges[len(firstPage.Edges)-1].Node.GetID())
	legacyPage, err := query.purchaseCorrections(ctx, purchaseNodeID, &pageSize, &legacyCursor)
	if err != nil || len(legacyPage.Edges) != 1 || legacyPage.Edges[0].Node.GetID() != secondPage.Edges[0].Node.GetID() {
		t.Fatalf("legacy correction cursor is wrong: connection=%#v error=%v", legacyPage, err)
	}
	loaded, err := loadPurchase(ctx, db, purchase.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CorrectionCount != len(corrections) || int64(loaded.CorrectionAmountYen) != -350 || int64(loaded.RemainingRefundableAmountYen) != 650 {
		t.Fatalf("purchase correction aggregate is wrong: count=%d amount=%d remaining=%d", loaded.CorrectionCount, loaded.CorrectionAmountYen, loaded.RemainingRefundableAmountYen)
	}
	if len(loaded.Items) != 1 || int64(loaded.Items[0].RemainingRefundableAmountYen) != 900 {
		t.Fatalf("item refundable amount is wrong: items=%#v", loaded.Items)
	}
	if firstPage.Edges[0].Cursor == secondPage.Edges[0].Cursor || firstPage.Edges[1].Cursor == secondPage.Edges[0].Cursor {
		t.Fatal("correction cursor repeated a row across pages")
	}
}
