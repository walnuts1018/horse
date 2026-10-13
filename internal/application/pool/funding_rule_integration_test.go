package pool

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/apperror"
	householdapp "github.com/walnuts1018/horse/internal/application/household"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestSetFundingRuleValidatesAndVersionsHouseholdRule(t *testing.T) {
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

	actorID := createFundingRuleTestMember(t, db, "actor")
	memberID := createFundingRuleTestMember(t, db, "member")
	outsiderID := createFundingRuleTestMember(t, db, "outsider")
	t.Cleanup(func() {
		executor := bobpgx.NewPool(db)
		for _, id := range []gofrsuuid.UUID{actorID, memberID, outsiderID} {
			_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(id)))).Exec(t.Context(), executor)
		}
	})
	household, err := householdapp.NewService(db).Create(ctx, householdapp.CreateCommand{ActorID: actorID.String(), Name: "funding rule"})
	if err != nil {
		t.Fatal(err)
	}
	householdID := fundingRuleUUID(t, household.ID)
	memberRole := "MEMBER"
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdID, MemberID: &memberID, Role: &memberRole}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).Exec(t.Context(), bobpgx.NewPool(db))
	})

	service := NewService(db)
	personalPool, err := service.Create(ctx, CreateCommand{ActorID: actorID.String(), HouseholdID: household.ID, Name: "personal", Kind: "PERSONAL"})
	if err != nil {
		t.Fatal(err)
	}
	sharedPool, err := service.Create(ctx, CreateCommand{ActorID: actorID.String(), HouseholdID: household.ID, Name: "shared", Kind: "SHARED"})
	if err != nil {
		t.Fatal(err)
	}
	assertBadInput := func(command SetFundingRuleCommand) {
		t.Helper()
		err := service.SetFundingRule(ctx, command)
		if kind, ok := apperror.KindOf(err); !ok || kind != apperror.KindBadInput {
			t.Fatalf("SetFundingRule error = %v; want bad input", err)
		}
	}
	assertBadInput(SetFundingRuleCommand{
		ActorID: actorID.String(), HouseholdID: household.ID, PoolID: personalPool.ID,
		ExpectedVersion: 1, Personal: true, PersonalOwnerID: outsiderID.String(),
	})
	assertBadInput(SetFundingRuleCommand{
		ActorID: actorID.String(), HouseholdID: household.ID, PoolID: personalPool.ID,
		ExpectedVersion: 1, Shared: true, FundingShares: []FundingShare{{MemberID: actorID.String(), Weight: 1}}, SettlementPolicy: "BALANCE_TO_STANDARD",
	})
	assertBadInput(SetFundingRuleCommand{
		ActorID: actorID.String(), HouseholdID: household.ID, PoolID: sharedPool.ID,
		ExpectedVersion: 1, Shared: true, FundingShares: []FundingShare{{MemberID: outsiderID.String(), Weight: 1}}, SettlementPolicy: "BALANCE_TO_STANDARD",
	})
	assertBadInput(SetFundingRuleCommand{
		ActorID: actorID.String(), HouseholdID: household.ID, PoolID: sharedPool.ID,
		ExpectedVersion: 1, Personal: true, Shared: true,
	})

	if err := service.SetFundingRule(ctx, SetFundingRuleCommand{
		ActorID: actorID.String(), HouseholdID: household.ID, PoolID: personalPool.ID,
		ExpectedVersion: 1, Personal: true, PersonalOwnerID: actorID.String(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.SetFundingRule(ctx, SetFundingRuleCommand{
		ActorID: actorID.String(), HouseholdID: household.ID, PoolID: sharedPool.ID,
		ExpectedVersion: 1, Shared: true, SettlementPolicy: "REIMBURSE_PERSONAL",
		FundingShares: []FundingShare{{MemberID: actorID.String(), Weight: 3}, {MemberID: memberID.String(), Weight: 2}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.SetFundingRule(ctx, SetFundingRuleCommand{
		ActorID: actorID.String(), HouseholdID: household.ID, PoolID: personalPool.ID,
		ExpectedVersion: 1, Personal: true, PersonalOwnerID: actorID.String(),
	}); !errors.Is(err, apperror.ErrConflict) {
		t.Fatalf("stale funding rule version error = %v; want conflict", err)
	}

	executor := bobpgx.NewPool(db)
	poolID := fundingRuleUUID(t, personalPool.ID)
	pool, err := models.Pools.Query(sm.Where(models.Pools.Columns.ID.EQ(psql.Arg(poolID)))).One(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if pool.FundingRuleVersion != 2 {
		t.Fatalf("personal funding rule version = %d; want 2", pool.FundingRuleVersion)
	}
	rule, err := models.PoolFundingRules.Query(sm.Where(psql.And(
		models.PoolFundingRules.Columns.PoolID.EQ(psql.Arg(poolID)),
		models.PoolFundingRules.Columns.Version.EQ(psql.Arg(int32(2))),
	))).One(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if !rule.OwnerMemberID.Valid || rule.OwnerMemberID.V != actorID || rule.CreatedBy != actorID {
		t.Fatalf("personal funding rule owner/creator = %#v/%s; want %s/%s", rule.OwnerMemberID, rule.CreatedBy, actorID, actorID)
	}
	sharedPoolID := fundingRuleUUID(t, sharedPool.ID)
	shares, err := models.PoolFundingShares.Query(sm.Where(psql.And(
		models.PoolFundingShares.Columns.PoolID.EQ(psql.Arg(sharedPoolID)),
		models.PoolFundingShares.Columns.Version.EQ(psql.Arg(int32(2))),
	))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	shareWeights := make(map[gofrsuuid.UUID]int64, len(shares))
	for _, share := range shares {
		shareWeights[share.MemberID] = share.Weight
	}
	if len(shareWeights) != 2 || shareWeights[actorID] != 3 || shareWeights[memberID] != 2 {
		t.Fatalf("shared funding shares = %#v; want actor weight 3 and member weight 2", shareWeights)
	}
	householdRow, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).One(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if householdRow.Revision != 5 {
		t.Fatalf("household revision = %d; want 5 after two pool creates and two rule updates", householdRow.Revision)
	}
	events, err := models.HouseholdEvents.Query(sm.Where(psql.And(
		models.HouseholdEvents.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdEvents.Columns.Type.EQ(psql.Arg("pool.funding_rule.updated")),
	))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("funding rule event count = %d; want 2", len(events))
	}
	for _, event := range events {
		if !event.ActorID.Valid || event.ActorID.V != actorID || event.Revision != 2 {
			t.Fatalf("funding rule event actor/revision = %#v/%d; want %s/2", event.ActorID, event.Revision, actorID)
		}
		var fields []string
		if err := json.Unmarshal(event.ChangedFields.Val, &fields); err != nil {
			t.Fatal(err)
		}
		if len(fields) != 3 || fields[0] != "ownerMemberId" || fields[1] != "fundingShares" || fields[2] != "settlementPolicy" {
			t.Fatalf("funding rule event fields = %v", fields)
		}
	}
}

func createFundingRuleTestMember(t *testing.T, db *pgxpool.Pool, name string) gofrsuuid.UUID {
	t.Helper()
	id, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	subject := "funding-rule-integration:" + id.String()
	displayName := name
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &id, OidcSubject: &subject, DisplayName: &displayName}).Exec(t.Context(), bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	return id
}

func fundingRuleUUID(t *testing.T, value string) gofrsuuid.UUID {
	t.Helper()
	id, err := gofrsuuid.FromString(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
