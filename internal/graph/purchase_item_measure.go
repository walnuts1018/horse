package graph

import (
	"database/sql"
	"errors"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/walnuts1018/horse/internal/graph/model"
)

const maxQuantityNumerator int64 = 1_000_000_000_000
const maxQuantityDenominator int64 = 1_000_000

type purchaseItemMeasure struct {
	quantityNumerator                   sql.Null[int64]
	quantityDenominator                 sql.Null[int64]
	quantityUnit                        sql.Null[string]
	printedUnitPriceYen                 sql.Null[int64]
	printedUnitPriceQuantityNumerator   sql.Null[int64]
	printedUnitPriceQuantityDenominator sql.Null[int64]
	printedUnitPriceUnit                sql.Null[string]
}

func measureFromInput(input *model.PurchaseItemInput) (purchaseItemMeasure, error) {
	var quantityNumerator, quantityDenominator, quantityUnit *string
	if input.Quantity != nil {
		quantityNumerator, quantityDenominator, quantityUnit = &input.Quantity.Numerator, &input.Quantity.Denominator, &input.Quantity.Unit
	}
	var printedPrice *model.Long
	var printedBasisNumerator, printedBasisDenominator, printedUnit *string
	if input.PrintedUnitPrice != nil {
		if input.PrintedUnitPrice.Basis == nil {
			return purchaseItemMeasure{}, errors.New("printed unit price basis is required")
		}
		printedPrice = &input.PrintedUnitPrice.AmountYen
		printedBasisNumerator = &input.PrintedUnitPrice.Basis.Numerator
		printedBasisDenominator = &input.PrintedUnitPrice.Basis.Denominator
		printedUnit = &input.PrintedUnitPrice.Basis.Unit
	}
	return newPurchaseItemMeasure(quantityNumerator, quantityDenominator, quantityUnit, printedPrice, printedBasisNumerator, printedBasisDenominator, printedUnit)
}

func measureFromUpdate(input model.UpdatePurchaseItemInput, current purchaseItemMeasure) (purchaseItemMeasure, error) {
	updated := current
	if containsChangedField(input.ChangedFields, "quantity") {
		if !input.Quantity.IsSet() {
			return purchaseItemMeasure{}, errors.New("changed quantity has no value")
		}
		quantity := input.Quantity.Value()
		var numeratorValue, denominatorValue, unitValue *string
		if quantity != nil {
			numeratorValue, denominatorValue, unitValue = &quantity.Numerator, &quantity.Denominator, &quantity.Unit
		}
		numerator, denominator, err := parseMeasureRational(numeratorValue, denominatorValue)
		if err != nil {
			return purchaseItemMeasure{}, err
		}
		updated.quantityNumerator, updated.quantityDenominator = numerator, denominator
		updated.quantityUnit = nullString(unitValue)
	} else if input.Quantity.IsSet() {
		return purchaseItemMeasure{}, errors.New("quantity is missing from changedFields")
	}
	if containsChangedField(input.ChangedFields, "printedUnitPrice") {
		if !input.PrintedUnitPrice.IsSet() {
			return purchaseItemMeasure{}, errors.New("changed printed unit price has no value")
		}
		printedUnitPrice := input.PrintedUnitPrice.Value()
		var price *model.Long
		var numeratorInput, denominatorInput, unitInput *string
		if printedUnitPrice != nil {
			if printedUnitPrice.Basis == nil {
				return purchaseItemMeasure{}, errors.New("printed unit price basis is required")
			}
			price = &printedUnitPrice.AmountYen
			numeratorInput = &printedUnitPrice.Basis.Numerator
			denominatorInput = &printedUnitPrice.Basis.Denominator
			unitInput = &printedUnitPrice.Basis.Unit
		}
		updated.printedUnitPriceYen = nullLong(price)
		numerator, denominator, err := parseMeasureRational(numeratorInput, denominatorInput)
		if err != nil {
			return purchaseItemMeasure{}, err
		}
		updated.printedUnitPriceQuantityNumerator, updated.printedUnitPriceQuantityDenominator = numerator, denominator
		updated.printedUnitPriceUnit = nullString(unitInput)
	} else if input.PrintedUnitPrice.IsSet() {
		return purchaseItemMeasure{}, errors.New("printed unit price is missing from changedFields")
	}
	if _, err := newPurchaseItemMeasure(measureIntegerString(updated.quantityNumerator), measureIntegerString(updated.quantityDenominator), measureString(updated.quantityUnit),
		measureLong(updated.printedUnitPriceYen), measureIntegerString(updated.printedUnitPriceQuantityNumerator),
		measureIntegerString(updated.printedUnitPriceQuantityDenominator), measureString(updated.printedUnitPriceUnit)); err != nil {
		return purchaseItemMeasure{}, err
	}
	return updated, nil
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
	if measure.quantityNumerator.Valid && (measure.quantityNumerator.V <= 0 || measure.quantityNumerator.V > maxQuantityNumerator ||
		measure.quantityDenominator.V <= 0 || measure.quantityDenominator.V > maxQuantityDenominator || !validMeasureUnit(measure.quantityUnit.V)) {
		return purchaseItemMeasure{}, errors.New("invalid quantity")
	}
	measure.printedUnitPriceQuantityNumerator, measure.printedUnitPriceQuantityDenominator, err = parseMeasureRational(printedBasisNumerator, printedBasisDenominator)
	if err != nil {
		return purchaseItemMeasure{}, err
	}
	if anyValid(measure.printedUnitPriceYen.Valid, measure.printedUnitPriceQuantityNumerator.Valid, measure.printedUnitPriceQuantityDenominator.Valid, measure.printedUnitPriceUnit.Valid) &&
		(!measure.printedUnitPriceYen.Valid || !measure.printedUnitPriceQuantityNumerator.Valid || !measure.printedUnitPriceQuantityDenominator.Valid || !measure.printedUnitPriceUnit.Valid) {
		return purchaseItemMeasure{}, errors.New("printed unit price fields must be provided together")
	}
	if measure.printedUnitPriceYen.Valid && (measure.printedUnitPriceYen.V < 0 || measure.printedUnitPriceYen.V > maxMoney ||
		measure.printedUnitPriceQuantityNumerator.V <= 0 || measure.printedUnitPriceQuantityNumerator.V > maxQuantityNumerator ||
		measure.printedUnitPriceQuantityDenominator.V <= 0 || measure.printedUnitPriceQuantityDenominator.V > maxQuantityDenominator ||
		!validMeasureUnit(measure.printedUnitPriceUnit.V)) {
		return purchaseItemMeasure{}, errors.New("invalid printed unit price")
	}
	return measure, nil
}

func effectiveUnitPrice(amount int64, measure purchaseItemMeasure) (*string, *string, bool) {
	if !measure.quantityNumerator.Valid || amount < 0 {
		return nil, nil, true
	}
	numerator := new(big.Int).Mul(big.NewInt(amount), big.NewInt(measure.quantityDenominator.V))
	denominator := big.NewInt(measure.quantityNumerator.V)
	gcd := new(big.Int).GCD(nil, nil, numerator, denominator)
	numerator.Quo(numerator, gcd)
	denominator.Quo(denominator, gcd)
	unitPriceNumerator, unitPriceDenominator := numerator.String(), denominator.String()
	return &unitPriceNumerator, &unitPriceDenominator, true
}

func parseMeasureRational(numerator, denominator *string) (sql.Null[int64], sql.Null[int64], error) {
	if numerator == nil && denominator == nil {
		return sql.Null[int64]{}, sql.Null[int64]{}, nil
	}
	if numerator == nil || denominator == nil {
		return sql.Null[int64]{}, sql.Null[int64]{}, errors.New("rational numerator and denominator must be provided together")
	}
	numeratorValue, err := strconv.ParseInt(*numerator, 10, 64)
	if err != nil || numeratorValue <= 0 || numeratorValue > maxQuantityNumerator || strconv.FormatInt(numeratorValue, 10) != *numerator {
		return sql.Null[int64]{}, sql.Null[int64]{}, errors.New("invalid rational numerator")
	}
	denominatorValue, err := strconv.ParseInt(*denominator, 10, 64)
	if err != nil || denominatorValue <= 0 || denominatorValue > maxQuantityDenominator || strconv.FormatInt(denominatorValue, 10) != *denominator {
		return sql.Null[int64]{}, sql.Null[int64]{}, errors.New("invalid rational denominator")
	}
	gcd := new(big.Int).GCD(nil, nil, big.NewInt(numeratorValue), big.NewInt(denominatorValue))
	if gcd.Int64() != 1 {
		return sql.Null[int64]{}, sql.Null[int64]{}, errors.New("rational must be reduced")
	}
	return sql.Null[int64]{V: numeratorValue, Valid: true}, sql.Null[int64]{V: denominatorValue, Valid: true}, nil
}

func validMeasureUnit(unit string) bool {
	unit = strings.TrimSpace(unit)
	return unit != "" && len(unit) <= 32
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

func measureLong(value sql.Null[int64]) *model.Long {
	if !value.Valid {
		return nil
	}
	result := model.Long(value.V)
	return &result
}

func measureString(value sql.Null[string]) *string {
	if !value.Valid {
		return nil
	}
	return &value.V
}

func measureIntegerString(value sql.Null[int64]) *string {
	if !value.Valid {
		return nil
	}
	result := strconv.FormatInt(value.V, 10)
	return &result
}

func containsChangedField(fields []string, changed string) bool {
	return slices.Contains(fields, changed)
}
