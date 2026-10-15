package purchase

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/inputvalidation"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type purchaseItemUpdateState struct {
	householdID gofrsuuid.UUID
	household   *models.Household
	purchase    *models.Purchase
	item        *models.PurchaseItem
	status      string
}

type purchaseItemUpdatePlan struct {
	name            string
	amount          int64
	variantID       *string
	measure         purchaseItemUpdateMeasure
	productLearning itemProductLearningPlan
	itemChange      itemChangePlan
	accounting      purchaseItemAccountingPlan
}

func validateUpdateItemCommand(command UpdateItemCommand) error {
	if err := inputvalidation.Check(command); err != nil {
		return err
	}
	if len(command.ChangedFields) == 0 {
		return fmt.Errorf("%w: patch must contain a field", ErrBadInput)
	}
	seenFields := make(map[string]struct{}, len(command.ChangedFields))
	for _, field := range command.ChangedFields {
		if !slices.Contains(updateItemFields[:], field) {
			return fmt.Errorf("%w: invalid changed field", ErrBadInput)
		}
		if _, exists := seenFields[field]; exists {
			return fmt.Errorf("%w: duplicate changed field", ErrBadInput)
		}
		seenFields[field] = struct{}{}
		if !switchItemChangedField(command, field) {
			return fmt.Errorf("%w: changed field has no value", ErrBadInput)
		}
	}
	return nil
}

var updateItemFields = [...]string{
	itemFieldName, itemFieldAmountYen, itemFieldQuantity, itemFieldPrintedUnitPrice,
	itemFieldCategoryID, itemFieldProductVariantID, itemFieldAllocations,
}

func switchItemChangedField(command UpdateItemCommand, field string) bool {
	switch field {
	case itemFieldName:
		return command.Name != nil
	case itemFieldAmountYen:
		return command.AmountYen != nil
	case itemFieldQuantity:
		return command.QuantitySet
	case itemFieldPrintedUnitPrice:
		return command.PrintedUnitPriceSet
	case itemFieldCategoryID:
		return command.CategoryIDSet
	case itemFieldProductVariantID:
		return command.ProductVariantIDSet
	case itemFieldAllocations:
		return command.AllocationsSet
	default:
		return false
	}
}

func loadPurchaseItemUpdateState(ctx context.Context, executor bob.Executor, itemUUID, actorUUID gofrsuuid.UUID, command UpdateItemCommand) (purchaseItemUpdateState, error) {
	observedItem, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemUUID)))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return purchaseItemUpdateState{}, ErrNotFound
	}
	if err != nil {
		return purchaseItemUpdateState{}, err
	}
	observedPurchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(observedItem.PurchaseID)))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return purchaseItemUpdateState{}, ErrNotFound
	}
	if err != nil {
		return purchaseItemUpdateState{}, err
	}
	householdID := observedItem.HouseholdID
	if err := requirePurchaseMember(ctx, executor, householdID, actorUUID); err != nil {
		return purchaseItemUpdateState{}, err
	}
	observedPeriod := accounting.PeriodFor(observedPurchase.OccurredAt)
	if err := lockPurchaseAccountingPeriods(ctx, executor, householdID, observedPeriod); err != nil {
		return purchaseItemUpdateState{}, err
	}
	household, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForShare()).One(ctx, executor)
	if err != nil {
		return purchaseItemUpdateState{}, err
	}
	purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(observedItem.PurchaseID))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return purchaseItemUpdateState{}, ErrNotFound
	}
	if err != nil {
		return purchaseItemUpdateState{}, err
	}
	item, err := models.PurchaseItems.Query(sm.Where(psql.And(
		models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemUUID)),
		models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchase.ID)),
	)), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return purchaseItemUpdateState{}, ErrNotFound
	}
	if err != nil {
		return purchaseItemUpdateState{}, err
	}
	if !purchase.OccurredAt.Equal(observedPurchase.OccurredAt) || purchase.HouseholdID != householdID {
		return purchaseItemUpdateState{}, ErrConflict
	}
	status := purchase.Status
	if status != draftStatus && status != postedStatus {
		return purchaseItemUpdateState{}, fmt.Errorf("%w: closed purchases require a correction", ErrConflict)
	}
	if command.BaseRevision > item.Revision {
		return purchaseItemUpdateState{}, ErrConflict
	}
	conflict, err := purchaseUpdateValuesConflict(item.FieldRevisions.Val, command.BaseRevision, command.ChangedFields)
	if err != nil {
		return purchaseItemUpdateState{}, err
	}
	if conflict {
		return purchaseItemUpdateState{}, ErrConflict
	}
	return purchaseItemUpdateState{householdID: householdID, household: household, purchase: purchase, item: item, status: status}, nil
}

func buildPurchaseItemUpdatePlan(ctx context.Context, tx pgx.Tx, executor bob.Executor, command UpdateItemCommand, state purchaseItemUpdateState, deferAccountingSync bool) (purchaseItemUpdatePlan, error) {
	item, purchase := state.item, state.purchase
	newName, newAmount := item.Name, item.AmountYen
	if command.Name != nil {
		newName = strings.TrimSpace(*command.Name)
		if newName == "" {
			return purchaseItemUpdatePlan{}, fmt.Errorf("%w: invalid item name", ErrBadInput)
		}
	}
	if command.AmountYen != nil {
		newAmount = *command.AmountYen
		if newAmount < 0 || newAmount > maxMoney {
			return purchaseItemUpdatePlan{}, fmt.Errorf("%w: invalid item amount", ErrBadInput)
		}
	}
	categoryID, variantID := nullableID(item.CategoryID), nullableID(item.ProductVariantID)
	if command.CategoryIDSet {
		categoryID = command.CategoryID
	}
	if command.ProductVariantIDSet {
		variantID = command.ProductVariantID
	}
	productLearning, err := prepareItemProductLearning(command, variantID)
	if err != nil {
		return purchaseItemUpdatePlan{}, err
	}
	if command.ProductVariantIDSet && variantID != nil {
		variantUUID, parseErr := parseUUID(*variantID, "product variant")
		if parseErr != nil {
			return purchaseItemUpdatePlan{}, ErrNotFound
		}
		variant, queryErr := models.ProductVariants.Query(sm.Where(psql.And(
			models.ProductVariants.Columns.ID.EQ(psql.Arg(variantUUID)),
			models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(state.householdID)),
			models.ProductVariants.Columns.Archived.EQ(psql.Arg(false)),
		))).One(ctx, executor)
		if errors.Is(queryErr, sql.ErrNoRows) {
			return purchaseItemUpdatePlan{}, ErrNotFound
		}
		if queryErr != nil {
			return purchaseItemUpdatePlan{}, queryErr
		}
		concept, queryErr := models.ProductConcepts.Query(sm.Where(psql.And(
			models.ProductConcepts.Columns.ID.EQ(psql.Arg(variant.ConceptID)),
			models.ProductConcepts.Columns.HouseholdID.EQ(psql.Arg(state.householdID)),
		))).One(ctx, executor)
		if errors.Is(queryErr, sql.ErrNoRows) || queryErr == nil && concept.Archived {
			return purchaseItemUpdatePlan{}, ErrNotFound
		}
		if queryErr != nil {
			return purchaseItemUpdatePlan{}, queryErr
		}
	}
	measure, err := measureFromItemUpdate(command, item)
	if err != nil {
		return purchaseItemUpdatePlan{}, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	allocationOverride := item.AllocationOverride || command.AllocationsSet
	var requestedWeights []domain.Weight
	if command.AllocationsSet {
		requestedWeights = make([]domain.Weight, 0, len(command.Allocations))
		for _, allocation := range command.Allocations {
			requestedWeights = append(requestedWeights, domain.Weight{PoolID: allocation.PoolID, Value: allocation.Weight})
		}
	}
	payments, updatePayments, err := adjustedPurchasePayments(ctx, executor, purchase, item.AmountYen, newAmount)
	if err != nil {
		return purchaseItemUpdatePlan{}, err
	}
	categoryValue, err := nullableUUID(categoryID)
	if err != nil {
		return purchaseItemUpdatePlan{}, ErrNotFound
	}
	variantValue, err := nullableUUID(variantID)
	if err != nil {
		return purchaseItemUpdatePlan{}, ErrNotFound
	}
	itemPlan, err := buildItemChangePlan(ctx, tx, executor, item, purchase, itemChangeInput{
		fields: command.ChangedFields, amount: newAmount, categoryID: categoryValue, productVariantID: variantValue,
		allocationOverride: allocationOverride, categoryOrVariantChanged: command.CategoryIDSet || command.ProductVariantIDSet,
		allocationsSet: command.AllocationsSet, requestedWeights: requestedWeights,
		defaultAllocationVersion: state.household.DefaultAllocationVersion,
	})
	if err != nil {
		return purchaseItemUpdatePlan{}, err
	}
	accountingPlan := buildPurchaseItemAccountingPlan(state.status, payments, updatePayments, itemPlan.allocationsChanged, deferAccountingSync)
	return purchaseItemUpdatePlan{
		name: newName, amount: newAmount, variantID: variantID,
		measure: measure, productLearning: productLearning, itemChange: itemPlan, accounting: accountingPlan,
	}, nil
}
