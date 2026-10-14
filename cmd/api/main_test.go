package main

import (
	"testing"

	"github.com/walnuts1018/horse/internal/application/mailforwarding"
)

func TestConfiguredMailForwardingRequiresMatchingPoller(t *testing.T) {
	t.Parallel()
	const domain = "walnuts.dev"
	const prefix = "horse-mail+"
	const recipientBase = "horse-mail@walnuts.dev"

	tests := []struct {
		name          string
		pollerEnabled bool
		domain        string
		prefix        string
		base          string
		want          mailforwarding.Config
	}{
		{name: "poller disabled", domain: domain, prefix: prefix, base: recipientBase},
		{name: "recipient mismatch", pollerEnabled: true, domain: domain, prefix: prefix, base: "other@walnuts.dev"},
		{name: "domain mismatch", pollerEnabled: true, domain: domain, prefix: prefix, base: "horse-mail@example.test"},
		{
			name: "matching configuration", pollerEnabled: true, domain: domain, prefix: prefix, base: "horse-mail@WALNUTS.DEV",
			want: mailforwarding.Config{MailboxDomain: domain, AddressLocalPartPrefix: prefix},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := configuredMailForwarding(test.pollerEnabled, test.domain, test.prefix, test.base); got != test.want {
				t.Fatalf("configuredMailForwarding() = %+v, want %+v", got, test.want)
			}
		})
	}
}
