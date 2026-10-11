package auth

import "testing"

func TestSafeRedirect(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "/household/123?tab=ledger", want: "/household/123?tab=ledger"},
		{input: "https://attacker.example/", want: "/"},
		{input: "//attacker.example/", want: "/"},
		{input: "/%5c%5cattacker.example/", want: "/"},
		{input: "/\r\nLocation:https://attacker.example", want: "/"},
	}
	for _, test := range tests {
		if got := safeRedirect(test.input); got != test.want {
			t.Errorf("safeRedirect(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}
