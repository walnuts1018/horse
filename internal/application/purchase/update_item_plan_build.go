package purchase

import (
	"fmt"
	"strings"

	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type purchaseItemUpdateValues struct {
	name            string
	amount          int64
	categoryID      *string
	variantID       *string
	productLearning itemProductLearningPlan
}

type purchaseItemUpdateCalculation struct {
	measure            purchaseItemUpdateMeasure
	allocationOverride bool
	requestedWeights   []domain.Weight
}

func preparePurchaseItemUpdateValues(command UpdateItemCommand, item *models.PurchaseItem) (purchaseItemUpdateValues, error) {
	values := purchaseItemUpdateValues{
		name:       item.Name,
		amount:     item.AmountYen,
		categoryID: nullableID(item.CategoryID),
		variantID:  nullableID(item.ProductVariantID),
	}
	if command.Name != nil {
		values.name = strings.TrimSpace(*command.Name)
		if values.name == "" {
			return purchaseItemUpdateValues{}, fmt.Errorf("%w: invalid item name", ErrBadInput)
		}
	}
	if command.AmountYen != nil {
		values.amount = *command.AmountYen
		if values.amount < 0 || values.amount > maxMoney {
			return purchaseItemUpdateValues{}, fmt.Errorf("%w: invalid item amount", ErrBadInput)
		}
	}
	if command.CategoryIDSet {
		values.categoryID = command.CategoryID
	}
	if command.ProductVariantIDSet {
		values.variantID = command.ProductVariantID
	}
	productLearning, err := prepareItemProductLearning(command, values.variantID)
	if err != nil {
		return purchaseItemUpdateValues{}, err
	}
	values.productLearning = productLearning
	return values, nil
}

func preparePurchaseItemUpdateCalculation(command UpdateItemCommand, item *models.PurchaseItem) (purchaseItemUpdateCalculation, error) {
	measure, err := measureFromItemUpdate(command, item)
	if err != nil {
		return purchaseItemUpdateCalculation{}, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	calculation := purchaseItemUpdateCalculation{
		measure: measure, allocationOverride: item.AllocationOverride || command.AllocationsSet,
	}
	if command.AllocationsSet {
		calculation.requestedWeights = make([]domain.Weight, 0, len(command.Allocations))
		for _, allocation := range command.Allocations {
			calculation.requestedWeights = append(calculation.requestedWeights, domain.Weight{PoolID: allocation.PoolID, Value: allocation.Weight})
		}
	}
	return calculation, nil
}

func assemblePurchaseItemUpdatePlan(status string, deferAccountingSync bool, values purchaseItemUpdateValues, calculation purchaseItemUpdateCalculation, itemChange itemChangePlan, payments []Payment, updatePayments bool) purchaseItemUpdatePlan {
	return purchaseItemUpdatePlan{
		name: values.name, amount: values.amount, variantID: values.variantID,
		measure: calculation.measure, productLearning: values.productLearning, itemChange: itemChange,
		accounting: buildPurchaseItemAccountingPlan(status, payments, updatePayments, itemChange.allocationsChanged, deferAccountingSync),
	}
}
