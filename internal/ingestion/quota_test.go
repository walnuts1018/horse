package ingestion

import (
	"os"
	"testing"
	"time"

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
}
