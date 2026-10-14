package ingestion

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"
	_ "time/tzdata"

	"github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type GeminiRateControl interface {
	Wait(context.Context, string, string) error
	RecordCooldown(context.Context, string, string, time.Duration) error
	Reserve(context.Context, string, string, int64, GeminiQuotaLimit) (string, time.Duration, error)
	Settle(context.Context, string, int64) error
}

type GeminiQuotaKey struct {
	ProjectRoute string
	Model        string
}

type GeminiQuotaLimit struct {
	RequestsPerMinute int64
	InputTokensMinute int64
	RequestsPerDay    int64
}

type postgresGeminiRateControl struct {
	db *pgxpool.Pool
}

var errGeminiQuotaRequestTooLarge = errors.New("Gemini input exceeds configured token quota")

const geminiQuotaDayLocation = "America/Los_Angeles"

type geminiQuotaUsage struct {
	requestsMinute int64
	tokensMinute   int64
	requestsDay    int64
	minuteRows     []*models.GeminiQuotaReservation
	dayRows        []*models.GeminiQuotaReservation
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
	tx, err := c.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Gemini cooldown update: %w", err)
	}
	executor := bobpgx.NewTx(tx, nil)
	abort := func(cause error) error {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			return errors.Join(cause, fmt.Errorf("rollback Gemini cooldown update: %w", rollbackErr))
		}
		return cause
	}
	_, err = models.GeminiQuotaReservationLocks.Insert(
		&models.GeminiQuotaReservationLockSetter{ProjectRoute: &projectRoute, Model: &model, UpdatedAt: &now},
		im.OnConflict("project_route", "model").DoUpdate(im.SetCol("updated_at").To(psql.Arg(now))),
	).Exec(ctx, executor)
	if err != nil {
		return abort(fmt.Errorf("lock Gemini cooldown scope: %w", err))
	}
	_, err = models.GeminiQuotaCooldowns.Insert(
		&models.GeminiQuotaCooldownSetter{
			ProjectRoute: &projectRoute,
			Model:        &model,
			BlockedUntil: &blockedUntil,
			UpdatedAt:    &now,
		},
		im.OnConflict("project_route", "model").DoUpdate(
			im.SetCol("blocked_until").To(psql.F(
				"GREATEST",
				psql.Quote("gemini_quota_cooldowns", "blocked_until"),
				im.Excluded("blocked_until"),
			)),
			im.SetCol("updated_at").To(psql.Arg(now)),
		),
	).Exec(ctx, executor)
	if err != nil {
		return abort(fmt.Errorf("record shared Gemini cooldown: %w", err))
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Gemini cooldown update: %w", err)
	}
	return nil
}

func (c postgresGeminiRateControl) Reserve(ctx context.Context, projectRoute, model string, inputTokens int64, limits GeminiQuotaLimit) (string, time.Duration, error) {
	if inputTokens < 1 || limits.RequestsPerMinute < 1 || limits.InputTokensMinute < 1 || limits.RequestsPerDay < 1 {
		return "", 0, fmt.Errorf("invalid Gemini quota reservation values")
	}
	if inputTokens > limits.InputTokensMinute {
		return "", 0, errGeminiQuotaRequestTooLarge
	}
	now := time.Now().UTC()
	dayStart, dayReset, err := geminiQuotaDayBounds(now)
	if err != nil {
		return "", 0, err
	}
	reservationID, err := uuid.NewV7()
	if err != nil {
		return "", 0, fmt.Errorf("create Gemini quota reservation ID: %w", err)
	}
	tx, err := c.db.Begin(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("begin Gemini quota reservation: %w", err)
	}
	executor := bobpgx.NewTx(tx, nil)
	abort := func(cause error) error {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			return errors.Join(cause, fmt.Errorf("rollback Gemini quota reservation: %w", rollbackErr))
		}
		return cause
	}
	_, err = models.GeminiQuotaReservationLocks.Insert(
		&models.GeminiQuotaReservationLockSetter{ProjectRoute: &projectRoute, Model: &model, UpdatedAt: &now},
		im.OnConflict("project_route", "model").DoUpdate(im.SetCol("updated_at").To(psql.Arg(now))),
	).Exec(ctx, executor)
	if err != nil {
		return "", 0, abort(fmt.Errorf("lock Gemini quota reservation scope: %w", err))
	}
	if cooldown, queryErr := models.GeminiQuotaCooldowns.Query(sm.Where(psql.And(
		models.GeminiQuotaCooldowns.Columns.ProjectRoute.EQ(psql.Arg(projectRoute)),
		models.GeminiQuotaCooldowns.Columns.Model.EQ(psql.Arg(model)),
	))).One(ctx, executor); queryErr == nil && cooldown.BlockedUntil.After(now) {
		delay := time.Until(cooldown.BlockedUntil)
		if err := abort(nil); err != nil {
			return "", 0, err
		}
		return "", delay, nil
	} else if queryErr != nil && !errors.Is(queryErr, sql.ErrNoRows) {
		return "", 0, abort(fmt.Errorf("load Gemini quota cooldown: %w", queryErr))
	}
	_, err = models.GeminiQuotaReservations.Delete(dm.Where(psql.And(
		models.GeminiQuotaReservations.Columns.ProjectRoute.EQ(psql.Arg(projectRoute)),
		models.GeminiQuotaReservations.Columns.Model.EQ(psql.Arg(model)),
		models.GeminiQuotaReservations.Columns.ReservedAt.LT(psql.Arg(dayStart)),
	))).Exec(ctx, executor)
	if err != nil {
		return "", 0, abort(fmt.Errorf("prune expired Gemini quota reservations: %w", err))
	}
	reservations, err := models.GeminiQuotaReservations.Query(sm.Where(psql.And(
		models.GeminiQuotaReservations.Columns.ProjectRoute.EQ(psql.Arg(projectRoute)),
		models.GeminiQuotaReservations.Columns.Model.EQ(psql.Arg(model)),
		models.GeminiQuotaReservations.Columns.ReservedAt.GTE(psql.Arg(dayStart)),
	))).All(ctx, executor)
	if err != nil {
		return "", 0, abort(fmt.Errorf("load Gemini quota reservations: %w", err))
	}
	usage := quotaUsage(reservations, now)
	if usage.requestsMinute+1 > limits.RequestsPerMinute || usage.tokensMinute+inputTokens > limits.InputTokensMinute || usage.requestsDay+1 > limits.RequestsPerDay {
		delay := quotaRetryAfter(usage, now, dayReset, inputTokens, limits)
		if err := abort(nil); err != nil {
			return "", 0, err
		}
		return "", delay, nil
	}
	reservedAt := now
	_, err = models.GeminiQuotaReservations.Insert(&models.GeminiQuotaReservationSetter{
		ID: &reservationID, ProjectRoute: &projectRoute, Model: &model, ReservedAt: &reservedAt, InputTokens: &inputTokens,
	}).Exec(ctx, executor)
	if err != nil {
		return "", 0, abort(fmt.Errorf("save Gemini quota reservation: %w", err))
	}
	if err := tx.Commit(ctx); err != nil {
		return "", 0, fmt.Errorf("commit Gemini quota reservation: %w", err)
	}
	return reservationID.String(), 0, nil
}

func geminiQuotaDayBounds(now time.Time) (time.Time, time.Time, error) {
	location, err := time.LoadLocation(geminiQuotaDayLocation)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("load Gemini quota day location: %w", err)
	}
	localNow := now.In(location)
	start := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
	reset := start.AddDate(0, 0, 1)
	return start.UTC(), reset.UTC(), nil
}

func (c postgresGeminiRateControl) Settle(ctx context.Context, reservationID string, actualInputTokens int64) error {
	id, err := uuid.FromString(reservationID)
	if err != nil {
		return fmt.Errorf("parse Gemini quota reservation ID: %w", err)
	}
	reservation, err := models.GeminiQuotaReservations.Query(sm.Where(
		models.GeminiQuotaReservations.Columns.ID.EQ(psql.Arg(id)),
	)).One(ctx, bobpgx.NewPool(c.db))
	if err != nil {
		return fmt.Errorf("load Gemini quota reservation: %w", err)
	}
	tx, err := c.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Gemini quota settlement: %w", err)
	}
	executor := bobpgx.NewTx(tx, nil)
	abort := func(cause error) error {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			return errors.Join(cause, fmt.Errorf("rollback Gemini quota settlement: %w", rollbackErr))
		}
		return cause
	}
	now := time.Now().UTC()
	_, err = models.GeminiQuotaReservationLocks.Insert(&models.GeminiQuotaReservationLockSetter{
		ProjectRoute: &reservation.ProjectRoute, Model: &reservation.Model, UpdatedAt: &now,
	}, im.OnConflict("project_route", "model").DoUpdate(im.SetCol("updated_at").To(psql.Arg(now)))).Exec(ctx, executor)
	if err != nil {
		return abort(fmt.Errorf("lock Gemini quota settlement scope: %w", err))
	}
	reservation, err = models.GeminiQuotaReservations.Query(sm.Where(
		models.GeminiQuotaReservations.Columns.ID.EQ(psql.Arg(id)),
	)).One(ctx, executor)
	if err != nil {
		return abort(fmt.Errorf("reload Gemini quota reservation: %w", err))
	}
	settledAt := time.Now().UTC()
	reservation.ActualInputTokens = sql.Null[int64]{V: actualInputTokens, Valid: true}
	reservation.SettledAt = sql.Null[time.Time]{V: settledAt, Valid: true}
	if err := reservation.Update(ctx, executor, &models.GeminiQuotaReservationSetter{
		ActualInputTokens: &reservation.ActualInputTokens,
		SettledAt:         &reservation.SettledAt,
	}); err != nil {
		return abort(fmt.Errorf("update Gemini quota reservation: %w", err))
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Gemini quota settlement: %w", err)
	}
	return nil
}

func quotaUsage(reservations models.GeminiQuotaReservationSlice, now time.Time) geminiQuotaUsage {
	usage := geminiQuotaUsage{}
	minuteStart := now.Add(-time.Minute)
	for _, reservation := range reservations {
		usage.requestsDay++
		usage.dayRows = append(usage.dayRows, reservation)
		if reservation.ReservedAt.After(minuteStart) {
			usage.requestsMinute++
			inputTokens := reservation.InputTokens
			if reservation.ActualInputTokens.Valid {
				inputTokens = reservation.ActualInputTokens.V
			}
			usage.tokensMinute += inputTokens
			usage.minuteRows = append(usage.minuteRows, reservation)
		}
	}
	sort.Slice(usage.minuteRows, func(i, j int) bool {
		return usage.minuteRows[i].ReservedAt.Before(usage.minuteRows[j].ReservedAt)
	})
	sort.Slice(usage.dayRows, func(i, j int) bool {
		return usage.dayRows[i].ReservedAt.Before(usage.dayRows[j].ReservedAt)
	})
	return usage
}

func quotaRetryAfter(usage geminiQuotaUsage, now, dayReset time.Time, inputTokens int64, limits GeminiQuotaLimit) time.Duration {
	var delays []time.Duration
	if usage.requestsMinute+1 > limits.RequestsPerMinute {
		delays = append(delays, usage.minuteRows[0].ReservedAt.Add(time.Minute).Sub(now))
	}
	if usage.tokensMinute+inputTokens > limits.InputTokensMinute {
		remaining := usage.tokensMinute
		for _, reservation := range usage.minuteRows {
			reservedTokens := reservation.InputTokens
			if reservation.ActualInputTokens.Valid {
				reservedTokens = reservation.ActualInputTokens.V
			}
			remaining -= reservedTokens
			if remaining+inputTokens <= limits.InputTokensMinute {
				delays = append(delays, reservation.ReservedAt.Add(time.Minute).Sub(now))
				break
			}
		}
	}
	if usage.requestsDay+1 > limits.RequestsPerDay {
		delays = append(delays, dayReset.Sub(now))
	}
	var retryAfter time.Duration
	for _, delay := range delays {
		if delay > retryAfter {
			retryAfter = delay
		}
	}
	if retryAfter <= 0 {
		return time.Second
	}
	return retryAfter
}
