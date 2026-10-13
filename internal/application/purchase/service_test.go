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
