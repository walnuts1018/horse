package allocation

import (
	"os"
	"testing"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestSetDefaultAndRulePersistVersionsWeightsAndEvents(t *testing.T) {
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
	executor := bobpgx.NewPool(db)
	memberID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	householdID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	poolAID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	poolBID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	categoryID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	memberName, subject := "Allocation integration", "allocation-integration:"+memberID.String()
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberID, OidcSubject: &subject, DisplayName: &memberName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).Exec(t.Context(), executor)
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberID)))).Exec(t.Context(), executor)
	}()
	householdName := "allocation integration"
	if _, err := models.Households.Insert(&models.HouseholdSetter{ID: &householdID, Name: &householdName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdID, MemberID: &memberID}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	poolAName, poolBName, kind := "Allocation A", "Allocation B", "PERSONAL"
	if _, err := models.Pools.Insert(&models.PoolSetter{ID: &poolAID, HouseholdID: &householdID, Name: &poolAName, Kind: &kind}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	if _, err := models.Pools.Insert(&models.PoolSetter{ID: &poolBID, HouseholdID: &householdID, Name: &poolBName, Kind: &kind}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	categoryName := "Allocation category"
	if _, err := models.Categories.Insert(&models.CategorySetter{ID: &categoryID, HouseholdID: &householdID, Name: &categoryName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}

	service := NewService(db)
	if err := service.SetDefault(ctx, SetDefaultCommand{
		ActorID: memberID.String(), HouseholdID: householdID.String(),
		Weights: []Weight{{PoolID: poolAID.String(), Value: 3}, {PoolID: poolBID.String(), Value: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	household, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).One(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if household.DefaultAllocationVersion != 2 || household.Revision != 2 {
		t.Fatalf("default version/revision = %d/%d, want 2/2", household.DefaultAllocationVersion, household.Revision)
	}
	defaults, err := models.AllocationDefaults.Query(sm.Where(psql.And(
		models.AllocationDefaults.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.AllocationDefaults.Columns.Version.EQ(psql.Arg(int64(2))),
	))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if len(defaults) != 2 {
		t.Fatalf("default allocations = %d, want 2", len(defaults))
	}

	target := RuleTarget{CategoryID: new(categoryID.String())}
	command := SetRuleCommand{
		ActorID: memberID.String(), HouseholdID: householdID.String(), Target: target,
		Weights: []Weight{{PoolID: poolAID.String(), Value: 1}, {PoolID: poolBID.String(), Value: 2}},
	}
	ruleID, err := service.SetRule(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	command.Weights = []Weight{{PoolID: poolAID.String(), Value: 4}}
	updatedRuleID, err := service.SetRule(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	if updatedRuleID != ruleID {
		t.Fatalf("updated rule id = %s, want %s", updatedRuleID, ruleID)
	}
	ruleUUID, err := gofrsuuid.FromString(ruleID)
	if err != nil {
		t.Fatal(err)
	}
	rule, err := models.AllocationRules.Query(sm.Where(models.AllocationRules.Columns.ID.EQ(psql.Arg(ruleUUID)))).One(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if rule.Revision != 2 {
		t.Fatalf("allocation rule revision = %d, want 2", rule.Revision)
	}
	ruleWeights, err := models.AllocationRuleWeights.Query(sm.Where(models.AllocationRuleWeights.Columns.RuleID.EQ(psql.Arg(ruleUUID)))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if len(ruleWeights) != 1 || ruleWeights[0].PoolID != poolAID || ruleWeights[0].Weight != 4 {
		t.Fatalf("updated rule weights = %#v, want only pool A at weight 4", ruleWeights)
	}
	events, err := models.HouseholdEvents.Query(sm.Where(psql.And(
		models.HouseholdEvents.Columns.HouseholdID.EQ(psql.Arg(householdID)),
	)), sm.OrderBy(models.HouseholdEvents.Columns.Cursor)).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("allocation events = %d, want 3", len(events))
	}
	if events[0].Type != "allocation.default.updated" || events[0].Revision != 2 || events[0].EntityID != householdID {
		t.Fatalf("default allocation event = %#v", events[0])
	}
	if events[1].Type != "allocation.rule.updated" || events[1].Revision != 1 || events[2].Type != "allocation.rule.updated" || events[2].Revision != 2 {
		t.Fatalf("allocation rule events = %#v", events)
	}
}
