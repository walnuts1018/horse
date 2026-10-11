package graph

import "errors"

var (
	ErrConflict = errors.New("resource conflict")
	ErrNotFound = errors.New("resource not found")
	ErrBadInput = errors.New("invalid input")
)
