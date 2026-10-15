package purchase

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/inputvalidation"
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
	state := purchaseItemUpdateState{householdID: householdID, household: household, purchase: purchase, item: item, status: purchase.Status}
	if err := validatePurchaseItemUpdateState(command, state); err != nil {
		return purchaseItemUpdateState{}, err
	}
	return state, nil
}

func validatePurchaseItemUpdateState(command UpdateItemCommand, state purchaseItemUpdateState) error {
	if state.status != draftStatus && state.status != postedStatus {
		return fmt.Errorf("%w: closed purchases require a correction", ErrConflict)
	}
	if command.BaseRevision > state.item.Revision {
		return ErrConflict
	}
	conflict, err := purchaseUpdateValuesConflict(state.item.FieldRevisions.Val, command.BaseRevision, command.ChangedFields)
	if err != nil {
		return err
	}
	if conflict {
		return ErrConflict
	}
	return nil
}

func validatePurchaseItemUpdateVariant(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, command UpdateItemCommand, variantID *string) error {
	if !command.ProductVariantIDSet || variantID == nil {
		return nil
	}
	variantUUID, err := parseUUID(*variantID, "product variant")
	if err != nil {
		return ErrNotFound
	}
	variant, err := models.ProductVariants.Query(sm.Where(psql.And(
		models.ProductVariants.Columns.ID.EQ(psql.Arg(variantUUID)),
		models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.ProductVariants.Columns.Archived.EQ(psql.Arg(false)),
	))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	concept, err := models.ProductConcepts.Query(sm.Where(psql.And(
		models.ProductConcepts.Columns.ID.EQ(psql.Arg(variant.ConceptID)),
		models.ProductConcepts.Columns.HouseholdID.EQ(psql.Arg(householdID)),
	))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) || err == nil && concept.Archived {
		return ErrNotFound
	}
	return err
}

func buildPurchaseItemUpdatePlan(ctx context.Context, tx pgx.Tx, executor bob.Executor, command UpdateItemCommand, state purchaseItemUpdateState, deferAccountingSync bool) (purchaseItemUpdatePlan, error) {
	item, purchase := state.item, state.purchase
	values, err := preparePurchaseItemUpdateValues(command, item)
	if err != nil {
		return purchaseItemUpdatePlan{}, err
	}
	if err := validatePurchaseItemUpdateVariant(ctx, executor, state.householdID, command, values.variantID); err != nil {
		return purchaseItemUpdatePlan{}, err
	}
	calculation, err := preparePurchaseItemUpdateCalculation(command, item)
	if err != nil {
		return purchaseItemUpdatePlan{}, err
	}
	payments, updatePayments, err := adjustedPurchasePayments(ctx, executor, purchase, item.AmountYen, values.amount)
	if err != nil {
		return purchaseItemUpdatePlan{}, err
	}
	categoryValue, err := nullableUUID(values.categoryID)
	if err != nil {
		return purchaseItemUpdatePlan{}, ErrNotFound
	}
	variantValue, err := nullableUUID(values.variantID)
	if err != nil {
		return purchaseItemUpdatePlan{}, ErrNotFound
	}
	itemPlan, err := buildItemChangePlan(ctx, tx, executor, item, purchase, itemChangeInput{
		fields: command.ChangedFields, amount: values.amount, categoryID: categoryValue, productVariantID: variantValue,
		allocationOverride: calculation.allocationOverride, categoryOrVariantChanged: command.CategoryIDSet || command.ProductVariantIDSet,
		allocationsSet: command.AllocationsSet, requestedWeights: calculation.requestedWeights,
		defaultAllocationVersion: state.household.DefaultAllocationVersion,
	})
	if err != nil {
		return purchaseItemUpdatePlan{}, err
	}
	return assemblePurchaseItemUpdatePlan(state.status, deferAccountingSync, values, calculation, itemPlan, payments, updatePayments), nil
}
