package settlement

import (
	"database/sql"
	"testing"
	"time"

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
