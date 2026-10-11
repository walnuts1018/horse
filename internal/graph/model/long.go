package model

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
)

type Long int64

func (value Long) MarshalGQL(writer io.Writer) {
	_, _ = fmt.Fprint(writer, int64(value))
}

func (value *Long) UnmarshalGQL(input any) error {
	var parsed int64
	switch number := input.(type) {
	case int:
		parsed = int64(number)
	case int8:
		parsed = int64(number)
	case int16:
		parsed = int64(number)
	case int32:
		parsed = int64(number)
	case int64:
		parsed = number
	case json.Number:
		result, err := number.Int64()
		if err != nil {
			return errorsNewInteger()
		}
		parsed = result
	case float64:
		if math.Trunc(number) != number || number > math.MaxInt64 || number < math.MinInt64 {
			return errorsNewInteger()
		}
		parsed = int64(number)
	default:
		return errorsNewInteger()
	}
	*value = Long(parsed)
	return nil
}

func (value *Long) UnmarshalJSON(data []byte) error {
	parsed, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return errorsNewInteger()
	}
	*value = Long(parsed)
	return nil
}

func errorsNewInteger() error { return fmt.Errorf("expected an integer in signed 64-bit range") }
