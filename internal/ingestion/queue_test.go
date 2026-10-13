package ingestion

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestSafeQueueDialErrorDoesNotExposeEndpointCredentials(t *testing.T) {
	t.Parallel()

	const password = "queue-password"
	parseErr := errors.New("invalid URL escape")
	err := &url.Error{
		Op:  "parse",
		URL: "amqp://horse:" + password + "@rabbitmq.example/horse",
		Err: parseErr,
	}

	got := safeQueueDialError(err)
	if strings.Contains(got.Error(), password) {
		t.Fatal("queue dial error exposed endpoint credentials")
	}
	if !errors.Is(got, parseErr) {
		t.Fatalf("safe error does not wrap the original parse error: %v", got)
	}
}
