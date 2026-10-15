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
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/txcleanup"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const (
	itemFieldName             = "name"
	itemFieldAmountYen        = "amountYen"
	itemFieldQuantity         = "quantity"
	itemFieldPrintedUnitPrice = "printedUnitPrice"
	itemFieldCategoryID       = "categoryId"
	itemFieldProductVariantID = "productVariantId"
	itemFieldAllocations      = "allocations"
)

func (s *Service) UpdateItem(ctx context.Context, command UpdateItemCommand) (UpdateResult, error) {
	return s.updateItem(ctx, command, nil, false, nil)
}

func (s *Service) updateItem(ctx context.Context, command UpdateItemCommand, sharedTx pgx.Tx, deferAccountingSync bool, accountingChanged *bool) (UpdateResult, error) {
	if err := validateUpdateItemCommand(command); err != nil {
		return UpdateResult{}, err
	}
	itemUUID, err := parseUUID(command.ItemID, "item")
	if err != nil {
		return UpdateResult{}, err
	}
	actorUUID, err := parseUUID(command.ActorID, "actor")
	if err != nil {
		return UpdateResult{}, err
	}
	tx := sharedTx
	ownsTransaction := tx == nil
	if ownsTransaction {
		tx, err = s.db.Begin(ctx)
		if err != nil {
			return UpdateResult{}, err
		}
		defer txcleanup.Rollback(ctx, tx)
	}
	executor := bobpgx.NewTx(tx, nil)
	state, err := loadPurchaseItemUpdateState(ctx, executor, itemUUID, actorUUID, command)
	if err != nil {
		return UpdateResult{}, err
	}
	plan, err := buildPurchaseItemUpdatePlan(ctx, tx, executor, command, state, deferAccountingSync)
	if err != nil {
		return UpdateResult{}, err
	}
	if accountingChanged != nil {
		*accountingChanged = *accountingChanged || plan.itemChange.allocationsChanged
	}
	if err := applyItemVariantNameLearning(ctx, executor, state.householdID, actorUUID, plan.variantID, plan.productLearning); err != nil {
		return UpdateResult{}, err
	}
	fieldRevisionValue := itemChangeFieldValue(plan.itemChange)
	if _, err := models.PurchaseItems.Update((&models.PurchaseItemSetter{
		Name: &plan.name, AmountYen: &plan.amount, CategoryID: &plan.itemChange.categoryID, ProductVariantID: &plan.itemChange.productVariantID,
		AllocationOverride: &plan.itemChange.allocationOverride, Revision: &plan.itemChange.revision, FieldRevisions: &fieldRevisionValue,
		QuantityNumerator: &plan.measure.quantityNumerator, QuantityDenominator: &plan.measure.quantityDenominator, QuantityUnit: &plan.measure.quantityUnit,
		PrintedUnitPriceYen: &plan.measure.printedUnitPriceYen, PrintedUnitPriceQuantityNumerator: &plan.measure.printedUnitPriceQuantityNumerator,
		PrintedUnitPriceQuantityDenominator: &plan.measure.printedUnitPriceQuantityDenominator, PrintedUnitPriceUnit: &plan.measure.printedUnitPriceUnit,
	}).UpdateMod(), um.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemUUID))), um.SetCol("updated_at").To(psql.F("now"))).Exec(ctx, executor); err != nil {
		return UpdateResult{}, err
	}
	if err := recordItemProductVariantChange(ctx, executor, state.householdID, actorUUID, state.purchase, state.item, itemUUID, command, plan.variantID, plan.productLearning); err != nil {
		return UpdateResult{}, err
	}
	if err := applyItemAllocationPlan(ctx, executor, state.item, plan.itemChange); err != nil {
		return UpdateResult{}, err
	}
	if err := applyPurchaseItemPaymentPlan(ctx, tx, executor, state.householdID, state.purchase, plan.accounting); err != nil {
		return UpdateResult{}, err
	}
	if _, err := models.Purchases.Update(um.Where(models.Purchases.Columns.ID.EQ(psql.Arg(state.purchase.ID))),
		um.SetCol("revision").To(models.Purchases.Columns.Revision.Plus(psql.Arg(int64(1)))), um.SetCol("updated_at").To(psql.F("now")),
	).Exec(ctx, executor); err != nil {
		return UpdateResult{}, err
	}
	if err := accounting.RecordEvent(ctx, executor, state.householdID, itemUUID, "purchase.item.updated", plan.itemChange.revision, command.ChangedFields, actorUUID); err != nil {
		return UpdateResult{}, err
	}
	if err := applyPurchaseItemAccountingSync(ctx, executor, state.purchase, plan.accounting); err != nil {
		return UpdateResult{}, err
	}
	if ownsTransaction {
		if err := tx.Commit(ctx); err != nil {
			return UpdateResult{}, err
		}
	}
	return UpdateResult{PurchaseID: state.purchase.ID.String()}, nil
}

type purchaseItemUpdateMeasure struct {
	quantityNumerator, quantityDenominator                                                      sql.Null[int64]
	quantityUnit                                                                                sql.Null[string]
	printedUnitPriceYen, printedUnitPriceQuantityNumerator, printedUnitPriceQuantityDenominator sql.Null[int64]
	printedUnitPriceUnit                                                                        sql.Null[string]
}

func measureFromItemUpdate(command UpdateItemCommand, item *models.PurchaseItem) (purchaseItemUpdateMeasure, error) {
	result := purchaseItemUpdateMeasure{
		quantityNumerator: item.QuantityNumerator, quantityDenominator: item.QuantityDenominator, quantityUnit: item.QuantityUnit,
		printedUnitPriceYen: item.PrintedUnitPriceYen, printedUnitPriceQuantityNumerator: item.PrintedUnitPriceQuantityNumerator,
		printedUnitPriceQuantityDenominator: item.PrintedUnitPriceQuantityDenominator, printedUnitPriceUnit: item.PrintedUnitPriceUnit,
	}
	if command.QuantitySet {
		var numerator, denominator, unit *string
		if command.Quantity != nil {
			numerator, denominator, unit = &command.Quantity.Numerator, &command.Quantity.Denominator, &command.Quantity.Unit
		}
		var err error
		result.quantityNumerator, result.quantityDenominator, err = updateRational(numerator, denominator)
		if err != nil {
			return purchaseItemUpdateMeasure{}, err
		}
		result.quantityUnit = nullStringValue(unit)
	} else if slices.Contains(command.ChangedFields, itemFieldQuantity) {
		return purchaseItemUpdateMeasure{}, errors.New("changed quantity has no value")
	}
	if command.PrintedUnitPriceSet {
		var price *int64
		var numerator, denominator, unit *string
		if command.PrintedUnitPrice != nil {
			price = &command.PrintedUnitPrice.AmountYen
			numerator, denominator = &command.PrintedUnitPrice.Basis.Numerator, &command.PrintedUnitPrice.Basis.Denominator
			unit = &command.PrintedUnitPrice.Basis.Unit
		}
		result.printedUnitPriceYen = nullInt64(price)
		var err error
		result.printedUnitPriceQuantityNumerator, result.printedUnitPriceQuantityDenominator, err = updateRational(numerator, denominator)
		if err != nil {
			return purchaseItemUpdateMeasure{}, err
		}
		result.printedUnitPriceUnit = nullStringValue(unit)
	} else if slices.Contains(command.ChangedFields, itemFieldPrintedUnitPrice) {
		return purchaseItemUpdateMeasure{}, errors.New("changed printed unit price has no value")
	}
	if result.quantityNumerator.Valid != result.quantityDenominator.Valid || result.quantityNumerator.Valid != result.quantityUnit.Valid {
		return purchaseItemUpdateMeasure{}, errors.New("quantity fields must be provided together")
	}
	if result.printedUnitPriceYen.Valid != result.printedUnitPriceQuantityNumerator.Valid || result.printedUnitPriceYen.Valid != result.printedUnitPriceQuantityDenominator.Valid || result.printedUnitPriceYen.Valid != result.printedUnitPriceUnit.Valid {
		return purchaseItemUpdateMeasure{}, errors.New("printed unit price fields must be provided together")
	}
	var quantity *domain.Measure
	if result.quantityNumerator.Valid {
		quantity = &domain.Measure{
			Value: domain.Rational{Numerator: result.quantityNumerator.V, Denominator: result.quantityDenominator.V},
			Unit:  result.quantityUnit.V,
		}
	}
	var printedPrice *domain.PrintedUnitPrice
	if result.printedUnitPriceYen.Valid {
		printedPrice = &domain.PrintedUnitPrice{
			AmountYen: result.printedUnitPriceYen.V,
			Basis: domain.Measure{
				Value: domain.Rational{Numerator: result.printedUnitPriceQuantityNumerator.V, Denominator: result.printedUnitPriceQuantityDenominator.V},
				Unit:  result.printedUnitPriceUnit.V,
			},
		}
	}
	if err := domain.ValidateMeasure(quantity, printedPrice); err != nil {
		return purchaseItemUpdateMeasure{}, err
	}
	return result, nil
}

func updateRational(numerator, denominator *string) (sql.Null[int64], sql.Null[int64], error) {
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

func adjustedPurchasePayments(ctx context.Context, executor bob.Executor, purchase *models.Purchase, oldAmount, newAmount int64) ([]Payment, bool, error) {
	if oldAmount == newAmount {
		return nil, false, nil
	}
	items, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchase.ID)))).All(ctx, executor)
	if err != nil {
		return nil, false, err
	}
	var currentTotal int64
	for _, item := range items {
		var ok bool
		currentTotal, ok = checkedAdd(currentTotal, item.AmountYen)
		if !ok || currentTotal > maxMoney {
			return nil, false, fmt.Errorf("%w: purchase total is too large", ErrBadInput)
		}
	}
	newTotal, ok := checkedAdd(currentTotal-oldAmount, newAmount)
	if !ok || newTotal > maxMoney {
		return nil, false, fmt.Errorf("%w: purchase total is too large", ErrBadInput)
	}
	var legacyAccountID *string
	if purchase.PaymentAccountID.Valid {
		id := purchase.PaymentAccountID.V.String()
		legacyAccountID = &id
	}
	payments, err := loadPurchaseItemPayments(ctx, executor, purchase, legacyAccountID, currentTotal)
	if err != nil {
		return nil, false, err
	}
	if len(payments) == 0 {
		return nil, false, nil
	}
	adjusted, err := adjustPaymentsToTotal(payments, newTotal)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	return adjusted, true, nil
}

func loadPurchaseItemPayments(ctx context.Context, executor bob.Executor, purchase *models.Purchase, legacyAccountID *string, total int64) ([]Payment, error) {
	rows, err := models.PurchasePayments.Query(sm.Where(psql.And(
		models.PurchasePayments.Columns.HouseholdID.EQ(psql.Arg(purchase.HouseholdID)),
		models.PurchasePayments.Columns.PurchaseID.EQ(psql.Arg(purchase.ID)),
	)), sm.OrderBy(models.PurchasePayments.Columns.LineNo)).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	payments := make([]Payment, 0, len(rows))
	var paid int64
	for _, row := range rows {
		var ok bool
		paid, ok = checkedAdd(paid, row.AmountYen)
		if !ok {
			return nil, fmt.Errorf("%w: payment total is too large", ErrBadInput)
		}
		payments = append(payments, Payment{AccountID: row.AccountID.String(), AmountYen: row.AmountYen})
	}
	if len(payments) == 0 && legacyAccountID != nil {
		payments = append(payments, Payment{AccountID: *legacyAccountID, AmountYen: total})
		paid = total
	}
	if len(payments) > 0 && paid != total {
		return nil, fmt.Errorf("%w: payment allocations must equal purchase total", ErrBadInput)
	}
	return payments, nil
}

func nullableID(value sql.Null[gofrsuuid.UUID]) *string {
	if !value.Valid {
		return nil
	}
	id := value.V.String()
	return &id
}
func variantIDValue(value sql.Null[gofrsuuid.UUID]) *string { return nullableID(value) }
func nullableUUID(value *string) (sql.Null[gofrsuuid.UUID], error) {
	if value == nil {
		return sql.Null[gofrsuuid.UUID]{}, nil
	}
	id, err := gofrsuuid.FromString(*value)
	if err != nil {
		return sql.Null[gofrsuuid.UUID]{}, err
	}
	return sql.Null[gofrsuuid.UUID]{V: id, Valid: true}, nil
}
func nullStringValue(value *string) sql.Null[string] {
	if value == nil {
		return sql.Null[string]{}
	}
	return sql.Null[string]{V: strings.TrimSpace(*value), Valid: true}
}
func nullInt64(value *int64) sql.Null[int64] {
	if value == nil {
		return sql.Null[int64]{}
	}
	return sql.Null[int64]{V: *value, Valid: true}
}
func normalizeProductName(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}
