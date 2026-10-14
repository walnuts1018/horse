package ingestion

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type GeminiRateControl interface {
	Wait(context.Context, string, string) error
	RecordCooldown(context.Context, string, string, time.Duration) error
}

type postgresGeminiRateControl struct {
	db *pgxpool.Pool
}

func (c postgresGeminiRateControl) Wait(ctx context.Context, projectRoute, model string) error {
	for {
		state, err := models.GeminiQuotaCooldowns.Query(sm.Where(psql.And(
			models.GeminiQuotaCooldowns.Columns.ProjectRoute.EQ(psql.Arg(projectRoute)),
			models.GeminiQuotaCooldowns.Columns.Model.EQ(psql.Arg(model)),
		))).One(ctx, bobpgx.NewPool(c.db))
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("load shared Gemini cooldown: %w", err)
		}
		delay := time.Until(state.BlockedUntil)
		if delay <= 0 {
			return nil
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}

			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (c postgresGeminiRateControl) RecordCooldown(ctx context.Context, projectRoute, model string, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	now := time.Now().UTC()
	blockedUntil := now.Add(delay)
	updatedAt := now
	_, err := models.GeminiQuotaCooldowns.Insert(
		&models.GeminiQuotaCooldownSetter{
			ProjectRoute: &projectRoute,
			Model:        &model,
			BlockedUntil: &blockedUntil,
			UpdatedAt:    &updatedAt,
		},
		im.OnConflict("project_route", "model").DoUpdate(
			im.SetCol("blocked_until").To(psql.F(
				"GREATEST",
				psql.Quote("gemini_quota_cooldowns", "blocked_until"),
				im.Excluded("blocked_until"),
			)),
			im.SetCol("updated_at").To(psql.Arg(updatedAt)),
		),
	).Exec(ctx, bobpgx.NewPool(c.db))
	if err != nil {
		return fmt.Errorf("record shared Gemini cooldown: %w", err)
	}

	return nil
}
