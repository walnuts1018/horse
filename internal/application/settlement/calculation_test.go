package settlement

import (
	"database/sql"
	"testing"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestMemberActiveDuringPeriod(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	joinedBefore := start.AddDate(0, -1, 0)
	joinedAfter := start.AddDate(0, 1, 1)
	leftAtStart := sql.Null[time.Time]{V: start, Valid: true}
	leftDuringPeriod := sql.Null[time.Time]{V: start.AddDate(0, 0, 10), Valid: true}
	leftBeforePeriod := sql.Null[time.Time]{V: start.Add(-time.Hour), Valid: true}

	tests := []struct {
		name     string
		joinedAt time.Time
		leftAt   sql.Null[time.Time]
		want     bool
	}{
		{name: "active member", joinedAt: joinedBefore, want: true},
		{name: "backdated household history", joinedAt: joinedAfter, want: true},
		{name: "left during period", joinedAt: joinedBefore, leftAt: leftDuringPeriod, want: true},
		{name: "left at period start", joinedAt: joinedBefore, leftAt: leftAtStart, want: false},
		{name: "left before period", joinedAt: joinedBefore, leftAt: leftBeforePeriod, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			member := &models.HouseholdMember{JoinedAt: test.joinedAt, LeftAt: test.leftAt}
			if got := memberActiveDuringPeriod(member, start); got != test.want {
				t.Fatalf("memberActiveDuringPeriod() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestFundingScheduleUsesPeriodEndSnapshot(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	memberID := gofrsuuid.Must(gofrsuuid.NewV7())
	poolID := gofrsuuid.Must(gofrsuuid.NewV7())
	priorID := gofrsuuid.Must(gofrsuuid.NewV7())
	middleID := gofrsuuid.Must(gofrsuuid.NewV7())
	latestID := gofrsuuid.Must(gofrsuuid.NewV7())
	nextMonthID := gofrsuuid.Must(gofrsuuid.NewV7())
	endedID := gofrsuuid.Must(gofrsuuid.NewV7())
	cadence := "MONTHLY"
	active := true

	schedules := models.FundingScheduleSlice{
		{ID: priorID, MemberID: memberID, PoolID: poolID, AmountYen: 4000, Cadence: cadence,
			StartsOn: start, EndsOn: sql.Null[time.Time]{V: start.AddDate(0, 0, 14), Valid: true}},
		{ID: middleID, MemberID: memberID, PoolID: poolID, AmountYen: 8000, Cadence: cadence,
			StartsOn: start.AddDate(0, 0, 14), EndsOn: sql.Null[time.Time]{V: start.AddDate(0, 0, 24), Valid: true}},
		{ID: latestID, MemberID: memberID, PoolID: poolID, AmountYen: 10000, Cadence: cadence,
			StartsOn: start.AddDate(0, 0, 24), Active: active},
		{ID: nextMonthID, MemberID: memberID, PoolID: poolID, AmountYen: 12000, Cadence: cadence,
			StartsOn: end, Active: active},
		{ID: endedID, MemberID: memberID, PoolID: poolID, AmountYen: 2000, Cadence: cadence,
			StartsOn: start.AddDate(0, -1, 0), EndsOn: sql.Null[time.Time]{V: start.AddDate(0, 0, -1), Valid: true}},
	}

	amounts, err := aggregateFundingSchedules(start, end, schedules)
	if err != nil {
		t.Fatal(err)
	}
	key := fundingKey{memberID: memberID.String(), poolID: poolID.String()}
	got, exists := amounts[key]
	if !exists || got.amount != 10000 || got.schedule.ID != latestID {
		t.Fatalf("period-end schedule snapshot = %#v, want latest 10000-yen schedule", got)
	}
}
