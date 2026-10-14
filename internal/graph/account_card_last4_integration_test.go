package graph

import (
	"context"
	"database/sql"
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

func TestAccountCardLast4LifecycleAndValidation(t *testing.T) {
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

	memberID := uuid.NewV7().String()
	memberUUID := generatedUUID(memberID)
	subject, displayName := "account-card-last4:"+memberID, "Card last four digits"
	if _, err := models.Members.Insert(&models.MemberSetter{
		ID: &memberUUID, OidcSubject: &subject, DisplayName: &displayName,
	}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: displayName})
	resolver := &mutationResolver{Resolver: &Resolver{DB: db}}
	household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "Card last four digits"})
	if err != nil {
		t.Fatal(err)
	}
	householdUUID := generatedUUID(household.ID)
	t.Cleanup(func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})

	initialLast4 := "0042"
	card, err := resolver.createAccount(ctx, model.CreateAccountInput{
		HouseholdID: household.ID, Name: "Travel card", Kind: model.AccountKindCard, CardLast4: &initialLast4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if card.CardLast4 == nil || *card.CardLast4 != initialLast4 {
		t.Fatalf("created card last4 = %v, want %q", card.CardLast4, initialLast4)
	}

	updatedLast4 := "9876"
	result, err := resolver.SetAccountCardLast4(ctx, model.SetAccountCardLast4Input{
		AccountID: card.ID, CardLast4: &updatedLast4,
	})
	if err != nil {
		t.Fatal(err)
	}
	success, ok := result.(*model.SetAccountCardLast4Success)
	if !ok || success.Result.CardLast4 == nil || *success.Result.CardLast4 != updatedLast4 {
		t.Fatalf("updated result = %#v, want card last4 %q", result, updatedLast4)
	}

	loadedHousehold, err := loadHousehold(ctx, db, household.ID, householdFields{accounts: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := loadedHousehold.Accounts[0].CardLast4; got == nil || *got != updatedLast4 {
		t.Fatalf("loaded household card last4 = %v, want %q", got, updatedLast4)
	}
	result, err = resolver.SetAccountCardLast4(ctx, model.SetAccountCardLast4Input{AccountID: card.ID})
	if err != nil {
		t.Fatal(err)
	}
	if success, ok := result.(*model.SetAccountCardLast4Success); !ok || success.Result.CardLast4 != nil {
		t.Fatalf("cleared result = %#v, want card last4 unset", result)
	}

	invalidLast4 := "12x4"
	result, err = resolver.SetAccountCardLast4(ctx, model.SetAccountCardLast4Input{
		AccountID: card.ID, CardLast4: &invalidLast4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if userError, ok := result.(*model.SetAccountCardLast4UserError); !ok || userError.Code != model.MutationErrorCodeBadInput {
		t.Fatalf("invalid last4 result = %#v, want BAD_INPUT", result)
	}

	bank, err := resolver.createAccount(ctx, model.CreateAccountInput{
		HouseholdID: household.ID, Name: "Operating", Kind: model.AccountKindBank,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err = resolver.SetAccountCardLast4(ctx, model.SetAccountCardLast4Input{
		AccountID: bank.ID, CardLast4: &updatedLast4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if userError, ok := result.(*model.SetAccountCardLast4UserError); !ok || userError.Code != model.MutationErrorCodeBadInput {
		t.Fatalf("non-card last4 result = %#v, want BAD_INPUT", result)
	}
	resultCreate, err := resolver.CreateAccount(ctx, model.CreateAccountInput{
		HouseholdID: household.ID, Name: "Invalid bank", Kind: model.AccountKindBank, CardLast4: &updatedLast4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if userError, ok := resultCreate.(*model.CreateAccountUserError); !ok || userError.Code != model.MutationErrorCodeBadInput {
		t.Fatalf("non-card create result = %#v, want BAD_INPUT", resultCreate)
	}

	for _, accountKind := range []string{"CARD", "BANK"} {
		id := generatedUUID(uuid.NewV7().String())
		name := "Invalid " + accountKind
		invalid := sql.Null[string]{V: invalidLast4, Valid: true}
		if _, err := models.Accounts.Insert(&models.AccountSetter{
			ID: &id, HouseholdID: &householdUUID, Name: &name, Kind: &accountKind, CardLast4: &invalid,
		}).Exec(ctx, bobpgx.NewPool(db)); err == nil {
			t.Fatalf("database accepted card last4 %q for %s account", invalidLast4, accountKind)
		}
	}
}
