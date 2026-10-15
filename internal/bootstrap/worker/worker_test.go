package worker

import (
	"testing"

	"github.com/walnuts1018/horse/internal/config"
)

func TestProvideRuntimeEnablesQueueProcessingWithoutGeminiAPIKey(t *testing.T) {
	t.Parallel()

	runtime, err := provideRuntime(config.Worker{GeminiEnabled: true}, telemetryResult{}, databaseResult{}, objectStoreResult{})
	if err != nil {
		t.Fatal(err)
	}
	if !runtime.Worker.GeminiEnabled {
		t.Fatal("expected Gemini queue processing to remain enabled without API keys")
	}
}
