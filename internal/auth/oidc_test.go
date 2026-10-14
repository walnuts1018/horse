package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveLookupError(t *testing.T) {
	t.Parallel()

	t.Run("missing row is unauthenticated", func(t *testing.T) {
		t.Parallel()
		if err := resolveLookupError("resolve session", sql.ErrNoRows); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("resolveLookupError() = %v, want ErrUnauthenticated", err)
		}
	})

	t.Run("lookup failure preserves cause", func(t *testing.T) {
		t.Parallel()
		if err := resolveLookupError("resolve session", context.DeadlineExceeded); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("resolveLookupError() = %v, want context.DeadlineExceeded", err)
		} else if errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("resolveLookupError() = %v, want a non-authentication error", err)
		}
	})
}

func TestMeResponseClassifiesResolveErrors(t *testing.T) {
	t.Parallel()

	t.Run("unauthenticated response remains false with status ok", func(t *testing.T) {
		t.Parallel()
		response := httptest.NewRecorder()
		writeMeResponse(response, Principal{}, fmt.Errorf("resolve session: %w", ErrUnauthenticated))

		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
		}
		if body := response.Body.String(); !strings.Contains(body, `"authenticated":false`) {
			t.Fatalf("body = %q, want unauthenticated JSON", body)
		}
	})

	t.Run("dependency failure returns safe service unavailable response", func(t *testing.T) {
		t.Parallel()
		response := httptest.NewRecorder()
		cause := errors.New("database connection failed; credential=private")
		writeMeResponse(response, Principal{}, fmt.Errorf("resolve session: %w", cause))

		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
		}
		if body := response.Body.String(); body != "authentication unavailable\n" {
			t.Fatalf("body = %q, want a generic authentication error", body)
		}
	})
}

func TestSafeRedirect(t *testing.T) {
	t.Parallel()
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
