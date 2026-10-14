package txcleanup

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

func Rollback(ctx context.Context, tx interface{ Rollback(context.Context) error }) {
	if err := tx.Rollback(context.WithoutCancel(ctx)); err != nil {
		if errors.Is(err, pgx.ErrTxClosed) || errors.Is(err, sql.ErrTxDone) {
			return
		}
		slog.Default().ErrorContext(ctx, "failed to roll back transaction", "error", err)
	}
}
