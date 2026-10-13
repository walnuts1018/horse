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

func TestCreatePurchaseAndPostPurchaseCanShareIdempotencyKey(t *testing.T) {
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
	subject := "idempotency-integration:" + memberID
	displayName := "idempotency integration"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &displayName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(context.Background(), bobpgx.NewPool(db))
	})

	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: displayName})
	resolver := &mutationResolver{Resolver: (&Resolver{DB: db})}
	household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "idempotency integration"})
	if err != nil {
		t.Fatal(err)
	}
	householdUUID, err := gofrsuuid.FromString(household.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(context.Background(), bobpgx.NewPool(db))
	})
	pool, err := resolver.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "Personal", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{HouseholdID: household.ID, PoolID: pool.ID, Configuration: personalPoolFundingConfiguration(memberID), ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.setDefaultAllocation(ctx, model.SetDefaultAllocationInput{HouseholdID: household.ID, Allocations: []*model.AllocationInput{{PoolID: pool.ID, Weight: 1}}}); err != nil {
		t.Fatal(err)
	}
	account, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Card", Kind: model.AccountKindCard, PoolID: &pool.ID})
	if err != nil {
		t.Fatal(err)
	}

	key := uuid.NewV7().String()
	purchase, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey:     key,
		HouseholdID:        household.ID,
		OccurredAt:         "2026-10-13T10:00:00+09:00",
		Merchant:           "Idempotency test",
		PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: account.ID, AmountYen: 100}},
		Items:              []*model.PurchaseItemInput{{Name: "test item", AmountYen: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolver.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: key,
		HouseholdID:    household.ID,
		OccurredAt:     "2026-10-13T10:00:00+09:00",
		Merchant:       "Idempotency test",
		Items:          []*model.PurchaseItemInput{{Name: "test item", AmountYen: 101}},
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("createPurchase with a reused key and different payload should conflict: %v", err)
	}

	posted, err := resolver.postPurchase(ctx, purchase.ID, purchase.Revision, key)
	if err != nil {
		t.Fatalf("postPurchase with the createPurchase key failed: %v", err)
	}
	if posted.ID != purchase.ID || posted.Status != model.PurchaseStatusPosted {
		t.Fatalf("postPurchase did not post the created purchase: created=%#v posted=%#v", purchase, posted)
	}
}
