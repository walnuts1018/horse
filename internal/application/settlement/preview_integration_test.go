package settlement

import (
	"os"
	"slices"
	"testing"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestSettlementPreviewMatchesMonthlyCalculationsAfterClosedCarry(t *testing.T) {
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
	poolID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	scheduleID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	memberName, subject := "Preview member", "settlement-preview:"+memberID.String()
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberID, OidcSubject: &subject, DisplayName: &memberName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).Exec(t.Context(), executor)
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberID)))).Exec(t.Context(), executor)
	}()
	householdName := "settlement preview integration"
	if _, err := models.Households.Insert(&models.HouseholdSetter{ID: &householdID, Name: &householdName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdID, MemberID: &memberID}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	poolName, poolKind := "Preview pool", "PERSONAL"
	if _, err := models.Pools.Insert(&models.PoolSetter{ID: &poolID, HouseholdID: &householdID, Name: &poolName, Kind: &poolKind}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	amount, cadence, active := int64(100), "MONTHLY", true
	startsOn := time.Date(2025, time.December, 1, 0, 0, 0, 0, time.FixedZone("JST", 9*60*60))
	if _, err := models.FundingSchedules.Insert(&models.FundingScheduleSetter{
		ID: &scheduleID, HouseholdID: &householdID, MemberID: &memberID, PoolID: &poolID,
		AmountYen: &amount, Cadence: &cadence, StartsOn: &startsOn, Active: &active,
	}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	workflow := New(db)
	closed, err := workflow.Close(ctx, CloseCommand{ActorID: memberID.String(), HouseholdID: householdID.String(), Period: "2025-12"})
	if err != nil {
		t.Fatal(err)
	}
	if len(closed.Lines) != 1 || closed.Lines[0].AmountDueYen != amount {
		t.Fatalf("unexpected closed settlement: %#v", closed.Lines)
	}

	periods := []string{"2026-01", "2026-02", "2026-03"}
	carry := closed.Lines[0].AmountDueYen
	var expected []domain.SettlementAmount
	for _, period := range periods[:len(periods)-1] {
		lines, err := CalculatePeriod(ctx, executor, householdID.String(), period)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range lines {
			carry += line.AmountDueYen
		}
	}
	lastMonth, err := CalculatePeriod(ctx, executor, householdID.String(), periods[len(periods)-1])
	if err != nil {
		t.Fatal(err)
	}
	for index := range lastMonth {
		lastMonth[index].CarryYen = carry
		lastMonth[index].AmountDueYen += carry
	}
	expected = lastMonth
	actual, err := workflow.Preview(ctx, householdID.String(), periods[len(periods)-1])
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(actual, expected) {
		t.Fatalf("multi-month preview differs from monthly calculations: got %#v, want %#v", actual, expected)
	}
	if actual[0].CarryYen != 300 || actual[0].AmountDueYen != 400 {
		t.Fatalf("closed balance was not carried once through open months: %#v", actual[0])
	}
}
