package ingestion

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestPostgresGeminiRateControlKeepsLongestCooldown(t *testing.T) {
	t.Parallel()

	databaseURL := os.Getenv("HORSE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("HORSE_TEST_DATABASE_URL is not set")
	}
	ctx := t.Context()
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	model := "test-" + testUUID(t)
	projectRoute := ProjectFree
	control := postgresGeminiRateControl{db: db}
	defer cleanupQuotaReservations(t, db, projectRoute, model)
	if err := control.RecordCooldown(ctx, projectRoute, model, time.Minute); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.GeminiQuotaCooldowns.Delete(dm.Where(psql.And(
			models.GeminiQuotaCooldowns.Columns.ProjectRoute.EQ(psql.Arg(projectRoute)),
			models.GeminiQuotaCooldowns.Columns.Model.EQ(psql.Arg(model)),
		))).Exec(t.Context(), bobpgx.NewPool(db))
	}()

	first, err := models.GeminiQuotaCooldowns.Query(sm.Where(psql.And(
		models.GeminiQuotaCooldowns.Columns.ProjectRoute.EQ(psql.Arg(projectRoute)),
		models.GeminiQuotaCooldowns.Columns.Model.EQ(psql.Arg(model)),
	))).One(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if err := control.RecordCooldown(ctx, projectRoute, model, time.Second); err != nil {
		t.Fatal(err)
	}
	latest, err := models.GeminiQuotaCooldowns.Query(sm.Where(psql.And(
		models.GeminiQuotaCooldowns.Columns.ProjectRoute.EQ(psql.Arg(projectRoute)),
		models.GeminiQuotaCooldowns.Columns.Model.EQ(psql.Arg(model)),
	))).One(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if latest.BlockedUntil.Before(first.BlockedUntil) {
		t.Fatalf("shorter cooldown reduced blocked_until from %s to %s", first.BlockedUntil, latest.BlockedUntil)
	}
	reservationID, retryAfter, err := control.Reserve(ctx, projectRoute, model, 1, GeminiQuotaLimit{
		RequestsPerMinute: 1, InputTokensMinute: 1, RequestsPerDay: 1,
	})
	if err != nil || reservationID != "" || retryAfter <= 0 {
		t.Fatalf("cooldown reservation ID=%q retry-after=%s error=%v", reservationID, retryAfter, err)
	}
}

func TestPostgresGeminiQuotaReservationsAreAtomic(t *testing.T) {
	t.Parallel()

	databaseURL := os.Getenv("HORSE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("HORSE_TEST_DATABASE_URL is not set")
	}
	ctx := t.Context()
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	model := "test-" + testUUID(t)
	control := postgresGeminiRateControl{db: db}
	defer cleanupQuotaReservations(t, db, ProjectFree, model)

	const workers = 16
	limits := GeminiQuotaLimit{RequestsPerMinute: 5, InputTokensMinute: 100, RequestsPerDay: 5}
	type result struct {
		reservationID string
		err           error
	}
	results := make(chan result, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Go(func() {
			reservationID, _, reserveErr := control.Reserve(ctx, ProjectFree, model, 1, limits)
			results <- result{reservationID: reservationID, err: reserveErr}
		})
	}
	wait.Wait()
	close(results)
	reserved := 0
	deferred := 0
	for result := range results {
		switch {
		case result.err == nil && result.reservationID != "":
			reserved++
		case result.err == nil:
			deferred++
		case result.err != nil:
			t.Errorf("quota reservation: %v", result.err)
		}
	}
	if reserved != 5 {
		t.Fatalf("concurrent requests reserved = %d, want %d", reserved, 5)
	}
	if deferred != workers-reserved {
		t.Fatalf("deferred requests = %d, want %d", deferred, workers-reserved)
	}

	modelTokens := "test-" + testUUID(t)
	defer cleanupQuotaReservations(t, db, ProjectFree, modelTokens)
	limitedTokens := GeminiQuotaLimit{RequestsPerMinute: 5, InputTokensMinute: 6, RequestsPerDay: 10}
	reservationID, _, err := control.Reserve(ctx, ProjectFree, modelTokens, 4, limitedTokens)
	if err != nil || reservationID == "" {
		t.Fatalf("first token reservation ID=%q error=%v", reservationID, err)
	}
	if err := control.Settle(ctx, reservationID, 2); err != nil {
		t.Fatal(err)
	}
	if _, _, err := control.Reserve(ctx, ProjectFree, modelTokens, 4, limitedTokens); err != nil {
		t.Fatalf("settled token usage must replace its estimate: %v", err)
	}
	if _, retryAfter, err := control.Reserve(ctx, ProjectFree, modelTokens, 1, limitedTokens); err != nil || retryAfter <= 0 {
		t.Fatalf("token limit retry-after=%s error=%v, want a deferred reservation", retryAfter, err)
	}

	modelDaily := "test-" + testUUID(t)
	defer cleanupQuotaReservations(t, db, ProjectFree, modelDaily)
	lockUpdatedAt := time.Now().UTC()
	route := ProjectFree
	if _, err := models.GeminiQuotaReservationLocks.Insert(&models.GeminiQuotaReservationLockSetter{
		ProjectRoute: &route, Model: &modelDaily, UpdatedAt: &lockUpdatedAt,
	}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	oldReservationID, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	oldReservationAt := time.Now().Add(-time.Hour).UTC()
	oldTokens := int64(1)
	if _, err := models.GeminiQuotaReservations.Insert(&models.GeminiQuotaReservationSetter{
		ID: &oldReservationID, ProjectRoute: &route, Model: &modelDaily,
		ReservedAt: &oldReservationAt, InputTokens: &oldTokens,
	}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	if _, retryAfter, err := control.Reserve(ctx, ProjectFree, modelDaily, 1, GeminiQuotaLimit{
		RequestsPerMinute: 5, InputTokensMinute: 100, RequestsPerDay: 1,
	}); err != nil || retryAfter < 22*time.Hour {
		t.Fatalf("daily limit retry-after=%s error=%v, want approximately 23 hours", retryAfter, err)
	}
}

func cleanupQuotaReservations(t *testing.T, db *pgxpool.Pool, projectRoute, model string) {
	t.Helper()
	ctx := context.Background()
	if _, err := models.GeminiQuotaReservations.Delete(dm.Where(psql.And(
		models.GeminiQuotaReservations.Columns.ProjectRoute.EQ(psql.Arg(projectRoute)),
		models.GeminiQuotaReservations.Columns.Model.EQ(psql.Arg(model)),
	))).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Errorf("delete Gemini quota reservations: %v", err)
	}
	if _, err := models.GeminiQuotaReservationLocks.Delete(dm.Where(psql.And(
		models.GeminiQuotaReservationLocks.Columns.ProjectRoute.EQ(psql.Arg(projectRoute)),
		models.GeminiQuotaReservationLocks.Columns.Model.EQ(psql.Arg(model)),
	))).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Errorf("delete Gemini quota reservation lock: %v", err)
	}
}
