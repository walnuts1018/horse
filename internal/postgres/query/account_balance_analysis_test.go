package query

import (
	"math"
	"testing"
)

func TestCheckedAccountBalanceSubtract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  int64
		right int64
		want  int64
		ok    bool
	}{
		{name: "positive result", left: 8, right: 3, want: 5, ok: true},
		{name: "negative result", left: 3, right: 8, want: -5, ok: true},
		{name: "minimum boundary", left: math.MinInt64, right: math.MinInt64, want: 0, ok: true},
		{name: "maximum boundary", left: math.MaxInt64, right: math.MaxInt64, want: 0, ok: true},
		{name: "positive overflow", left: math.MaxInt64, right: -1},
		{name: "negative overflow", left: math.MinInt64, right: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := checkedAccountBalanceSubtract(tt.left, tt.right)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("checkedAccountBalanceSubtract(%d, %d) = (%d, %t), want (%d, %t)", tt.left, tt.right, got, ok, tt.want, tt.ok)
			}
		})
	}
}
