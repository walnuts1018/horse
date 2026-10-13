package accounting

import (
	"testing"
	"time"
)

func TestPeriodFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		when time.Time
		want string
	}{
		{
			name: "UTC timestamp belongs to the next month in Japan",
			when: time.Date(2026, time.January, 31, 15, 0, 0, 0, time.UTC),
			want: "2026-02",
		},
		{
			name: "timestamp before the Japan month boundary remains in the current month",
			when: time.Date(2026, time.January, 31, 14, 59, 59, 0, time.UTC),
			want: "2026-01",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := PeriodFor(test.when); got != test.want {
				t.Errorf("PeriodFor(%s) = %q, want %q", test.when, got, test.want)
			}
		})
	}
}

func TestAdd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  int64
		right int64
		want  int64
		ok    bool
	}{
		{name: "adds values", left: 4, right: 7, want: 11, ok: true},
		{name: "detects positive overflow", left: int64(^uint64(0) >> 1), right: 1},
		{name: "detects negative overflow", left: -int64(^uint64(0)>>1) - 1, right: -1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, ok := Add(test.left, test.right)
			if got != test.want || ok != test.ok {
				t.Errorf("Add(%d, %d) = (%d, %t), want (%d, %t)", test.left, test.right, got, ok, test.want, test.ok)
			}
		})
	}
}
