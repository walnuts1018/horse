package graph

import (
	"database/sql"
	"errors"
	"math/big"
	"strings"

	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/graph/model"
)

type purchaseItemMeasure struct {
	quantityNumerator                   sql.Null[int64]
	quantityDenominator                 sql.Null[int64]
	quantityUnit                        sql.Null[string]
	printedUnitPriceYen                 sql.Null[int64]
	printedUnitPriceQuantityNumerator   sql.Null[int64]
	printedUnitPriceQuantityDenominator sql.Null[int64]
	printedUnitPriceUnit                sql.Null[string]
}

func newPurchaseItemMeasure(quantityNumerator, quantityDenominator, quantityUnit *string,
	printedPrice *model.Long, printedBasisNumerator, printedBasisDenominator, printedUnit *string,
) (purchaseItemMeasure, error) {
	measure := purchaseItemMeasure{
		quantityUnit: nullString(quantityUnit), printedUnitPriceYen: nullLong(printedPrice), printedUnitPriceUnit: nullString(printedUnit),
	}
	numerator, denominator, err := parseMeasureRational(quantityNumerator, quantityDenominator)
	if err != nil {
		return purchaseItemMeasure{}, err
	}
	measure.quantityNumerator, measure.quantityDenominator = numerator, denominator
	if anyValid(measure.quantityNumerator.Valid, measure.quantityDenominator.Valid, measure.quantityUnit.Valid) &&
		(!measure.quantityNumerator.Valid || !measure.quantityDenominator.Valid || !measure.quantityUnit.Valid) {
		return purchaseItemMeasure{}, errors.New("quantity fields must be provided together")
	}
	if measure.quantityNumerator.Valid {
		quantity := domain.Measure{
			Value: domain.Rational{Numerator: measure.quantityNumerator.V, Denominator: measure.quantityDenominator.V},
			Unit:  measure.quantityUnit.V,
		}
		if err := domain.ValidateMeasure(&quantity, nil); err != nil {
			return purchaseItemMeasure{}, err
		}
	}
	measure.printedUnitPriceQuantityNumerator, measure.printedUnitPriceQuantityDenominator, err = parseMeasureRational(printedBasisNumerator, printedBasisDenominator)
	if err != nil {
		return purchaseItemMeasure{}, err
	}
	if anyValid(measure.printedUnitPriceYen.Valid, measure.printedUnitPriceQuantityNumerator.Valid, measure.printedUnitPriceQuantityDenominator.Valid, measure.printedUnitPriceUnit.Valid) &&
		(!measure.printedUnitPriceYen.Valid || !measure.printedUnitPriceQuantityNumerator.Valid || !measure.printedUnitPriceQuantityDenominator.Valid || !measure.printedUnitPriceUnit.Valid) {
		return purchaseItemMeasure{}, errors.New("printed unit price fields must be provided together")
	}
	if measure.printedUnitPriceYen.Valid {
		price := domain.PrintedUnitPrice{
			AmountYen: measure.printedUnitPriceYen.V,
			Basis: domain.Measure{
				Value: domain.Rational{Numerator: measure.printedUnitPriceQuantityNumerator.V, Denominator: measure.printedUnitPriceQuantityDenominator.V},
				Unit:  measure.printedUnitPriceUnit.V,
			},
		}
		if err := domain.ValidateMeasure(nil, &price); err != nil {
			return purchaseItemMeasure{}, err
		}
	}
	return measure, nil
}

func effectiveUnitPrice(amount int64, measure purchaseItemMeasure) (*string, *string) {
	if !measure.quantityNumerator.Valid || amount < 0 {
		return nil, nil
	}
	numerator := new(big.Int).Mul(big.NewInt(amount), big.NewInt(measure.quantityDenominator.V))
	denominator := big.NewInt(measure.quantityNumerator.V)
	gcd := new(big.Int).GCD(nil, nil, numerator, denominator)
	numerator.Quo(numerator, gcd)
	denominator.Quo(denominator, gcd)
	unitPriceNumerator, unitPriceDenominator := numerator.String(), denominator.String()
	return &unitPriceNumerator, &unitPriceDenominator
}

func parseMeasureRational(numerator, denominator *string) (sql.Null[int64], sql.Null[int64], error) {
	if numerator == nil && denominator == nil {
		return sql.Null[int64]{}, sql.Null[int64]{}, nil
	}
	if numerator == nil || denominator == nil {
		return sql.Null[int64]{}, sql.Null[int64]{}, errors.New("rational numerator and denominator must be provided together")
	}
	rational, err := domain.ParseRational(*numerator, *denominator)
	if err != nil {
		return sql.Null[int64]{}, sql.Null[int64]{}, err
	}
	return sql.Null[int64]{V: rational.Numerator, Valid: true}, sql.Null[int64]{V: rational.Denominator, Valid: true}, nil
}

func anyValid(values ...bool) bool {
	for _, value := range values {
		if value {
			return true
		}
	}
	return false
}

func nullLong(value *model.Long) sql.Null[int64] {
	if value == nil {
		return sql.Null[int64]{}
	}
	return sql.Null[int64]{V: int64(*value), Valid: true}
}

func nullString(value *string) sql.Null[string] {
	if value == nil {
		return sql.Null[string]{}
	}
	trimmed := strings.TrimSpace(*value)
	return sql.Null[string]{V: trimmed, Valid: true}
}
