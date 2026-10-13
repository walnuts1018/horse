package graph

import (
	"os"
	"testing"
	"time"

	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestHomeSummaryUsesPurchaseDaysAndPostedSpending(t *testing.T) {
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
	memberUUID := generatedUUID(memberID)
	subject, memberName := "home-summary:"+memberID, "Home summary integration"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &memberName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()

	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: memberName})
	resolver := &mutationResolver{Resolver: &Resolver{DB: db}}
	household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "Home summary integration"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(generatedUUID(household.ID))))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	pool, err := resolver.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "Home summary", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{HouseholdID: household.ID, PoolID: pool.ID, Configuration: personalPoolFundingConfiguration(memberID), ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.setDefaultAllocation(ctx, model.SetDefaultAllocationInput{HouseholdID: household.ID, Allocations: []*model.AllocationInput{{PoolID: pool.ID, Weight: 1}}}); err != nil {
		t.Fatal(err)
	}
	account, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Home summary card", Kind: model.AccountKindCard, PoolID: &pool.ID})
	if err != nil {
		t.Fatal(err)
	}

	location, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(location)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	yesterdayPurchase, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: model.DateTime(today.Add(-time.Microsecond).Format(time.RFC3339Nano)), Merchant: "Yesterday",
		PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: account.ID, AmountYen: 100}},
		Items:              []*model.PurchaseItemInput{{Name: "Yesterday item", AmountYen: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	yesterdayPurchase, err = resolver.postPurchase(ctx, yesterdayPurchase.ID, yesterdayPurchase.Revision, uuid.NewV7().String())
	if err != nil {
		t.Fatal(err)
	}
	todayPurchase, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: model.DateTime(today.Format(time.RFC3339)), Merchant: "Today",
		PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: account.ID, AmountYen: 200}},
		Items:              []*model.PurchaseItemInput{{Name: "Today item", AmountYen: 200}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.postPurchase(ctx, todayPurchase.ID, todayPurchase.Revision, uuid.NewV7().String()); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: model.DateTime(today.Add(time.Hour).Format(time.RFC3339)), Merchant: "Draft",
		PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: account.ID, AmountYen: 500}},
		Items:              []*model.PurchaseItemInput{{Name: "Draft item", AmountYen: 500}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.correctPurchase(ctx, model.CorrectPurchaseInput{
		HouseholdID: household.ID, Target: &model.CorrectPurchaseTargetInput{Item: &model.PurchaseCorrectionItemTargetInput{PurchaseID: yesterdayPurchase.ID, ItemID: yesterdayPurchase.Items[0].ID}},
		AmountDeltaYen: -30, Reason: testCorrectionRefundReason, IdempotencyKey: uuid.NewV7().String(),
	}); err != nil {
		t.Fatal(err)
	}

	summary, err := (&queryResolver{Resolver: &Resolver{DB: db}}).homeSummary(ctx, household.ID)
	if err != nil {
		t.Fatal(err)
	}
	amountByDate := make(map[string]int64, len(summary.DailySpending))
	for _, entry := range summary.DailySpending {
		amountByDate[string(entry.Date)] = int64(entry.AmountYen)
	}
	if len(summary.DailySpending) != 7 || amountByDate[today.AddDate(0, 0, -1).Format(time.DateOnly)] != 70 || amountByDate[today.Format(time.DateOnly)] != 200 {
		t.Fatalf("daily spending did not use Japan-time purchase dates, posted purchases, and purchase-day refunds: %#v", summary.DailySpending)
	}
}
