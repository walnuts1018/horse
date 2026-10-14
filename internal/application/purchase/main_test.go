package purchase

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/walnuts1018/horse/internal/testinfra"
)

func TestMain(m *testing.M) {
	var terminate func(context.Context) error
	if os.Getenv("HORSE_TEST_DATABASE_URL") == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		databaseURL, stop, err := testinfra.StartPostgres(ctx)
		if err == nil {
			err = testinfra.ApplyMigrations(ctx, databaseURL)
		}
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "start purchase integration database: %v\n", err)
			if stop != nil {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cleanupCancel()
				if cleanupErr := stop(cleanupCtx); cleanupErr != nil {
					fmt.Fprintf(os.Stderr, "terminate PostgreSQL test container: %v\n", cleanupErr)
				}
			}
			os.Exit(1)
		}
		if err := os.Setenv("HORSE_TEST_DATABASE_URL", databaseURL); err != nil {
			fmt.Fprintf(os.Stderr, "set purchase integration database URL: %v\n", err)
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cleanupCancel()
			if cleanupErr := stop(cleanupCtx); cleanupErr != nil {
				fmt.Fprintf(os.Stderr, "terminate PostgreSQL test container: %v\n", cleanupErr)
			}
			os.Exit(1)
		}
		terminate = stop
	}
	code := m.Run()
	if terminate != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := terminate(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "terminate PostgreSQL test container: %v\n", err)
			code = 1
		}
		cancel()
	}
	os.Exit(code)
}
