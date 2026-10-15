package graph

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func mapPurchases(purchases models.PurchaseSlice, correctionAggregates map[gofrsuuid.UUID]purchaseCorrectionAggregate, itemCorrectionAggregates map[gofrsuuid.UUID]int64, fields purchaseLoadFields) ([]*model.Purchase, error) {
	result := make([]*model.Purchase, 0, len(purchases))
	for _, purchase := range purchases {
		loaded := &model.Purchase{
			ID: purchase.ID.String(), HouseholdID: purchase.HouseholdID.String(),
			OccurredAt: timeString(purchase.OccurredAt), DatePrecision: model.PurchaseDatePrecision(purchase.DatePrecision), Merchant: purchase.Merchant,
			Origin: model.PurchaseOrigin(purchase.Origin), SourceKinds: make([]model.SourceKind, 0),
			Revision: int(purchase.Revision), Status: model.PurchaseStatus(purchase.Status), Currency: currencyJPY,
			CreatedAt: timeString(purchase.CreatedAt), UpdatedAt: timeString(purchase.UpdatedAt),
			Items: make([]*model.PurchaseItem, 0), PaymentAllocations: make([]*model.PurchasePayment, 0),
			CorrectionsConnection: &model.PurchaseCorrectionConnection{
				Edges: make([]*model.PurchaseCorrectionEdge, 0), PageInfo: &model.PageInfo{},
			},
		}
		sourceKinds := make(map[model.SourceKind]struct{}, len(purchase.R.EvidenceLinks)+len(purchase.R.DocumentConfirmations))
		for _, link := range purchase.R.EvidenceLinks {
			if link.R.SourceDocument != nil {
				sourceKinds[model.SourceKind(strings.ToUpper(link.R.SourceDocument.SourceKind))] = struct{}{}
			}
		}
		for _, confirmation := range purchase.R.DocumentConfirmations {
			if confirmation.R.SourceDocument != nil {
				sourceKinds[model.SourceKind(strings.ToUpper(confirmation.R.SourceDocument.SourceKind))] = struct{}{}
			}
		}
		for kind := range sourceKinds {
			loaded.SourceKinds = append(loaded.SourceKinds, kind)
		}
		slices.Sort(loaded.SourceKinds)
		loaded.HasEvidence = len(purchase.R.EvidenceLinks)+len(purchase.R.DocumentConfirmations) > 0

		var total int64
		for _, storedItem := range purchase.R.PurchaseItems {
			var remainingItemRefundable int64
			if fields.itemCorrections {
				var ok bool
				remainingItemRefundable, ok = checkedAdd(storedItem.AmountYen, itemCorrectionAggregates[storedItem.ID])
				if !ok {
					return nil, fmt.Errorf("purchase item %s refundable total exceeds int64", storedItem.ID)
				}
			}
			item := &model.PurchaseItem{
				ID: storedItem.ID.String(), Name: storedItem.Name, AmountYen: graphLong(storedItem.AmountYen),
				RemainingRefundableAmountYen: graphLong(remainingItemRefundable),
				Revision:                     int(storedItem.Revision), Currency: currencyJPY, Allocations: make([]*model.PurchaseItemAllocation, 0), Tags: make([]*model.Tag, 0),
			}
			measure := purchaseItemMeasure{
				quantityNumerator: storedItem.QuantityNumerator, quantityDenominator: storedItem.QuantityDenominator,
				quantityUnit: storedItem.QuantityUnit, printedUnitPriceYen: storedItem.PrintedUnitPriceYen,
				printedUnitPriceQuantityNumerator:   storedItem.PrintedUnitPriceQuantityNumerator,
				printedUnitPriceQuantityDenominator: storedItem.PrintedUnitPriceQuantityDenominator, printedUnitPriceUnit: storedItem.PrintedUnitPriceUnit,
			}
			if measure.quantityNumerator.Valid {
				item.Quantity = &model.PurchaseQuantity{Numerator: strconv.FormatInt(measure.quantityNumerator.V, 10), Denominator: strconv.FormatInt(measure.quantityDenominator.V, 10), Unit: measure.quantityUnit.V}
			}
			if measure.printedUnitPriceYen.Valid {
				item.PrintedUnitPrice = &model.PrintedUnitPrice{
					AmountYen: graphLong(measure.printedUnitPriceYen.V),
					Basis:     &model.PurchaseQuantity{Numerator: strconv.FormatInt(measure.printedUnitPriceQuantityNumerator.V, 10), Denominator: strconv.FormatInt(measure.printedUnitPriceQuantityDenominator.V, 10), Unit: measure.printedUnitPriceUnit.V},
				}
			}
			effectiveNumerator, effectiveDenominator := effectiveUnitPrice(storedItem.AmountYen, measure)
			if effectiveNumerator != nil && effectiveDenominator != nil {
				item.EffectiveUnitPrice = &model.EffectiveUnitPrice{NumeratorYen: *effectiveNumerator, Denominator: *effectiveDenominator}
			}
			if storedItem.CategoryID.Valid {
				categoryID := storedItem.CategoryID.V.String()
				item.CategoryID = &categoryID
			}
			if storedItem.ProductVariantID.Valid {
				variantID := storedItem.ProductVariantID.V.String()
				item.ProductVariantID = &variantID
			}
			if fields.total {
				nextTotal, ok := checkedAdd(total, storedItem.AmountYen)
				if !ok {
					return nil, fmt.Errorf("purchase %s total exceeds int64", purchase.ID)
				}
				total = nextTotal
			}
			for _, storedAllocation := range storedItem.R.ItemAllocations {
				if storedAllocation.R.Pool == nil {
					return nil, fmt.Errorf("item allocation for item %s has no pool", storedItem.ID)
				}
				item.Allocations = append(item.Allocations, &model.PurchaseItemAllocation{
					PoolID: storedAllocation.PoolID.String(), PoolName: storedAllocation.R.Pool.Name,
					Weight: graphLong(storedAllocation.Weight), AmountYen: graphLong(storedAllocation.AmountYen),
				})
			}
			for _, tagLink := range storedItem.R.PurchaseItemTags {
				if tagLink.R.Tag == nil {
					return nil, fmt.Errorf("purchase item tag %s has no tag", tagLink.TagID)
				}
				item.Tags = append(item.Tags, tagModel(tagLink.R.Tag))
			}
			loaded.Items = append(loaded.Items, item)
		}
		if fields.total {
			loaded.TotalYen = graphLong(total)
		}
		if fields.purchaseCorrections {
			aggregate := correctionAggregates[purchase.ID]
			loaded.CorrectionAmountYen = graphLong(aggregate.AmountYen)
			loaded.CorrectionCount = int(aggregate.Count)
			if fields.remainingRefundable {
				remainingRefundable, ok := checkedAdd(total, aggregate.AmountYen)
				if !ok {
					return nil, fmt.Errorf("purchase %s refundable total exceeds int64", purchase.ID)
				}
				loaded.RemainingRefundableAmountYen = graphLong(remainingRefundable)
			}
		}
		for _, storedPayment := range purchase.R.PurchasePayments {
			loaded.PaymentAllocations = append(loaded.PaymentAllocations, &model.PurchasePayment{
				PaymentAccountID: storedPayment.AccountID.String(), AmountYen: graphLong(storedPayment.AmountYen),
				Account: graphAccount(storedPayment.R.Account),
			})
		}
		if len(loaded.PaymentAllocations) == 0 && purchase.PaymentAccountID.Valid {
			loaded.PaymentAllocations = append(loaded.PaymentAllocations, &model.PurchasePayment{
				PaymentAccountID: purchase.PaymentAccountID.V.String(), Account: graphAccount(purchase.R.Account), AmountYen: graphLong(total),
			})
		}
		result = append(result, loaded)
	}
	return result, nil
}
