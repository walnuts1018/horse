package auth

import (
	"strings"
	"testing"
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
