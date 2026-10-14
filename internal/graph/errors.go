package graph

import (
	"errors"

	"github.com/walnuts1018/horse/internal/application/apperror"
	purchaseapp "github.com/walnuts1018/horse/internal/application/purchase"
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
	case errors.Is(err, purchaseapp.ErrBadInput):
		return apperror.KindBadInput, true
	case errors.Is(err, purchaseapp.ErrConflict):
		return apperror.KindConflict, true
	case errors.Is(err, purchaseapp.ErrNotFound):
		return apperror.KindNotFound, true
	default:
		return apperror.KindUnknown, false
	}
}
