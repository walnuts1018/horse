package graph

import (
	"encoding/base64"
	"fmt"
)

func pageDirection(after, before *string, first, last *int) (int, bool, error) {
	if after != nil && *after != "" && before != nil && *before != "" {
		return 0, false, fmt.Errorf("%w: after and before cannot be used together", ErrBadInput)
	}
	if first != nil && last != nil {
		return 0, false, fmt.Errorf("%w: first and last cannot be used together", ErrBadInput)
	}
	backward := last != nil
	pageSize := 50
	if backward {
		pageSize = *last
	} else if first != nil {
		pageSize = *first
	}
	if pageSize < 1 || pageSize > 100 {
		return 0, false, fmt.Errorf("%w: page size must be between 1 and 100", ErrBadInput)
	}
	return pageSize, backward, nil
}

func encodeCursor(kind, value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(kind + ":" + value))
}

func decodeCursor(cursor, kind string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", fmt.Errorf("%w: invalid cursor", ErrBadInput)
	}
	value := string(decoded)
	prefix := kind + ":"
	if len(value) <= len(prefix) || value[:len(prefix)] != prefix {
		return "", fmt.Errorf("%w: expected %s cursor", ErrBadInput, kind)
	}
	return value[len(prefix):], nil
}

func cursorID(cursor *string, kind string) (*string, error) {
	if cursor == nil || *cursor == "" {
		return nil, nil
	}
	value, err := decodeCursor(*cursor, kind)
	if err != nil {
		return nil, err
	}
	return &value, nil
}
