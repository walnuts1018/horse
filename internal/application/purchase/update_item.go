package purchase

import (
	"context"
	"database/sql"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
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
	itemLearningItemOnly      = "ITEM_ONLY"
	itemLearningVariantName   = "VARIANT_NAME"
	itemLearningMerchantAlias = "MERCHANT_ALIAS"
)

func (s *Service) UpdateItem(ctx context.Context, command UpdateItemCommand) (UpdateResult, error) {
	if len(command.ChangedFields) == 0 {
		return UpdateResult{}, fmt.Errorf("%w: patch must contain a field", ErrBadInput)
	}
	seenFields := make(map[string]struct{}, len(command.ChangedFields))
	for _, field := range command.ChangedFields {
		if !slices.Contains([]string{itemFieldName, itemFieldAmountYen, itemFieldQuantity, itemFieldPrintedUnitPrice, itemFieldCategoryID, itemFieldProductVariantID, itemFieldAllocations}, field) {
			return UpdateResult{}, fmt.Errorf("%w: invalid changed field", ErrBadInput)
		}
		if _, exists := seenFields[field]; exists {
			return UpdateResult{}, fmt.Errorf("%w: duplicate changed field", ErrBadInput)
		}
		seenFields[field] = struct{}{}
		provided := switchItemChangedField(command, field)
		if !provided {
			return UpdateResult{}, fmt.Errorf("%w: changed field has no value", ErrBadInput)
		}
	}
	itemUUID, err := parseUUID(command.ItemID, "item")
	if err != nil {
		return UpdateResult{}, err
	}
	actorUUID, err := parseUUID(command.ActorID, "actor")
	if err != nil {
		return UpdateResult{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return UpdateResult{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	observedItem, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemUUID)))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return UpdateResult{}, ErrNotFound
	}
	if err != nil {
		return UpdateResult{}, err
	}
	observedPurchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(observedItem.PurchaseID)))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return UpdateResult{}, ErrNotFound
	}
	if err != nil {
		return UpdateResult{}, err
	}
	householdID := observedItem.HouseholdID
	if err := requirePurchaseMember(ctx, executor, householdID, actorUUID); err != nil {
		return UpdateResult{}, err
	}
	observedPeriod := accountingPeriod(observedPurchase.OccurredAt)
	if err := lockPurchaseAccountingPeriods(ctx, executor, householdID, observedPeriod); err != nil {
		return UpdateResult{}, err
	}
	household, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForShare()).One(ctx, executor)
	if err != nil {
		return UpdateResult{}, err
	}
	purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(observedItem.PurchaseID))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return UpdateResult{}, ErrNotFound
	}
	if err != nil {
		return UpdateResult{}, err
	}
	item, err := models.PurchaseItems.Query(sm.Where(psql.And(
		models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemUUID)),
		models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchase.ID)),
	)), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return UpdateResult{}, ErrNotFound
	}
	if err != nil {
		return UpdateResult{}, err
	}
	if !purchase.OccurredAt.Equal(observedPurchase.OccurredAt) || purchase.HouseholdID != householdID {
		return UpdateResult{}, ErrConflict
	}
	status := purchase.Status
	if status != draftStatus && status != postedStatus {
		return UpdateResult{}, fmt.Errorf("%w: closed purchases require a correction", ErrConflict)
	}
	if command.BaseRevision > item.Revision {
		return UpdateResult{}, ErrConflict
	}
	conflict, err := purchaseUpdateValuesConflict(item.FieldRevisions.Val, command.BaseRevision, command.ChangedFields)
	if err != nil {
		return UpdateResult{}, err
	}
	if conflict {
		return UpdateResult{}, ErrConflict
	}
	newName, newAmount := item.Name, item.AmountYen
	if command.Name != nil {
		newName = strings.TrimSpace(*command.Name)
		if newName == "" || len(newName) > 240 {
			return UpdateResult{}, fmt.Errorf("%w: invalid item name", ErrBadInput)
		}
	}
	if command.AmountYen != nil {
		newAmount = *command.AmountYen
		if newAmount < 0 || newAmount > maxMoney {
			return UpdateResult{}, fmt.Errorf("%w: invalid item amount", ErrBadInput)
		}
	}
	categoryID, variantID := nullableID(item.CategoryID), nullableID(item.ProductVariantID)
	if command.CategoryIDSet {
		categoryID = command.CategoryID
	}
	if command.ProductVariantIDSet {
		variantID = command.ProductVariantID
	}
	if command.ProductLearningSet && command.ProductLearning == nil {
		return UpdateResult{}, fmt.Errorf("%w: product learning configuration must select a scope", ErrBadInput)
	}
	learningScope, learningVariantName, err := updateItemLearning(command.ProductLearning)
	if err != nil {
		return UpdateResult{}, err
	}
	if command.ProductLearning != nil && !slices.Contains(command.ChangedFields, itemFieldProductVariantID) {
		return UpdateResult{}, fmt.Errorf("%w: product learning requires a product variant change", ErrBadInput)
	}
	if command.ProductVariantIDSet && variantID != nil {
		variantUUID, parseErr := parseUUID(*variantID, "product variant")
		if parseErr != nil {
			return UpdateResult{}, ErrNotFound
		}
		variant, queryErr := models.ProductVariants.Query(sm.Where(psql.And(
			models.ProductVariants.Columns.ID.EQ(psql.Arg(variantUUID)),
			models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.ProductVariants.Columns.Archived.EQ(psql.Arg(false)),
		))).One(ctx, executor)
		if errors.Is(queryErr, sql.ErrNoRows) {
			return UpdateResult{}, ErrNotFound
		}
		if queryErr != nil {
			return UpdateResult{}, queryErr
		}
		concept, queryErr := models.ProductConcepts.Query(sm.Where(psql.And(
			models.ProductConcepts.Columns.ID.EQ(psql.Arg(variant.ConceptID)),
			models.ProductConcepts.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		))).One(ctx, executor)
		if errors.Is(queryErr, sql.ErrNoRows) || queryErr == nil && concept.Archived {
			return UpdateResult{}, ErrNotFound
		}
		if queryErr != nil {
			return UpdateResult{}, queryErr
		}
	}
	if learningScope != itemLearningItemOnly && variantID == nil {
		return UpdateResult{}, fmt.Errorf("%w: product learning requires a linked variant", ErrBadInput)
	}
	measure, err := measureFromItemUpdate(command, item)
	if err != nil {
		return UpdateResult{}, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	ruleVersion := household.DefaultAllocationVersion
	allocationOverride := item.AllocationOverride
	if command.AllocationsSet {
		allocationOverride = true
	}
	var weights []domain.Weight
	var ruleID *gofrsuuid.UUID
	allocationRuleVersion := ruleVersion
	if command.AllocationsSet {
		weights = make([]domain.Weight, 0, len(command.Allocations))
		for _, allocation := range command.Allocations {
			weights = append(weights, domain.Weight{PoolID: allocation.PoolID, Value: allocation.Weight})
		}
	} else if (command.CategoryIDSet || command.ProductVariantIDSet) && !item.AllocationOverride {
		var categoryUUID, variantUUID *gofrsuuid.UUID
		if categoryID != nil {
			parsed, parseErr := parseUUID(*categoryID, "category")
			if parseErr != nil {
				return UpdateResult{}, ErrNotFound
			}
			categoryUUID = &parsed
		}
		if variantID != nil {
			parsed, parseErr := parseUUID(*variantID, "product variant")
			if parseErr != nil {
				return UpdateResult{}, ErrNotFound
			}
			variantUUID = &parsed
		}
		weights, ruleID, allocationRuleVersion, err = allocationRuleWeights(ctx, tx, householdID, categoryUUID, variantUUID, normalizeMerchant(purchase.Merchant))
		if err == nil && ruleID == nil {
			weights, err = loadWeights(ctx, tx, householdID, ruleVersion)
		}
	} else {
		allocations, queryErr := models.ItemAllocations.Query(sm.Where(models.ItemAllocations.Columns.ItemID.EQ(psql.Arg(itemUUID))), sm.OrderBy(models.ItemAllocations.Columns.PoolID)).All(ctx, executor)
		if queryErr != nil {
			return UpdateResult{}, queryErr
		}
		for _, allocation := range allocations {
			weights = append(weights, domain.Weight{PoolID: allocation.PoolID.String(), Value: allocation.Weight})
			if len(weights) == 1 {
				if allocation.RuleID.Valid {
					id := allocation.RuleID.V
					ruleID = &id
				}
				allocationRuleVersion = allocation.RuleVersion
			}
		}
		if len(weights) == 0 {
			weights, err = loadWeights(ctx, tx, householdID, ruleVersion)
		}
	}
	if err != nil {
		return UpdateResult{}, err
	}
	allocated, err := domain.Allocate(newAmount, weights)
	if err != nil {
		return UpdateResult{}, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	payments, updatePayments, err := adjustedPurchasePayments(ctx, executor, purchase, item.AmountYen, newAmount)
	if err != nil {
		return UpdateResult{}, err
	}
	newRevision := item.Revision + 1
	updatedFields, err := purchaseUpdateFieldRevisions(item.FieldRevisions.Val, command.ChangedFields, newRevision)
	if err != nil {
		return UpdateResult{}, err
	}
	if learningScope == itemLearningVariantName {
		variantName := strings.TrimSpace(*learningVariantName)
		variantKey := normalizeProductName(variantName)
		if variantKey == "" || len(variantName) > 240 {
			return UpdateResult{}, fmt.Errorf("%w: invalid normalized product name", ErrBadInput)
		}
		variantUUID, parseErr := parseUUID(*variantID, "product variant")
		if parseErr != nil {
			return UpdateResult{}, ErrNotFound
		}
		if _, err := models.ProductVariants.Update((&models.ProductVariantSetter{Name: &variantName, NormalizedName: &variantKey}).UpdateMod(),
			um.Where(psql.And(models.ProductVariants.Columns.ID.EQ(psql.Arg(variantUUID)), models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdID)))),
			um.SetCol("revision").To(models.ProductVariants.Columns.Revision.Plus(psql.Arg(int64(1)))),
			um.SetCol("updated_at").To(psql.F("now")),
		).Exec(ctx, executor); err != nil {
			return UpdateResult{}, err
		}
		variant, err := models.ProductVariants.Query(sm.Where(models.ProductVariants.Columns.ID.EQ(psql.Arg(variantUUID)))).One(ctx, executor)
		if err != nil {
			return UpdateResult{}, err
		}
		if err := writeEvent(ctx, tx, householdID, variant.ID, "product.variant.renamed", variant.Revision, []string{itemFieldName}, actorUUID); err != nil {
			return UpdateResult{}, err
		}
	}
	categoryValue, err := nullableUUID(categoryID)
	if err != nil {
		return UpdateResult{}, ErrNotFound
	}
	variantValue, err := nullableUUID(variantID)
	if err != nil {
		return UpdateResult{}, ErrNotFound
	}
	fieldRevisionValue := types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(updatedFields)}
	if _, err := models.PurchaseItems.Update((&models.PurchaseItemSetter{
		Name: &newName, AmountYen: &newAmount, CategoryID: &categoryValue, ProductVariantID: &variantValue,
		AllocationOverride: &allocationOverride, Revision: &newRevision, FieldRevisions: &fieldRevisionValue,
		QuantityNumerator: &measure.quantityNumerator, QuantityDenominator: &measure.quantityDenominator, QuantityUnit: &measure.quantityUnit,
		PrintedUnitPriceYen: &measure.printedUnitPriceYen, PrintedUnitPriceQuantityNumerator: &measure.printedUnitPriceQuantityNumerator,
		PrintedUnitPriceQuantityDenominator: &measure.printedUnitPriceQuantityDenominator, PrintedUnitPriceUnit: &measure.printedUnitPriceUnit,
	}).UpdateMod(), um.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemUUID))), um.SetCol("updated_at").To(psql.F("now"))).Exec(ctx, executor); err != nil {
		return UpdateResult{}, err
	}
	if command.ProductVariantIDSet && variantID != nil {
		merchantKey := normalizeMerchant(purchase.Merchant)
		if learningScope == itemLearningMerchantAlias {
			if merchantKey == "" {
				return UpdateResult{}, fmt.Errorf("%w: merchant-specific learning requires a merchant", ErrBadInput)
			}
			aliasID, err := upsertProductAlias(ctx, executor, householdID, variantID, merchantKey, item.Name, normalizeProductName(item.Name))
			if err != nil {
				return UpdateResult{}, err
			}
			aliasUUID, parseErr := parseUUID(aliasID, "product alias")
			if parseErr != nil {
				return UpdateResult{}, parseErr
			}
			alias, err := models.ProductAliases.Query(sm.Where(models.ProductAliases.Columns.ID.EQ(psql.Arg(aliasUUID)))).One(ctx, executor)
			if err != nil {
				return UpdateResult{}, err
			}
			if err := writeEvent(ctx, tx, householdID, alias.ID, "product.alias.learned", alias.Revision, []string{"alias", "variantId", "merchant"}, actorUUID); err != nil {
				return UpdateResult{}, err
			}
		}
		variantUUID, parseErr := parseUUID(*variantID, "product variant")
		if parseErr != nil {
			return UpdateResult{}, ErrNotFound
		}
		previousVariant, parseErr := nullableUUID(variantIDValue(item.ProductVariantID))
		if parseErr != nil {
			return UpdateResult{}, ErrNotFound
		}
		correctionUUID := newUUID()
		if _, err := models.ProductCorrections.Insert(&models.ProductCorrectionSetter{
			ID: &correctionUUID, HouseholdID: &householdID, PurchaseID: &purchase.ID, ItemID: &itemUUID,
			RawName: &item.Name, PreviousProductVariantID: &previousVariant, ProductVariantID: &variantUUID,
			LearningScope: &learningScope, MerchantKey: &merchantKey, ActorID: &actorUUID,
		}).Exec(ctx, executor); err != nil {
			return UpdateResult{}, err
		}
	}
	if _, err := models.ItemAllocations.Delete(dm.Where(models.ItemAllocations.Columns.ItemID.EQ(psql.Arg(itemUUID)))).Exec(ctx, executor); err != nil {
		return UpdateResult{}, err
	}
	for _, allocation := range allocated {
		poolUUID, parseErr := parseUUID(allocation.PoolID, "pool")
		if parseErr != nil {
			return UpdateResult{}, parseErr
		}
		ruleValue := sql.Null[gofrsuuid.UUID]{}
		if ruleID != nil {
			ruleValue = sql.Null[gofrsuuid.UUID]{V: *ruleID, Valid: true}
		}
		if _, err := models.ItemAllocations.Insert(&models.ItemAllocationSetter{
			HouseholdID: &householdID, PurchaseID: &purchase.ID, ItemID: &itemUUID, PoolID: &poolUUID,
			Weight: &allocation.Weight, AmountYen: &allocation.Amount, RuleVersion: &allocationRuleVersion, RuleID: &ruleValue,
		}).Exec(ctx, executor); err != nil {
			return UpdateResult{}, err
		}
	}
	if updatePayments {
		validated, err := validatePayments(ctx, tx, householdID, payments)
		if err != nil {
			return UpdateResult{}, err
		}
		if err := replaceUpdatePayments(ctx, executor, purchase, validated); err != nil {
			return UpdateResult{}, err
		}
	}
	if _, err := models.Purchases.Update(um.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchase.ID))),
		um.SetCol("revision").To(models.Purchases.Columns.Revision.Plus(psql.Arg(int64(1)))), um.SetCol("updated_at").To(psql.F("now")),
	).Exec(ctx, executor); err != nil {
		return UpdateResult{}, err
	}
	if err := writeEvent(ctx, tx, householdID, itemUUID, "purchase.item.updated", newRevision, command.ChangedFields, actorUUID); err != nil {
		return UpdateResult{}, err
	}
	if status == postedStatus {
		if err := syncPostedPurchaseAccounting(ctx, executor, purchase); err != nil {
			return UpdateResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return UpdateResult{}, err
	}
	return UpdateResult{PurchaseID: purchase.ID.String()}, nil
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
	if result.quantityNumerator.Valid && (result.quantityNumerator.V <= 0 || result.quantityNumerator.V > maxQuantityNumerator || result.quantityDenominator.V <= 0 || result.quantityDenominator.V > maxQuantityDenominator || strings.TrimSpace(result.quantityUnit.V) == "" || len(result.quantityUnit.V) > 32) {
		return purchaseItemUpdateMeasure{}, errors.New("invalid quantity")
	}
	if result.printedUnitPriceYen.Valid != result.printedUnitPriceQuantityNumerator.Valid || result.printedUnitPriceYen.Valid != result.printedUnitPriceQuantityDenominator.Valid || result.printedUnitPriceYen.Valid != result.printedUnitPriceUnit.Valid {
		return purchaseItemUpdateMeasure{}, errors.New("printed unit price fields must be provided together")
	}
	if result.printedUnitPriceYen.Valid && (result.printedUnitPriceYen.V < 0 || result.printedUnitPriceYen.V > maxMoney || result.printedUnitPriceQuantityNumerator.V <= 0 || result.printedUnitPriceQuantityNumerator.V > maxQuantityNumerator || result.printedUnitPriceQuantityDenominator.V <= 0 || result.printedUnitPriceQuantityDenominator.V > maxQuantityDenominator || strings.TrimSpace(result.printedUnitPriceUnit.V) == "" || len(result.printedUnitPriceUnit.V) > 32) {
		return purchaseItemUpdateMeasure{}, errors.New("invalid printed unit price")
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
	n, err := strconv.ParseInt(*numerator, 10, 64)
	if err != nil || n <= 0 || n > maxQuantityNumerator || strconv.FormatInt(n, 10) != *numerator {
		return sql.Null[int64]{}, sql.Null[int64]{}, errors.New("invalid rational numerator")
	}
	d, err := strconv.ParseInt(*denominator, 10, 64)
	if err != nil || d <= 0 || d > maxQuantityDenominator || strconv.FormatInt(d, 10) != *denominator {
		return sql.Null[int64]{}, sql.Null[int64]{}, errors.New("invalid rational denominator")
	}
	if new(big.Int).GCD(nil, nil, big.NewInt(n), big.NewInt(d)).Int64() != 1 {
		return sql.Null[int64]{}, sql.Null[int64]{}, errors.New("rational must be reduced")
	}
	return sql.Null[int64]{V: n, Valid: true}, sql.Null[int64]{V: d, Valid: true}, nil
}

func updateItemLearning(value *ProductLearning) (string, *string, error) {
	if value == nil {
		return itemLearningItemOnly, nil, nil
	}
	selected := 0
	if value.ItemOnly != nil {
		selected++
	}
	if value.VariantName != nil {
		selected++
	}
	if value.MerchantAlias != nil {
		selected++
	}
	if selected != 1 {
		return "", nil, fmt.Errorf("%w: product learning configuration must select exactly one scope", ErrBadInput)
	}
	if value.ItemOnly != nil && *value.ItemOnly {
		return itemLearningItemOnly, nil, nil
	}
	if value.VariantName != nil {
		return itemLearningVariantName, value.VariantName, nil
	}
	if value.MerchantAlias != nil && *value.MerchantAlias {
		return itemLearningMerchantAlias, nil, nil
	}
	return "", nil, fmt.Errorf("%w: product learning marker must be true", ErrBadInput)
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
	weights := make([]domain.Weight, 0, len(payments))
	for _, payment := range payments {
		weights = append(weights, domain.Weight{PoolID: payment.AccountID, Value: payment.AmountYen})
	}
	amounts, err := domain.Allocate(newTotal, weights)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	amountByAccount := make(map[string]int64, len(amounts))
	for _, amount := range amounts {
		amountByAccount[amount.PoolID] = amount.Amount
	}
	adjusted := make([]Payment, 0, len(payments))
	for _, payment := range payments {
		if amount := amountByAccount[payment.AccountID]; amount > 0 {
			adjusted = append(adjusted, Payment{AccountID: payment.AccountID, AmountYen: amount})
		}
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
