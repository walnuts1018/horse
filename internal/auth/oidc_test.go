package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/oauth2"
)

func TestNewRejectsIncompleteOIDCConfiguration(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		config  Config
		missing string
	}{
		{
			name:    "issuer",
			config:  Config{ClientID: "client", RedirectURL: "https://horse.test/auth/callback"},
			missing: "issuer URL",
		},
		{
			name:    "client ID",
			config:  Config{IssuerURL: "https://auth.test", RedirectURL: "https://horse.test/auth/callback"},
			missing: "client ID",
		},
		{
			name:    "redirect URL",
			config:  Config{IssuerURL: "https://auth.test", ClientID: "client"},
			missing: "redirect URL",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service, err := New(t.Context(), nil, test.config)
			if service != nil || err == nil || !strings.Contains(err.Error(), test.missing) {
				t.Fatalf("New() = (%v, %v), want incomplete configuration error containing %q", service, err, test.missing)
			}
		})
	}
}

func TestNewDisablesOIDCWhenConfigurationIsEmpty(t *testing.T) {
	t.Parallel()

	service, err := New(t.Context(), nil, Config{})
	if service != nil || err != nil {
		t.Fatalf("New() = (%v, %v), want (nil, nil) when OIDC is disabled", service, err)
	}
}

func TestLogOperationalFailureOmitsSensitiveErrorText(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	service := &Service{logger: slog.New(slog.NewTextHandler(&output, nil))}
	service.logOperationalFailure(t.Context(), "store OIDC session", &pgconn.PgError{
		Code:    "23505",
		Message: "private user@example.test state=private-state code=private-code token=private-token",
	})

	log := output.String()
	if !strings.Contains(log, "store OIDC session") || !strings.Contains(log, "error_type=*pgconn.PgError") || !strings.Contains(log, "database_code=23505") {
		t.Fatalf("operational log is missing safe diagnostics: %s", log)
	}
	for _, privateValue := range []string{"user@example.test", "private-state", "private-code", "private-token"} {
		if strings.Contains(log, privateValue) {
			t.Fatalf("operational log exposed %q: %s", privateValue, log)
		}
	}
}

func TestLogOperationalFailureIncludesProviderStatusWithoutResponseBody(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	service := &Service{logger: slog.New(slog.NewTextHandler(&output, nil))}
	err := fmt.Errorf("exchange failed: %w", &oauth2.RetrieveError{
		Response:  &http.Response{StatusCode: http.StatusBadGateway},
		ErrorCode: "server_error",
		Body:      []byte("private response state=private-state code=private-code token=private-token"),
	})
	service.logOperationalFailure(t.Context(), "exchange OIDC authorization code", err)

	log := output.String()
	if !strings.Contains(log, "provider_status=502") {
		t.Fatalf("operational log is missing provider status: %s", log)
	}
	for _, privateValue := range []string{"private response", "private-state", "private-code", "private-token"} {
		if strings.Contains(log, privateValue) {
			t.Fatalf("operational log exposed %q: %s", privateValue, log)
		}
	}
}

func TestIsOperationalTokenExchangeError(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "authorization code rejected",
			err:  &oauth2.RetrieveError{Response: &http.Response{StatusCode: http.StatusBadRequest}, ErrorCode: oauthInvalidGrantError},
		},
		{
			name: "authorization code rejected by unauthorized response",
			err:  &oauth2.RetrieveError{Response: &http.Response{StatusCode: http.StatusUnauthorized}, ErrorCode: oauthInvalidGrantError},
		},
		{
			name: "client configuration rejected",
			err:  &oauth2.RetrieveError{Response: &http.Response{StatusCode: http.StatusBadRequest}, ErrorCode: "invalid_client"},
			want: true,
		},
		{
			name: "provider unavailable",
			err:  &oauth2.RetrieveError{Response: &http.Response{StatusCode: http.StatusServiceUnavailable}, ErrorCode: oauthInvalidGrantError},
			want: true,
		},
		{
			name: "provider rate limited",
			err:  &oauth2.RetrieveError{Response: &http.Response{StatusCode: http.StatusTooManyRequests}},
			want: true,
		},
		{
			name: "transport failure",
			err:  context.DeadlineExceeded,
			want: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := isOperationalTokenExchangeError(test.err); got != test.want {
				t.Fatalf("isOperationalTokenExchangeError() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestIsOperationalIDTokenVerificationError(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "remote key retrieval failure",
			err:  errors.New("failed to verify signature: fetching keys oidc: get keys failed: private token=secret state=secret code=secret"),
			want: true,
		},
		{
			name: "invalid signature",
			err:  errors.New("failed to verify signature: failed to verify id token signature"),
		},
		{
			name: "malformed token",
			err:  errors.New("oidc: malformed jwt"),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := isOperationalIDTokenVerificationError(test.err); got != test.want {
				t.Fatalf("isOperationalIDTokenVerificationError() = %t, want %t", got, test.want)
			}
			if !test.want {
				return
			}

			var output bytes.Buffer
			service := &Service{logger: slog.New(slog.NewTextHandler(&output, nil))}
			service.logOperationalFailure(t.Context(), "verify OIDC ID token", test.err)
			for _, privateValue := range []string{"secret", "private token", "state", "code"} {
				if strings.Contains(output.String(), privateValue) {
					t.Fatalf("operational verification log exposed %q: %s", privateValue, output.String())
				}
			}
		})
	}
}
