package planning

import (
	"testing"
	"time"
)

func TestNextRecurringDue(t *testing.T) {
	t.Parallel()
	const january31 = "2026-01-31"
	tests := []struct {
		name    string
		start   string
		due     string
		cadence string
		want    string
	}{
		{name: "weekly", start: "2026-10-14", due: "2026-10-14", cadence: "WEEKLY", want: "2026-10-21"},
		{name: "month end", start: january31, due: january31, cadence: "MONTHLY", want: "2026-02-28"},
		{name: "month end returns to start day", start: january31, due: "2026-02-28", cadence: "MONTHLY", want: "2026-03-31"},
		{name: "leap day returns to start date", start: "2024-02-29", due: "2027-02-28", cadence: "YEARLY", want: "2028-02-29"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			start, err := time.Parse(time.DateOnly, test.start)
			if err != nil {
				t.Fatal(err)
			}
			due, err := time.Parse(time.DateOnly, test.due)
			if err != nil {
				t.Fatal(err)
			}
			got, err := nextRecurringDue(start, due, test.cadence)
			if err != nil {
				t.Fatal(err)
			}
			if got.Format(time.DateOnly) != test.want {
				t.Fatalf("nextRecurringDue(%s, %s) = %s, want %s", test.due, test.cadence, got.Format(time.DateOnly), test.want)
			}
		})
	}
}
