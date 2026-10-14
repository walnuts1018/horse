package txcleanup

import (
	"context"
	"log/slog"
)

func Rollback(ctx context.Context, tx interface{ Rollback(context.Context) error }) {
	if err := tx.Rollback(context.WithoutCancel(ctx)); err != nil {
		slog.Default().ErrorContext(ctx, "failed to roll back transaction", "error", err)
	}
}
