package graph

import "github.com/walnuts1018/horse/internal/application/apperror"

var (
	ErrConflict = apperror.ErrConflict
	ErrNotFound = apperror.ErrNotFound
	ErrBadInput = apperror.ErrBadInput
)

func ApplicationErrorKind(err error) (apperror.Kind, bool) {
	return apperror.KindOf(err)
}
