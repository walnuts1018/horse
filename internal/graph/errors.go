package graph

import (
	"errors"

	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/application/funding"
	purchaseapp "github.com/walnuts1018/horse/internal/application/purchase"
	"github.com/walnuts1018/horse/internal/application/settlement"
	"github.com/walnuts1018/horse/internal/application/transfer"
)

var (
	ErrConflict = apperror.ErrConflict
	ErrNotFound = apperror.ErrNotFound
	ErrBadInput = apperror.ErrBadInput
)

func ApplicationErrorKind(err error) (apperror.Kind, bool) {
	if kind, ok := apperror.KindOf(err); ok {
		return kind, true
	}
	switch {
	case errors.Is(err, purchaseapp.ErrBadInput), errors.Is(err, funding.ErrBadInput), errors.Is(err, transfer.ErrBadInput), errors.Is(err, settlement.ErrBadInput):
		return apperror.KindBadInput, true
	case errors.Is(err, purchaseapp.ErrConflict), errors.Is(err, funding.ErrConflict), errors.Is(err, transfer.ErrConflict), errors.Is(err, settlement.ErrConflict):
		return apperror.KindConflict, true
	case errors.Is(err, purchaseapp.ErrNotFound), errors.Is(err, funding.ErrNotFound), errors.Is(err, transfer.ErrNotFound), errors.Is(err, settlement.ErrNotFound):
		return apperror.KindNotFound, true
	default:
		return apperror.KindUnknown, false
	}
}
