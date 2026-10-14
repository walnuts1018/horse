package graph

import (
	"context"
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

func TestHouseholdLoaderLoadsOnlyRequestedFieldsAcrossHouseholds(t *testing.T) {
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
	subject, memberName := "household-loader:"+memberID, "Household loader integration"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &memberName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: memberName})
	mutation := &mutationResolver{Resolver: &Resolver{DB: db}}
	first, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "Household loader first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "Household loader second"})
	if err != nil {
		t.Fatal(err)
	}
	for _, household := range []*model.Household{first, second} {
		householdUUID := generatedUUID(household.ID)
		t.Cleanup(func() {
			_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
		})
		if _, err := mutation.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: household.Name + " account", Kind: model.AccountKindBank}); err != nil {
			t.Fatal(err)
		}
	}
	ids := []gofrsuuid.UUID{generatedUUID(first.ID), generatedUUID(second.ID)}
	selected, err := loadHouseholds(ctx, db, ids, householdFields{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		household := selected[id]
		if household == nil || household.Accounts != nil || household.Members != nil || household.PoolVariants != nil || household.Categories != nil || household.DefaultAllocation != nil {
			t.Fatalf("unrequested household fields were loaded: %#v", household)
		}
	}

	selected, err = loadHouseholds(ctx, db, ids, householdFields{members: true, accounts: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		household := selected[id]
		if len(household.Members) != 1 || household.Members[0].ID != memberID {
			t.Fatalf("household members = %#v, want the household member", household.Members)
		}
		if len(household.Accounts) != 1 || household.Accounts[0].Name != household.Name+" account" {
			t.Fatalf("household accounts = %#v, want its account", household.Accounts)
		}
		if household.PoolVariants != nil || household.Categories != nil || household.DefaultAllocation != nil {
			t.Fatalf("unrequested household fields were loaded: %#v", household)
		}
	}
}
