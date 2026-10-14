package graph

import (
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

func TestPurchaseDefaultsUsePostedHistoryWithinHousehold(t *testing.T) {
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

	memberID := uuid.NewV7().String()
	memberUUID := generatedUUID(memberID)
	subject, name := "purchase-defaults:"+memberID, "Purchase defaults integration"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &name}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(t.Context(), bobpgx.NewPool(db))
	})
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: name})
	resolver := &mutationResolver{Resolver: &Resolver{DB: db}}
	type fixture struct {
		householdID string
		accountID   string
	}
	fixtures := make([]fixture, 2)
	for i := range fixtures {
		household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "Purchase defaults " + string(rune('A'+i))})
		if err != nil {
			t.Fatal(err)
		}
		householdUUID := generatedUUID(household.ID)
		t.Cleanup(func() {
			_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(t.Context(), bobpgx.NewPool(db))
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
		fixtures[i] = fixture{householdID: household.ID, accountID: account.ID}
	}
	for i, occurrence := range []string{"2026-10-12T00:00:00Z", "2026-10-13T00:00:00Z"} {
		purchase, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{
			IdempotencyKey: uuid.NewV7().String(), HouseholdID: fixtures[0].householdID, OccurredAt: model.DateTime(occurrence), Merchant: "Shared merchant",
			PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: fixtures[0].accountID, AmountYen: 100}},
			Items:              []*model.PurchaseItemInput{{Name: "Item", AmountYen: 100}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := resolver.postPurchase(ctx, purchase.ID, purchase.Revision, uuid.NewV7().String()); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			continue
		}
		foreignPurchase, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{
			IdempotencyKey: uuid.NewV7().String(), HouseholdID: fixtures[1].householdID, OccurredAt: model.DateTime(occurrence), Merchant: "Shared merchant",
			PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: fixtures[1].accountID, AmountYen: 100}},
			Items:              []*model.PurchaseItemInput{{Name: "Item", AmountYen: 100}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := resolver.postPurchase(ctx, foreignPurchase.ID, foreignPurchase.Revision, uuid.NewV7().String()); err != nil {
			t.Fatal(err)
		}
	}
	defaults, err := (&queryResolver{Resolver: &Resolver{DB: db}}).PurchaseDefaults(ctx, fixtures[0].householdID)
	if err != nil {
		t.Fatal(err)
	}
	if len(defaults) != 1 || defaults[0].Merchant != "Shared merchant" || defaults[0].PaymentAccountID != fixtures[0].accountID || defaults[0].PurchaseCount != 2 {
		t.Fatalf("purchase defaults = %#v, want only household account with two posted purchases", defaults)
	}
	if defaults[0].LastUsedAt != "2026-10-13T00:00:00Z" {
		t.Fatalf("last used at = %q, want newest posted purchase", defaults[0].LastUsedAt)
	}
	outsiderID := uuid.NewV7().String()
	outsiderUUID := generatedUUID(outsiderID)
	outsiderSubject, outsiderName := "purchase-defaults-outsider:"+outsiderID, "Outsider"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &outsiderUUID, OidcSubject: &outsiderSubject, DisplayName: &outsiderName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(outsiderUUID)))).Exec(t.Context(), bobpgx.NewPool(db))
	})
	outsiderCtx := auth.WithPrincipal(ctx, auth.Principal{MemberID: outsiderID, Subject: outsiderSubject, DisplayName: outsiderName})
	if _, err := (&queryResolver{Resolver: &Resolver{DB: db}}).PurchaseDefaults(outsiderCtx, fixtures[0].householdID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign member query error = %v, want not found", err)
	}
}
