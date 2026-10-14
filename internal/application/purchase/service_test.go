package purchase

import (
	"errors"
	"testing"

	gofrsuuid "github.com/gofrs/uuid/v5"
)

func TestParseUUID(t *testing.T) {
	t.Parallel()

	const value = "018f47d4-9e1a-7cc0-85b8-4f98c3429472"
	want, err := gofrsuuid.FromString(value)
	if err != nil {
		t.Fatalf("FromString() error = %v", err)
	}
	got, err := parseUUID(value, "purchase")
	if err != nil {
		t.Fatalf("parseUUID() error = %v", err)
	}
	if got != want {
		t.Fatalf("parseUUID() = %s, want %s", got, want)
	}

	_, err = parseUUID("invalid", "purchase")
	if !errors.Is(err, ErrBadInput) {
		t.Fatalf("parseUUID() error = %v, want ErrBadInput", err)
	}
}

func TestPurchaseOrigin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input CreateCommand
		want  string
	}{
		{name: "manual", want: manualOrigin},
		{name: "imported", input: CreateCommand{SourceDocumentID: new("document")}, want: "AI"},
		{name: "explicit", input: CreateCommand{Origin: "UNKNOWN", SourceDocumentID: new("document")}, want: "UNKNOWN"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := purchaseOrigin(test.input); got != test.want {
				t.Fatalf("purchaseOrigin() = %q, want %q", got, test.want)
			}
		})
	}
}
