package model

import (
	"errors"
	"io"
	"strconv"
	"time"
)

type Date string

func (d Date) MarshalGQL(w io.Writer) {
	_, _ = io.WriteString(w, strconv.Quote(string(d)))
}

func (d *Date) UnmarshalGQL(value any) error {
	text, ok := value.(string)
	if !ok {
		return errors.New("Date must be a string")
	}
	if _, err := time.Parse(time.DateOnly, text); err != nil {
		return errors.New("Date must use YYYY-MM-DD format")
	}
	*d = Date(text)
	return nil
}

type DateTime string

func (d DateTime) MarshalGQL(w io.Writer) {
	_, _ = io.WriteString(w, strconv.Quote(string(d)))
}

func (d *DateTime) UnmarshalGQL(value any) error {
	text, ok := value.(string)
	if !ok {
		return errors.New("DateTime must be a string")
	}
	if _, err := time.Parse(time.RFC3339Nano, text); err != nil {
		return errors.New("DateTime must use RFC3339 format")
	}
	*d = DateTime(text)
	return nil
}

type YearMonth string

func (ym YearMonth) MarshalGQL(w io.Writer) {
	_, _ = io.WriteString(w, strconv.Quote(string(ym)))
}

func (ym *YearMonth) UnmarshalGQL(value any) error {
	text, ok := value.(string)
	if !ok {
		return errors.New("YearMonth must be a string")
	}
	if _, err := time.Parse("2006-01", text); err != nil {
		return errors.New("YearMonth must use YYYY-MM format")
	}
	*ym = YearMonth(text)
	return nil
}
