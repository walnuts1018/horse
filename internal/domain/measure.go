package domain

import (
	"errors"
	"math/big"
	"strconv"
	"strings"
)

const (
	MaxMeasureNumerator   int64 = 1_000_000_000_000
	MaxMeasureDenominator int64 = 1_000_000
	MaxMeasurePrice       int64 = 1_000_000_000_000
)

type Rational struct {
	Numerator   int64
	Denominator int64
}

type Measure struct {
	Value Rational
	Unit  string
}

type PrintedUnitPrice struct {
	AmountYen int64
	Basis     Measure
}

func ParseRational(numerator, denominator string) (Rational, error) {
	n, err := strconv.ParseInt(numerator, 10, 64)
	if err != nil || n <= 0 || n > MaxMeasureNumerator || strconv.FormatInt(n, 10) != numerator {
		return Rational{}, errors.New("invalid rational numerator")
	}
	d, err := strconv.ParseInt(denominator, 10, 64)
	if err != nil || d <= 0 || d > MaxMeasureDenominator || strconv.FormatInt(d, 10) != denominator {
		return Rational{}, errors.New("invalid rational denominator")
	}
	if new(big.Int).GCD(nil, nil, big.NewInt(n), big.NewInt(d)).Int64() != 1 {
		return Rational{}, errors.New("rational must be reduced")
	}
	return Rational{Numerator: n, Denominator: d}, nil
}

func ValidateMeasure(quantity *Measure, printedPrice *PrintedUnitPrice) error {
	if quantity != nil && !validMeasure(*quantity) {
		return errors.New("invalid quantity")
	}
	if printedPrice != nil && (printedPrice.AmountYen < 0 || printedPrice.AmountYen > MaxMeasurePrice || !validMeasure(printedPrice.Basis)) {
		return errors.New("invalid printed unit price")
	}
	return nil
}

func validMeasure(measure Measure) bool {
	return measure.Value.Numerator > 0 && measure.Value.Numerator <= MaxMeasureNumerator &&
		measure.Value.Denominator > 0 && measure.Value.Denominator <= MaxMeasureDenominator &&
		strings.TrimSpace(measure.Unit) != "" && len(measure.Unit) <= 32
}
