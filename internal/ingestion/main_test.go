package ingestion

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/walnuts1018/horse/internal/testinfra"
)

func TestMain(m *testing.M) {
	terminate := setupIngestionTestDatabase()
	code := m.Run()
	if terminate != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := terminate(cleanupCtx); err != nil {
			log.Printf("terminate PostgreSQL test container: %v", err)
		}
	}
	os.Exit(code)
}

func setupIngestionTestDatabase() func(context.Context) error {
	if os.Getenv("HORSE_TEST_DATABASE_URL") != "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	databaseURL, terminate, err := testinfra.StartPostgres(ctx)
	if err == nil {
		err = testinfra.ApplyMigrations(ctx, databaseURL)
	}
	cancel()
	if err != nil {
		log.Printf("ingestion integration tests are unavailable: %v", err)
		if terminate != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cleanupCancel()
			if cleanupErr := terminate(cleanupCtx); cleanupErr != nil {
				log.Printf("terminate PostgreSQL test container: %v", cleanupErr)
			}
		}
		return nil
	}
	if err := os.Setenv("HORSE_TEST_DATABASE_URL", databaseURL); err != nil {
		log.Printf("ingestion integration tests are unavailable: %v", err)
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if cleanupErr := terminate(cleanupCtx); cleanupErr != nil {
			log.Printf("terminate PostgreSQL test container: %v", cleanupErr)
		}
		return nil
	}
	return terminate
}
