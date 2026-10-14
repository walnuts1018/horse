package config

import "testing"

func TestParseGeminiQuotaLimits(t *testing.T) {
	limits, err := ParseGeminiQuotaLimits("free/test-model=1/2/3;paid/test-model=4/5/6")
	if err != nil {
		t.Fatal(err)
	}
	if got := limits[GeminiQuotaKey{ProjectRoute: "free", Model: "test-model"}]; got != (GeminiQuotaLimit{
		RequestsPerMinute: 1,
		InputTokensMinute: 2,
		RequestsPerDay:    3,
	}) {
		t.Fatalf("free quota = %#v", got)
	}
	if limits == nil {
		t.Fatal("unset quota configuration must produce an empty map")
	}
}

func TestParseGeminiQuotaLimitsRejectsInvalidEntries(t *testing.T) {
	for _, raw := range []string{
		"free/test-model=1/2",
		"other/test-model=1/2/3",
		"free/test-model=0/2/3",
		"free/test-model=1/2/3;free/test-model=1/2/3",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ParseGeminiQuotaLimits(raw); err == nil {
				t.Fatal("invalid quota configuration was accepted")
			}
		})
	}
}
