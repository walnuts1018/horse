package settlement

import (
	"crypto/sha256"
	"database/sql"
	"os"
	"testing"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/funding"
	"github.com/walnuts1018/horse/internal/application/funding_schedule"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestIrregularFundingOverpaymentCarriesAcrossClosedPeriod(t *testing.T) {
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
	memberA, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	memberB, err := gofrsuuid.NewV7()
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
	for _, member := range []struct {
		id   gofrsuuid.UUID
		name string
	}{{memberA, "Funding member A"}, {memberB, "Funding member B"}} {
		subject := "funding-scenario:" + member.id.String()
		if _, err := models.Members.Insert(&models.MemberSetter{ID: &member.id, OidcSubject: &subject, DisplayName: &member.name}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).Exec(t.Context(), executor)
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.In(psql.Arg(memberA), psql.Arg(memberB)))).Exec(t.Context(), executor)
	}()
	householdName := "irregular funding integration"
	if _, err := models.Households.Insert(&models.HouseholdSetter{ID: &householdID, Name: &householdName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	for _, memberID := range []gofrsuuid.UUID{memberA, memberB} {
		if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdID, MemberID: &memberID}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
	}
	poolName, poolKind := "Shared funding pool", "SHARED"
	if _, err := models.Pools.Insert(&models.PoolSetter{ID: &poolID, HouseholdID: &householdID, Name: &poolName, Kind: &poolKind}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	startsOn := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.FixedZone("JST", 9*60*60))
	cadence, active := "MONTHLY", true
	for _, schedule := range []struct {
		memberID gofrsuuid.UUID
		amount   int64
	}{{memberA, 6000}, {memberB, 4000}} {
		scheduleID, err := gofrsuuid.NewV7()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := models.FundingSchedules.Insert(&models.FundingScheduleSetter{
			ID: &scheduleID, HouseholdID: &householdID, MemberID: &schedule.memberID, PoolID: &poolID,
			AmountYen: &schedule.amount, Cadence: &cadence, StartsOn: &startsOn, Active: &active,
		}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
	}
	workflow := funding.New(db)
	command := funding.Command{
		ActorID: memberA.String(), HouseholdID: householdID.String(), MemberID: memberA.String(), PoolID: poolID.String(),
		AmountYen: 10000, OccurredAt: time.Date(2026, time.June, 15, 12, 0, 0, 0, time.UTC),
		IdempotencyKey:     "irregular-funding-" + householdID.String(),
		RequestFingerprint: sha256.Sum256([]byte("member-a-funded-10000")),
	}
	if _, err := workflow.Do(ctx, command); err != nil {
		t.Fatal(err)
	}
	settlements := New(db)
	open, err := settlements.Get(ctx, householdID.String(), "2026-06")
	if err != nil {
		t.Fatal(err)
	}
	lines := make(map[string]struct {
		scheduled int64
		paid      int64
		due       int64
	}, len(open.Lines))
	for _, line := range open.Lines {
		lines[line.MemberID] = struct {
			scheduled int64
			paid      int64
			due       int64
		}{line.ScheduledYen, line.PaidYen, line.AmountDueYen}
	}
	if lines[memberA.String()].scheduled != 6000 || lines[memberA.String()].paid != 10000 || lines[memberA.String()].due != -4000 {
		t.Fatalf("overpayment should remain a 4000-yen credit: %#v", lines[memberA.String()])
	}
	if lines[memberB.String()].scheduled != 4000 || lines[memberB.String()].paid != 0 || lines[memberB.String()].due != 4000 {
		t.Fatalf("unpaid irregular contribution should remain due: %#v", lines[memberB.String()])
	}
	var scheduledEvidence, paidEvidence bool
	for _, evidence := range open.Evidence {
		if evidence.MemberID != memberA.String() {
			continue
		}
		if evidence.Kind == "SCHEDULED" && evidence.AmountYen == 6000 {
			scheduledEvidence = true
		}
		if evidence.Kind == "PAID" && evidence.AmountYen == 10000 {
			paidEvidence = true
		}
	}
	if !scheduledEvidence || !paidEvidence {
		t.Fatalf("settlement evidence omitted the scheduled or actual funding source: %#v", open.Evidence)
	}
	closed, err := settlements.Close(ctx, CloseCommand{ActorID: memberA.String(), HouseholdID: householdID.String(), Period: "2026-06"})
	if err != nil {
		t.Fatal(err)
	}
	if closed.Status != closedStatus || len(closed.Lines) != 2 {
		t.Fatalf("June settlement did not close with both members: %#v", closed)
	}
	if len(closed.Evidence) != len(open.Evidence) {
		t.Fatalf("closing changed the settlement evidence: open=%#v closed=%#v", open.Evidence, closed.Evidence)
	}
	next, err := settlements.Preview(ctx, householdID.String(), "2026-07")
	if err != nil {
		t.Fatal(err)
	}
	nextLines := make(map[string]int64, len(next))
	for _, line := range next {
		nextLines[line.MemberID] = line.AmountDueYen
	}
	if nextLines[memberA.String()] != 2000 || nextLines[memberB.String()] != 8000 {
		t.Fatalf("closed overpayment credit and unpaid contribution should carry once: %#v", nextLines)
	}

	for _, schedule := range []struct {
		memberID gofrsuuid.UUID
		amount   int64
		cadence  string
		startsOn time.Time
		endsOn   time.Time
	}{{
		memberID: memberA, amount: 1500, cadence: "WEEKLY",
		startsOn: time.Date(2026, time.August, 5, 0, 0, 0, 0, time.FixedZone("JST", 9*60*60)),
		endsOn:   time.Date(2026, time.August, 26, 0, 0, 0, 0, time.FixedZone("JST", 9*60*60)),
	}, {
		memberID: memberB, amount: 500, cadence: "CUSTOM",
		startsOn: time.Date(2026, time.August, 17, 0, 0, 0, 0, time.FixedZone("JST", 9*60*60)),
		endsOn:   time.Date(2026, time.September, 30, 0, 0, 0, 0, time.FixedZone("JST", 9*60*60)),
	}} {
		scheduleID, err := gofrsuuid.NewV7()
		if err != nil {
			t.Fatal(err)
		}
		active := true
		endsOn := sql.Null[time.Time]{V: schedule.endsOn, Valid: true}
		if _, err := models.FundingSchedules.Insert(&models.FundingScheduleSetter{
			ID: &scheduleID, HouseholdID: &householdID, MemberID: &schedule.memberID, PoolID: &poolID,
			AmountYen: &schedule.amount, Cadence: &schedule.cadence, StartsOn: &schedule.startsOn, EndsOn: &endsOn, Active: &active,
		}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
	}
	august, err := CalculatePeriod(ctx, executor, householdID.String(), "2026-08")
	if err != nil {
		t.Fatal(err)
	}
	augustScheduled := make(map[string]int64, len(august))
	for _, line := range august {
		augustScheduled[line.MemberID] = line.ScheduledYen
	}
	if augustScheduled[memberA.String()] != 12000 || augustScheduled[memberB.String()] != 4500 {
		t.Fatalf("weekly occurrences and custom date should be included in August: %#v", augustScheduled)
	}
	closedAugust, err := settlements.Close(ctx, CloseCommand{ActorID: memberA.String(), HouseholdID: householdID.String(), Period: "2026-08"})
	if err != nil {
		t.Fatal(err)
	}
	closedScheduled := make(map[string]int64, len(closedAugust.Lines))
	for _, line := range closedAugust.Lines {
		closedScheduled[line.MemberID] = line.ScheduledYen
	}
	if closedScheduled[memberA.String()] != 12000 || closedScheduled[memberB.String()] != 4500 {
		t.Fatalf("materialized weekly and custom obligations should match preview: %#v", closedScheduled)
	}

	newStartsOn := time.Date(2026, time.September, 15, 0, 0, 0, 0, time.FixedZone("JST", 9*60*60))
	scheduleService := funding_schedule.NewService(db)
	if _, err := scheduleService.Set(ctx, funding_schedule.SetCommand{
		ActorID: memberA.String(), HouseholdID: householdID.String(), MemberID: memberA.String(), PoolID: poolID.String(),
		AmountYen: 8000, Cadence: "MONTHLY", StartsOn: newStartsOn,
	}); err != nil {
		t.Fatal(err)
	}
	latestStartsOn := time.Date(2026, time.September, 25, 0, 0, 0, 0, time.FixedZone("JST", 9*60*60))
	latestSchedule, err := scheduleService.Set(ctx, funding_schedule.SetCommand{
		ActorID: memberA.String(), HouseholdID: householdID.String(), MemberID: memberA.String(), PoolID: poolID.String(),
		AmountYen: 10000, Cadence: "MONTHLY", StartsOn: latestStartsOn,
	})
	if err != nil {
		t.Fatal(err)
	}
	september, err := settlements.Preview(ctx, householdID.String(), "2026-09")
	if err != nil {
		t.Fatal(err)
	}
	septemberScheduled := make(map[string]int64, len(september))
	for _, line := range september {
		septemberScheduled[line.MemberID] = line.ScheduledYen
	}
	if septemberScheduled[memberA.String()] != 10000 {
		t.Fatalf("monthly schedule should use the amount effective at period end: %#v", septemberScheduled)
	}
	closedSeptember, err := settlements.Close(ctx, CloseCommand{ActorID: memberA.String(), HouseholdID: householdID.String(), Period: "2026-09"})
	if err != nil {
		t.Fatal(err)
	}
	var closedMemberAScheduled int64
	for _, line := range closedSeptember.Lines {
		if line.MemberID == memberA.String() {
			closedMemberAScheduled = line.ScheduledYen
		}
	}
	if closedMemberAScheduled != septemberScheduled[memberA.String()] {
		t.Fatalf("closed settlement should match the preview obligation: got %d, want %d", closedMemberAScheduled, septemberScheduled[memberA.String()])
	}
	memberAObligation, err := models.FundingObligations.Query(sm.Where(psql.And(
		models.FundingObligations.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.FundingObligations.Columns.MemberID.EQ(psql.Arg(memberA)),
		models.FundingObligations.Columns.PoolID.EQ(psql.Arg(poolID)),
		models.FundingObligations.Columns.Period.EQ(psql.Arg("2026-09")),
	))).One(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	latestScheduleID, err := gofrsuuid.FromString(latestSchedule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if memberAObligation.AmountYen != septemberScheduled[memberA.String()] || !memberAObligation.ScheduleID.Valid || memberAObligation.ScheduleID.V != latestScheduleID {
		t.Fatalf("obligation should preserve the period-end amount and schedule: %#v", memberAObligation)
	}
}
