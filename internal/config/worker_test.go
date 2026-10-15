package config

import "testing"

const (
	databaseURLEnvKey = "DATABASE_URL"
	s3EndpointEnvKey  = "S3_ENDPOINT"
	s3BucketEnvKey    = "S3_BUCKET"
	geminiEnabledKey  = "GEMINI_ENABLED"
	usdJPYRateKey     = "USDJPY_RATE"
	testDatabaseURL   = "postgres://localhost/horse"
	testS3Endpoint    = "http://seaweedfs:8333"
	testS3Bucket      = "horse"
)

func TestLoadWorkerResolvesDefaultsAndRateAliases(t *testing.T) {
	t.Parallel()

	base := map[string]string{
		databaseURLEnvKey: testDatabaseURL,
		s3EndpointEnvKey:  testS3Endpoint,
		s3BucketEnvKey:    testS3Bucket,
	}
	defaults, err := loadWorker(base)
	if err != nil {
		t.Fatal(err)
	}
	if defaults.GeminiEnabled || defaults.EffectiveUSDJPYRate() != defaultUSDJPYRate || defaults.EffectiveUSDJPYRateSource() != "application_default" {
		t.Fatalf("unexpected worker defaults: enabled=%t rate=%v source=%q", defaults.GeminiEnabled, defaults.EffectiveUSDJPYRate(), defaults.EffectiveUSDJPYRateSource())
	}

	aliases := map[string]string{
		databaseURLEnvKey:    testDatabaseURL,
		s3EndpointEnvKey:     testS3Endpoint,
		s3BucketEnvKey:       testS3Bucket,
		geminiEnabledKey:     "",
		usdJPYRateKey:        "",
		"GEMINI_USDJPY_RATE": "140.5",
	}
	aliased, err := loadWorker(aliases)
	if err != nil {
		t.Fatal(err)
	}
	if aliased.GeminiEnabled || aliased.EffectiveUSDJPYRate() != 140.5 || aliased.EffectiveUSDJPYRateSource() != "operator_config" {
		t.Fatalf("unexpected legacy rate fallback: enabled=%t rate=%v source=%q", aliased.GeminiEnabled, aliased.EffectiveUSDJPYRate(), aliased.EffectiveUSDJPYRateSource())
	}

	configured := map[string]string{
		databaseURLEnvKey:        testDatabaseURL,
		s3EndpointEnvKey:         testS3Endpoint,
		s3BucketEnvKey:           testS3Bucket,
		geminiEnabledKey:         "true",
		"GEMINI_FREE_API_KEY":    "test-key",
		"USDJPY_RATE":            "150.25",
		"GEMINI_USDJPY_RATE":     "140.5",
		"USDJPY_RATE_SOURCE":     "plan-reference",
		"USDJPY_RATE_AS_OF":      "2026-10-12",
		"GEMINI_FLASH_MODEL":     "flash-model",
		"GEMINI_FREE_PROJECT_ID": "free-project",
	}
	settings, err := loadWorker(configured)
	if err != nil {
		t.Fatal(err)
	}
	if !settings.GeminiEnabled || settings.EffectiveUSDJPYRate() != 150.25 || settings.EffectiveUSDJPYRateSource() != "plan-reference" || settings.USDJPYRateAsOf != "2026-10-12" || settings.GeminiFlashModel != "flash-model" || settings.GeminiFreeProject != "free-project" {
		t.Fatalf("worker environment was not resolved as expected: enabled=%t rate=%v source=%q asOf=%q", settings.GeminiEnabled, settings.EffectiveUSDJPYRate(), settings.EffectiveUSDJPYRateSource(), settings.USDJPYRateAsOf)
	}
}

func TestLoadWorkerAllowsGeminiWithoutAPIKey(t *testing.T) {
	t.Parallel()

	settings, err := loadWorker(map[string]string{
		databaseURLEnvKey: testDatabaseURL,
		s3EndpointEnvKey:  testS3Endpoint,
		s3BucketEnvKey:    testS3Bucket,
		geminiEnabledKey:  "true",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !settings.GeminiEnabled || settings.GeminiFreeAPIKey != "" || settings.GeminiPaidAPIKey != "" {
		t.Fatalf("unexpected Gemini worker settings: enabled=%t free key set=%t paid key set=%t", settings.GeminiEnabled, settings.GeminiFreeAPIKey != "", settings.GeminiPaidAPIKey != "")
	}
}

func TestLoadWorkerRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		environment map[string]string
	}{
		{
			name: "missing database url",
			environment: map[string]string{
				s3EndpointEnvKey: testS3Endpoint,
				s3BucketEnvKey:   testS3Bucket,
			},
		},
		{
			name: "incomplete object store credentials",
			environment: map[string]string{
				databaseURLEnvKey:  testDatabaseURL,
				s3EndpointEnvKey:   testS3Endpoint,
				s3BucketEnvKey:     testS3Bucket,
				"S3_ACCESS_KEY_ID": "key",
			},
		},
		{
			name: "non-positive exchange rate",
			environment: map[string]string{
				databaseURLEnvKey: testDatabaseURL,
				s3EndpointEnvKey:  testS3Endpoint,
				s3BucketEnvKey:    testS3Bucket,
				usdJPYRateKey:     "0",
			},
		},
		{
			name: "invalid rate date",
			environment: map[string]string{
				databaseURLEnvKey:   testDatabaseURL,
				s3EndpointEnvKey:    testS3Endpoint,
				s3BucketEnvKey:      testS3Bucket,
				"USDJPY_RATE_AS_OF": "2026-02-30",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := loadWorker(tt.environment); err == nil {
				t.Fatal("expected invalid worker configuration error")
			}
		})
	}
}
