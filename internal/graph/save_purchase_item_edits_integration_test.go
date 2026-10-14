package graph

import (
	"errors"
	"os"
	"testing"
	"time"

	"uuid"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	purchaseapp "github.com/walnuts1018/horse/internal/application/purchase"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestSavePurchaseItemEditsAppliesDistinctPatchesAtomically(t *testing.T) {
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
	if err := db.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	memberID := uuid.NewV7().String()
	memberUUID, err := gofrsuuid.FromString(memberID)
	if err != nil {
		t.Fatal(err)
	}
	subject := "save-item-edits:" + memberID
	memberName := "Purchase Editor"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &memberName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: memberName})
	resolver := &mutationResolver{Resolver: &Resolver{DB: db}}
	household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "Item edit integration"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(generatedUUID(household.ID))))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	poolA, err := resolver.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "Editor A", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	poolB, err := resolver.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "Editor B", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	for _, pool := range []*model.Pool{poolA, poolB} {
		if _, err := resolver.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{HouseholdID: household.ID, PoolID: pool.ID, Configuration: personalPoolFundingConfiguration(memberID), ExpectedVersion: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := resolver.setDefaultAllocation(ctx, model.SetDefaultAllocationInput{HouseholdID: household.ID, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 1}}}); err != nil {
		t.Fatal(err)
	}
	category, err := resolver.createCategory(ctx, model.CreateCategoryInput{HouseholdID: household.ID, Name: "Edited category"})
	if err != nil {
		t.Fatal(err)
	}
	account, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Editor card", Kind: model.AccountKindCard, PoolID: &poolA.ID})
	if err != nil {
		t.Fatal(err)
	}
	purchase, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: "2026-10-01T12:00:00Z", Merchant: "Editor Market",
		PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: account.ID, AmountYen: 1000}},
		Items:              []*model.PurchaseItemInput{{Name: "Milk", AmountYen: 600}, {Name: "Bread", AmountYen: 400}},
	})
	if err != nil {
		t.Fatal(err)
	}
	firstItem, secondItem := purchase.Items[0], purchase.Items[1]
	if _, err := resolver.postPurchase(ctx, purchase.ID, purchase.Revision, uuid.NewV7().String()); err != nil {
		t.Fatal(err)
	}

	updatedName := "Whole milk"
	updatedAmount := int64(700)
	updatedCategoryID := category.ID
	updatedMerchant := "Updated Market"
	updatedOccurredAt := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	updatedOccurredAtValue := model.DateTime(updatedOccurredAt.Format(time.RFC3339Nano))
	updatedPaymentAmount := model.Long(1100)
	updatedPayments := []*model.PurchasePaymentInput{{PaymentAccountID: account.ID, AmountYen: updatedPaymentAmount}}
	postedPurchase, err := loadPurchase(ctx, db, purchase.ID)
	if err != nil {
		t.Fatal(err)
	}
	updatedAmountLong := model.Long(updatedAmount)
	mutationResult, err := resolver.SavePurchaseItemEdits(ctx, model.SavePurchaseItemEditsInput{
		PurchaseID: purchase.ID, PurchaseBaseRevision: int(postedPurchase.Revision),
		PurchasePatch: omittable[*model.PurchasePatch](&model.PurchasePatch{
			Merchant: omittable(&updatedMerchant), OccurredAt: omittable(&updatedOccurredAtValue),
			PaymentAllocations: omittable(updatedPayments),
		}),
		Items: []*model.UpdatePurchaseItemInput{
			{ItemID: firstItem.ID, BaseRevision: int(firstItem.Revision), Name: omittable(&updatedName), AmountYen: omittable(&updatedAmountLong)},
			{ItemID: secondItem.ID, BaseRevision: int(secondItem.Revision), CategoryID: omittable(&updatedCategoryID), Allocations: omittable([]*model.AllocationInput{{PoolID: poolB.ID, Weight: 1}})},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, ok := mutationResult.(*model.SavePurchaseItemEditsSuccess)
	if !ok {
		t.Fatalf("save mutation must succeed: %#v", mutationResult)
	}
	if len(result.Items) != 2 {
		t.Fatalf("updated result must include both items: %#v", result.Items)
	}
	updatedPurchase, err := loadPurchase(ctx, db, purchase.ID)
	if err != nil {
		t.Fatal(err)
	}
	updatedItems := make(map[string]*model.PurchaseItem, len(updatedPurchase.Items))
	for _, item := range updatedPurchase.Items {
		updatedItems[item.ID] = item
	}
	updatedFirst, updatedSecond := updatedItems[firstItem.ID], updatedItems[secondItem.ID]
	if updatedFirst == nil || updatedFirst.Name != updatedName || updatedFirst.AmountYen != model.Long(updatedAmount) || updatedFirst.Revision != firstItem.Revision+1 {
		t.Fatalf("first item patch was not applied: %#v", updatedFirst)
	}
	if updatedSecond == nil || updatedSecond.CategoryID == nil || *updatedSecond.CategoryID != category.ID || updatedSecond.Revision != secondItem.Revision+1 {
		t.Fatalf("second item patch was not applied: %#v", updatedSecond)
	}
	if amountForPool(updatedSecond.Allocations, poolA.ID) != 0 || amountForPool(updatedSecond.Allocations, poolB.ID) != 400 {
		t.Fatalf("second item allocation was not recalculated: %#v", updatedSecond.Allocations)
	}
	if updatedPurchase.Status != model.PurchaseStatusPosted || updatedPurchase.Merchant != updatedMerchant || updatedPurchase.OccurredAt != model.DateTime(updatedOccurredAt.Format(time.RFC3339Nano)) {
		t.Fatalf("purchase header patch was not applied: %#v", updatedPurchase)
	}
	if len(updatedPurchase.PaymentAllocations) != 1 || updatedPurchase.PaymentAllocations[0].AmountYen != 1100 {
		t.Fatalf("posted purchase payment snapshot was not preserved: %#v", updatedPurchase)
	}

	rollbackName := "Must roll back"
	invalidAmount := model.Long(-1)
	_, err = resolver.savePurchaseItemEdits(ctx, model.SavePurchaseItemEditsInput{
		PurchaseID: purchase.ID,
		Items: []*model.UpdatePurchaseItemInput{
			{ItemID: updatedFirst.ID, BaseRevision: int(updatedFirst.Revision), Name: omittable(&rollbackName)},
			{ItemID: updatedSecond.ID, BaseRevision: int(updatedSecond.Revision), AmountYen: omittable(&invalidAmount)},
		},
	})
	if !errors.Is(err, purchaseapp.ErrBadInput) {
		t.Fatalf("invalid item patch must fail the whole save: %v", err)
	}
	rolledBackPurchase, err := loadPurchase(ctx, db, purchase.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range rolledBackPurchase.Items {
		if item.ID == updatedFirst.ID && (item.Name != updatedName || item.Revision != updatedFirst.Revision) {
			t.Fatalf("earlier item patch escaped transaction rollback: %#v", item)
		}
		if item.ID == updatedSecond.ID && (item.AmountYen != 400 || item.Revision != updatedSecond.Revision) {
			t.Fatalf("invalid item patch changed stored data: %#v", item)
		}
	}

	headerRollbackName := "Header update must roll back"
	invalidPaymentAmount := model.Long(1)
	_, err = resolver.savePurchaseItemEdits(ctx, model.SavePurchaseItemEditsInput{
		PurchaseID: purchase.ID, PurchaseBaseRevision: int(rolledBackPurchase.Revision),
		PurchasePatch: omittable[*model.PurchasePatch](&model.PurchasePatch{
			PaymentAllocations: omittable([]*model.PurchasePaymentInput{{PaymentAccountID: account.ID, AmountYen: invalidPaymentAmount}}),
		}),
		Items: []*model.UpdatePurchaseItemInput{{ItemID: updatedFirst.ID, BaseRevision: int(updatedFirst.Revision), Name: omittable(&headerRollbackName)}},
	})
	if !errors.Is(err, purchaseapp.ErrBadInput) {
		t.Fatalf("invalid header patch must fail the whole save: %v", err)
	}
	rolledBackHeader, err := loadPurchase(ctx, db, purchase.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range rolledBackHeader.Items {
		if item.ID == updatedFirst.ID && (item.Name != updatedName || item.Revision != updatedFirst.Revision) {
			t.Fatalf("item patch escaped header validation rollback: %#v", item)
		}
	}
}
