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
	personalPool, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: first.ID, Name: "configured personal", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{HouseholdID: first.ID, PoolID: personalPool.ID, Configuration: personalPoolFundingConfiguration(memberID), ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	sharedPool, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: first.ID, Name: "configured shared", Kind: model.PoolKindShared})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{
		HouseholdID: first.ID, PoolID: sharedPool.ID,
		Configuration:   sharedPoolFundingConfiguration([]*model.PoolFundingShareInput{{MemberID: memberID, Weight: 100}}, model.SharedPoolSettlementPolicyBalanceToStandard),
		ExpectedVersion: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: first.ID, Name: "unconfigured personal", Kind: model.PoolKindPersonal}); err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: first.ID, Name: "unconfigured shared", Kind: model.PoolKindShared}); err != nil {
		t.Fatal(err)
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
	selected, err = loadHouseholds(ctx, db, ids, householdFields{pools: true})
	if err != nil {
		t.Fatal(err)
	}
	pools := selected[generatedUUID(first.ID)].PoolVariants
	if len(pools) != 4 {
		t.Fatalf("household pool count = %d, want 4", len(pools))
	}
	if pool, ok := pools[0].(*model.PersonalPool); !ok || pool.ID != personalPool.ID || pool.OwnerMemberID == nil || *pool.OwnerMemberID != memberID || pool.FundingRuleVersion != 2 {
		t.Fatalf("pools[0] = %#v, want configured personal pool", pools[0])
	}
	if pool, ok := pools[1].(*model.SharedPool); !ok || pool.ID != sharedPool.ID || pool.SettlementPolicy == nil || *pool.SettlementPolicy != model.SharedPoolSettlementPolicyBalanceToStandard || pool.FundingRuleVersion != 2 || len(pool.FundingShares) != 1 || pool.FundingShares[0].MemberID != memberID || pool.FundingShares[0].MemberName != memberName || pool.FundingShares[0].Weight != 100 {
		t.Fatalf("pools[1] = %#v, want configured shared pool", pools[1])
	}
	if _, ok := pools[2].(*model.UnconfiguredPersonalPool); !ok {
		t.Fatalf("pools[2] = %#v, want unconfigured personal pool", pools[2])
	}
	if _, ok := pools[3].(*model.UnconfiguredSharedPool); !ok {
		t.Fatalf("pools[3] = %#v, want unconfigured shared pool", pools[3])
	}
}
