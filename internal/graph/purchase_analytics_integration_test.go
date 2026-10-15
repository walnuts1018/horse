package graph

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"uuid"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestPurchaseAnalyticsAggregatesCorrectionsAndReturnsTopItemObservations(t *testing.T) {
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
	subject, name := "purchase-analytics:"+memberID, "Purchase Analytics"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &name}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(t.Context(), bobpgx.NewPool(db))
	})
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: name})
	mutation := &mutationResolver{Resolver: &Resolver{DB: db}}
	household, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "Purchase analytics"})
	if err != nil {
		t.Fatal(err)
	}
	householdUUID, err := gofrsuuid.FromString(household.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(t.Context(), bobpgx.NewPool(db))
	})
	pool, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "Analytics pool", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{HouseholdID: household.ID, PoolID: pool.ID, Configuration: personalPoolFundingConfiguration(memberID), ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.setDefaultAllocation(ctx, model.SetDefaultAllocationInput{HouseholdID: household.ID, Allocations: []*model.AllocationInput{{PoolID: pool.ID, Weight: 1}}}); err != nil {
		t.Fatal(err)
	}
	account, err := mutation.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Analytics account", Kind: model.AccountKindCard, PoolID: &pool.ID})
	if err != nil {
		t.Fatal(err)
	}
	secondAccount, err := mutation.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Analytics second account", Kind: model.AccountKindCard, PoolID: &pool.ID})
	if err != nil {
		t.Fatal(err)
	}
	purchase, err := mutation.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: "2026-04-15T10:00:00+09:00", Merchant: "Market",
		PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: account.ID, AmountYen: 60}, {PaymentAccountID: secondAccount.ID, AmountYen: 40}},
		Items:              []*model.PurchaseItemInput{{Name: "牛乳", AmountYen: 100, Quantity: &model.PurchaseQuantityInput{Numerator: "1", Denominator: "1", Unit: "個"}, PrintedUnitPrice: &model.PrintedUnitPriceInput{AmountYen: 100, Basis: &model.PurchaseQuantityInput{Numerator: "1", Denominator: "1", Unit: "個"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.postPurchase(ctx, purchase.ID, purchase.Revision, uuid.NewV7().String()); err != nil {
		t.Fatal(err)
	}
	for _, target := range []*model.CorrectPurchaseTargetInput{
		{Item: &model.PurchaseCorrectionItemTargetInput{PurchaseID: purchase.ID, ItemID: purchase.Items[0].ID}},
		{Purchase: &model.PurchaseCorrectionPurchaseTargetInput{PurchaseID: purchase.ID}},
	} {
		amount := model.Long(-10)
		if target.Purchase != nil {
			amount = -20
		}
		if _, err := mutation.correctPurchase(ctx, model.CorrectPurchaseInput{HouseholdID: household.ID, Target: target, AmountDeltaYen: amount, Reason: "refund", IdempotencyKey: uuid.NewV7().String()}); err != nil {
			t.Fatal(err)
		}
	}
	legacyPurchase, err := createPostedAnalyticsPurchase(ctx, mutation, household.ID, "2026-05-01T00:00:00+09:00", "May market", account.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := models.PurchasePayments.Delete(dm.Where(models.PurchasePayments.Columns.PurchaseID.EQ(psql.Arg(legacyPurchase.ID)))).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	legacyAccountUUID, err := gofrsuuid.FromString(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	legacyPaymentAccountID := sql.Null[gofrsuuid.UUID]{V: legacyAccountUUID, Valid: true}
	_, err = models.Purchases.Update((&models.PurchaseSetter{PaymentAccountID: &legacyPaymentAccountID}).UpdateMod(), um.Where(models.Purchases.Columns.ID.EQ(psql.Arg(legacyPurchase.ID)))).One(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	otherHousehold, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "Other purchase analytics"})
	if err != nil {
		t.Fatal(err)
	}
	otherHouseholdUUID, err := gofrsuuid.FromString(otherHousehold.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(otherHouseholdUUID)))).Exec(t.Context(), bobpgx.NewPool(db))
	})
	otherPool, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: otherHousehold.ID, Name: "Other analytics pool", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{HouseholdID: otherHousehold.ID, PoolID: otherPool.ID, Configuration: personalPoolFundingConfiguration(memberID), ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.setDefaultAllocation(ctx, model.SetDefaultAllocationInput{HouseholdID: otherHousehold.ID, Allocations: []*model.AllocationInput{{PoolID: otherPool.ID, Weight: 1}}}); err != nil {
		t.Fatal(err)
	}
	otherAccount, err := mutation.createAccount(ctx, model.CreateAccountInput{HouseholdID: otherHousehold.ID, Name: "Other analytics account", Kind: model.AccountKindCard, PoolID: &otherPool.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createPostedAnalyticsPurchase(ctx, mutation, otherHousehold.ID, "2026-04-15T10:00:00+09:00", "Other household market", otherAccount.ID, 250); err != nil {
		t.Fatal(err)
	}

	entries, err := (&queryResolver{Resolver: mutation.Resolver}).purchaseAnalytics(ctx, household.ID, "2026-04", "2026-04")
	if err != nil {
		t.Fatal(err)
	}
	poolNodeID, err := encodeAnalyticsID("pool", pool.ID)
	if err != nil {
		t.Fatal(err)
	}
	accountNodeID, err := encodeAnalyticsID("account", account.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondAccountNodeID, err := encodeAnalyticsID("account", secondAccount.ID)
	if err != nil {
		t.Fatal(err)
	}
	amountByID := make(map[string]int64)
	countByID := make(map[string]int)
	for _, entry := range entries {
		if entry.Period != "2026-04" {
			t.Fatalf("period = %q, want 2026-04", entry.Period)
		}
		if entry.Dimension == "merchant" && entry.ID == "Market" || entry.Dimension == "item" && entry.ID == "牛乳\x00個" || entry.ID == "__correction__" || entry.Dimension == "pool" && entry.ID == poolNodeID || entry.Dimension == "account" && (entry.ID == accountNodeID || entry.ID == secondAccountNodeID) {
			key := entry.Dimension + ":" + entry.ID
			amountByID[key] += int64(entry.AmountYen)
			countByID[key] += entry.Count
		}
	}
	for key, want := range map[string]int64{"merchant:Market": 70, "item:牛乳\x00個": 90, "item:__correction__": -20, "pool:" + poolNodeID: 100, "pool:__correction__": -30, "account:" + accountNodeID: 60, "account:" + secondAccountNodeID: 40, "account:__correction__": -30} {
		if amountByID[key] != want {
			t.Errorf("%s total = %d, want %d", key, amountByID[key], want)
		}
	}
	for _, entry := range entries {
		if entry.Name == "May market" || entry.Name == "Other household market" {
			t.Fatalf("analytics included out-of-range or another household purchase: %#v", entry)
		}
	}
	if countByID["merchant:Market"] != 1 || countByID["item:牛乳\x00個"] != 1 {
		t.Fatalf("merchant/item counts = %d/%d, want 1/1", countByID["merchant:Market"], countByID["item:牛乳\x00個"])
	}

	observations, err := (&queryResolver{Resolver: mutation.Resolver}).purchasePriceObservations(ctx, household.ID, "2026-04", "2026-04", []string{"牛乳\x00個"})
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 || observations[0].AmountYen != 100 || observations[0].RemainingRefundableAmountYen != 90 || observations[0].PrintedUnitPriceYen == nil || int64(*observations[0].PrintedUnitPriceYen) != 100 || observations[0].EffectiveUnitPriceNumeratorYen == nil || *observations[0].EffectiveUnitPriceNumeratorYen != "100" {
		t.Fatalf("price observations = %#v, want one corrected item observation with both stored and refundable amounts", observations)
	}
	mayEntries, err := (&queryResolver{Resolver: mutation.Resolver}).purchaseAnalytics(ctx, household.ID, "2026-05", "2026-05")
	if err != nil {
		t.Fatal(err)
	}
	if len(mayEntries) == 0 {
		t.Fatal("analytics did not include May purchase when May was requested")
	}
	for _, entry := range mayEntries {
		if entry.Dimension == "account" && entry.ID == accountNodeID && entry.AmountYen == 50 && entry.Count == 1 {
			return
		}
	}
	t.Fatal("legacy PaymentAccountID did not contribute to account analytics")
}

func createPostedAnalyticsPurchase(ctx context.Context, mutation *mutationResolver, householdID, occurredAt, merchant, accountID string, amount model.Long) (*model.Purchase, error) {
	purchase, err := mutation.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: householdID, OccurredAt: model.DateTime(occurredAt), Merchant: merchant,
		PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: accountID, AmountYen: amount}},
		Items:              []*model.PurchaseItemInput{{Name: merchant + " item", AmountYen: amount}},
	})
	if err != nil {
		return nil, err
	}
	posted, err := mutation.postPurchase(ctx, purchase.ID, purchase.Revision, uuid.NewV7().String())
	if err != nil {
		return nil, err
	}
	return posted, nil
}
