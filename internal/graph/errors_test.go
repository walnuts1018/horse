package graph

import (
	"errors"
	"fmt"
	"testing"

	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/application/funding"
	purchaseapp "github.com/walnuts1018/horse/internal/application/purchase"
	"github.com/walnuts1018/horse/internal/application/settlement"
	"github.com/walnuts1018/horse/internal/application/transfer"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func TestApplicationErrorKind(t *testing.T) {
	t.Parallel()

	rootCause := errors.New("invalid account")
	wrapped := fmt.Errorf("record funding: %w", apperror.Wrap(apperror.KindBadInput, rootCause))

	kind, ok := ApplicationErrorKind(wrapped)
	if !ok || kind != apperror.KindBadInput {
		t.Fatalf("ApplicationErrorKind() = (%v, %t), want (%v, true)", kind, ok, apperror.KindBadInput)
	}
	if !errors.Is(wrapped, rootCause) {
		t.Fatal("wrapped application error does not preserve its cause")
	}
	if !errors.Is(fmt.Errorf("wrapped: %w", ErrBadInput), apperror.ErrBadInput) {
		t.Fatal("GraphQL bad-input sentinel does not match the shared application sentinel")
	}

	tests := []struct {
		name string
		err  error
		want apperror.Kind
	}{
		{name: "purchase", err: fmt.Errorf("purchase: %w", purchaseapp.ErrConflict), want: apperror.KindConflict},
		{name: "funding", err: fmt.Errorf("funding: %w", funding.ErrNotFound), want: apperror.KindNotFound},
		{name: "transfer", err: fmt.Errorf("transfer: %w", transfer.ErrBadInput), want: apperror.KindBadInput},
		{name: "settlement", err: fmt.Errorf("settlement: %w", settlement.ErrConflict), want: apperror.KindConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			kind, ok := ApplicationErrorKind(test.err)
			if !ok || kind != test.want {
				t.Fatalf("ApplicationErrorKind() = (%v, %t), want (%v, true)", kind, ok, test.want)
			}
		})
	}
}

func TestMutationErrorResultUsesSharedClassification(t *testing.T) {
	t.Parallel()

	got, ok := mutationErrorResult(fmt.Errorf("service: %w", apperror.Wrap(apperror.KindNotFound, errors.New("missing"))))
	if !ok || got.Code != model.MutationErrorCodeNotFound {
		t.Fatalf("mutationErrorResult() = (%+v, %t), want NOT_FOUND", got, ok)
	}
}
