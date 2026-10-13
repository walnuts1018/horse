package apperror

import "errors"

type Kind uint8

const (
	KindUnknown Kind = iota
	KindBadInput
	KindConflict
	KindNotFound
)

var (
	ErrBadInput = &Error{Kind: KindBadInput, message: "invalid input"}
	ErrConflict = &Error{Kind: KindConflict, message: "resource conflict"}
	ErrNotFound = &Error{Kind: KindNotFound, message: "resource not found"}
)

type Error struct {
	Kind    Kind
	Cause   error
	message string
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return e.message
}

func (e *Error) Unwrap() error {
	return e.Cause
}

func (e *Error) Is(target error) bool {
	targetError, ok := target.(*Error)
	return ok && targetError.Kind == e.Kind
}

func Wrap(kind Kind, cause error) error {
	if cause == nil {
		return nil
	}
	return &Error{Kind: kind, Cause: cause}
}

func KindOf(err error) (Kind, bool) {
	var applicationError *Error
	if !errors.As(err, &applicationError) || applicationError.Kind == KindUnknown {
		return KindUnknown, false
	}
	return applicationError.Kind, true
}
