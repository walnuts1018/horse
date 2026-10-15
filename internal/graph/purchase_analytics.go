package graph

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"uuid"

	"github.com/walnuts1018/horse/internal/graph/model"
	postgresquery "github.com/walnuts1018/horse/internal/postgres/query"
)

func (r *queryResolver) purchaseAnalytics(ctx context.Context, householdID, from, to string) ([]*model.PurchaseAnalyticsEntry, error) {
	hhID, start, end, err := monthlySpendingRange(ctx, r.DB, householdID, from, to)
	if err != nil {
		return nil, err
	}
	rows, err := postgresquery.PurchaseAnalytics(ctx, queryExecutor(r.DB), hhID, start, end)
	if err != nil {
		return nil, err
	}
	result := make([]*model.PurchaseAnalyticsEntry, 0, len(rows))
	for _, row := range rows {
		amount, err := monthlySpendingAmount(row.AmountYen)
		if err != nil {
			return nil, err
		}
		id, err := encodeAnalyticsID(row.Dimension, row.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, &model.PurchaseAnalyticsEntry{Dimension: row.Dimension, ID: id, Name: row.Name, Period: model.YearMonth(row.Period), AmountYen: graphLong(amount), Count: int(row.Count)})
	}
	return result, nil
}

func (r *queryResolver) purchasePriceObservations(ctx context.Context, householdID, from, to string, itemIDs []string) ([]*model.PurchasePriceObservation, error) {
	if len(itemIDs) > 5 {
		return nil, fmt.Errorf("%w: at most five item IDs may be requested", ErrBadInput)
	}
	hhID, start, end, err := monthlySpendingRange(ctx, r.DB, householdID, from, to)
	if err != nil {
		return nil, err
	}
	dbItemIDs := make([]string, 0, len(itemIDs))
	graphqlIDs := make(map[string]string, len(itemIDs))
	for _, itemID := range itemIDs {
		dbID, err := decodeAnalyticsItemID(itemID)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid item ID", ErrBadInput)
		}
		dbItemIDs = append(dbItemIDs, dbID)
		graphqlIDs[dbID] = itemID
	}
	rows, err := postgresquery.PurchasePriceObservations(ctx, queryExecutor(r.DB), hhID, start, end, dbItemIDs)
	if err != nil {
		return nil, err
	}
	result := make([]*model.PurchasePriceObservation, 0, len(rows))
	for _, row := range rows {
		if _, ok := graphqlIDs[row.ItemID]; !ok {
			itemID, err := encodeAnalyticsID("item", row.ItemID)
			if err != nil {
				return nil, err
			}
			graphqlIDs[row.ItemID] = itemID
		}
		measure := purchaseItemMeasure{
			quantityNumerator: row.QuantityNumerator, quantityDenominator: row.QuantityDenominator, quantityUnit: row.QuantityUnit,
			printedUnitPriceYen: row.PrintedUnitPriceYen, printedUnitPriceQuantityNumerator: row.PrintedUnitPriceQuantityNumerator,
			printedUnitPriceQuantityDenominator: row.PrintedUnitPriceQuantityDenominator, printedUnitPriceUnit: row.PrintedUnitPriceUnit,
		}
		effectiveNumerator, effectiveDenominator := effectiveUnitPrice(row.AmountYen, measure)
		observation := &model.PurchasePriceObservation{ItemID: graphqlIDs[row.ItemID], OccurredAt: model.DateTime(timeString(row.OccurredAt)), AmountYen: graphLong(row.AmountYen)}
		observation.EffectiveUnitPriceNumeratorYen, observation.EffectiveUnitPriceDenominator = effectiveNumerator, effectiveDenominator
		if row.PrintedUnitPriceYen.Valid {
			value := graphLong(row.PrintedUnitPriceYen.V)
			observation.PrintedUnitPriceYen = &value
		}
		if row.PrintedUnitPriceQuantityNumerator.Valid {
			value := strconv.FormatInt(row.PrintedUnitPriceQuantityNumerator.V, 10)
			observation.PrintedUnitPriceQuantityNumerator = &value
		}
		if row.PrintedUnitPriceQuantityDenominator.Valid {
			value := strconv.FormatInt(row.PrintedUnitPriceQuantityDenominator.V, 10)
			observation.PrintedUnitPriceQuantityDenominator = &value
		}
		if row.PrintedUnitPriceUnit.Valid {
			value := row.PrintedUnitPriceUnit.V
			observation.PrintedUnitPriceUnit = &value
		}
		result = append(result, observation)
	}
	return result, nil
}

func encodeAnalyticsID(dimension, raw string) (string, error) {
	switch dimension {
	case "pool":
		return encodeNodeID(nodeTypePool, raw)
	case "account":
		return encodeNodeID(nodeTypeAccount, raw)
	case "item":
		base, unit, found := strings.Cut(raw, "\x1f")
		if found {
			if _, err := uuid.Parse(base); err == nil {
				base, err = encodeNodeID(nodeTypeProductVariant, base)
				if err != nil {
					return "", err
				}
			}
			return base + "\x00" + unit, nil
		}
	}
	return raw, nil
}

func decodeAnalyticsItemID(value string) (string, error) {
	base, unit, found := strings.Cut(value, "\x00")
	if !found {
		return "", fmt.Errorf("item ID has no unit separator")
	}
	if raw, err := decodeNodeID(base, nodeTypeProductVariant); err == nil {
		base = raw
	}
	return base + "\x1f" + unit, nil
}
