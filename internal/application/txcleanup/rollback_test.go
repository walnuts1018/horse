package txcleanup

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stephenafamo/bob"
)

type rollbackTransaction struct {
	bob.Transaction
	ctxErr error
	err    error
}

func (tx *rollbackTransaction) Rollback(ctx context.Context) error {
	tx.ctxErr = ctx.Err()
	return tx.err
}

func TestRollbackUsesUncancelledContextAndLogsFailure(t *testing.T) {
	rollbackErr := errors.New("rollback failed")
	tx := &rollbackTransaction{err: rollbackErr}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	Rollback(ctx, tx)

	if tx.ctxErr != nil {
		t.Fatalf("rollback context error = %v, want nil", tx.ctxErr)
	}
	if !strings.Contains(output.String(), rollbackErr.Error()) {
		t.Fatalf("rollback error was not logged: %q", output.String())
	}
}
