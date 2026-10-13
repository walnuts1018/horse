package ingestion

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (w *Worker) RecoverExhausted(ctx context.Context) error {
	cutoff := time.Now().Add(-35 * time.Minute)
	tx, err := w.DB.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin stale extraction recovery: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.Error("Rollback stale extraction recovery failed", "error", err)
		}
	}()
	executor := bobpgx.NewTx(tx, nil)
	staleJobs, err := models.ExtractionJobs.Query(sm.Where(psql.And(
		models.ExtractionJobs.Columns.Status.EQ(psql.Arg("running")),
		models.ExtractionJobs.Columns.UpdatedAt.LT(psql.Arg(cutoff)),
	)), sm.ForUpdate()).All(ctx, executor)
	if err != nil {
		return fmt.Errorf("load stale extraction jobs: %w", err)
	}
	now := time.Now().UTC()
	for _, staleJob := range staleJobs {
		failureCount := staleJob.FailureCount + 1
		status, errorCode := extractionStatusQueued, "worker_interrupted"
		availableAt := now
		if failureCount >= 5 {
			status, errorCode, availableAt = extractionStatusFailed, "retry_exhausted", staleJob.AvailableAt
		}
		failureCode := sql.Null[string]{V: errorCode, Valid: true}
		if err := staleJob.Update(ctx, executor, &models.ExtractionJobSetter{
			FailureCount: &failureCount, Status: &status, ErrorCode: &failureCode,
			AvailableAt: &availableAt, UpdatedAt: &now,
		}); err != nil {
			return fmt.Errorf("recover stale extraction job: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit stale extraction recovery: %w", err)
	}
	return nil
}
