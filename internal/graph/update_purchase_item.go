package graph

import (
	"context"
	"database/sql"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (r *mutationResolver) updatePurchaseItem(ctx context.Context, input model.UpdatePurchaseItemInput) (*model.PurchaseItem, error) {
	changedFields, err := purchaseItemChangedFieldKeys(input)
	if err != nil {
		return nil, err
	}
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	itemUUID, err := gofrsuuid.FromString(input.ItemID)
	if err != nil {
		return nil, ErrNotFound
	}
	executor := bobpgx.NewTx(tx, nil)
	observedItem, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemUUID)))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := observedItem.LoadPurchase(ctx, executor); err != nil {
		return nil, err
	}
	if observedItem.R.Purchase == nil {
		return nil, ErrNotFound
	}
	observedPurchase := observedItem.R.Purchase
	householdID := observedItem.HouseholdID.String()
	purchaseID := observedItem.PurchaseID.String()
	observedOccurredAt := observedPurchase.OccurredAt
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	household, err := models.Households.Query(
		sm.Where(models.Households.Columns.ID.EQ(psql.Arg(observedItem.HouseholdID))),
		sm.ForShare(),
	).One(ctx, executor)
	if err != nil {
		return nil, err
	}
	if err := lockAccountingPeriods(ctx, tx, householdID, accountingPeriod(observedOccurredAt)); err != nil {
		return nil, err
	}
	purchaseUUID := observedItem.PurchaseID
	purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseUUID))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	item, err := models.PurchaseItems.Query(sm.Where(psql.And(
		models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemUUID)),
		models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)),
	)), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !purchase.OccurredAt.Equal(observedOccurredAt) {
		return nil, ErrConflict
	}
	status := purchase.Status
	if status != string(model.PurchaseStatusDraft) && status != string(model.PurchaseStatusPosted) {
		return nil, fmt.Errorf("%w: closed purchases require a correction", ErrConflict)
	}
	name, amount := item.Name, item.AmountYen
	var categoryID *string
	if item.CategoryID.Valid {
		category := item.CategoryID.V.String()
		categoryID = &category
	}
	var productVariantID *string
	if item.ProductVariantID.Valid {
		variant := item.ProductVariantID.V.String()
		productVariantID = &variant
	}
	allocationOverride := item.AllocationOverride
	revision := item.Revision
	fieldRevisions := item.FieldRevisions.Val
	merchant := purchase.Merchant
	ruleVersion := household.DefaultAllocationVersion
	if input.BaseRevision > int(item.Revision) {
		return nil, ErrConflict
	}
	if input.ProductLearning != nil && !slices.Contains(changedFields, "productVariantId") {
		return nil, fmt.Errorf("%w: product learning requires a product variant change", ErrBadInput)
	}
	conflict, err := valuesConflict(item.FieldRevisions.Val, input.BaseRevision, changedFields)
	if err != nil {
		return nil, err
	}
	if conflict {
		return nil, ErrConflict
	}
	currentMeasure := purchaseItemMeasure{
		quantityNumerator: item.QuantityNumerator, quantityDenominator: item.QuantityDenominator, quantityUnit: item.QuantityUnit,
		printedUnitPriceYen: item.PrintedUnitPriceYen, printedUnitPriceQuantityNumerator: item.PrintedUnitPriceQuantityNumerator,
		printedUnitPriceQuantityDenominator: item.PrintedUnitPriceQuantityDenominator, printedUnitPriceUnit: item.PrintedUnitPriceUnit,
	}
	newMeasure, err := measureFromUpdate(input, currentMeasure, changedFields)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	newName, newAmount, newCategory := name, amount, categoryID
	if input.Name.IsSet() {
		newName = strings.TrimSpace(*input.Name.Value())
		if newName == "" || len(newName) > 240 {
			return nil, fmt.Errorf("%w: invalid item name", ErrBadInput)
		}
	}
	if input.AmountYen.IsSet() {
		newAmount = int64(*input.AmountYen.Value())
		if newAmount < 0 || newAmount > maxMoney {
			return nil, fmt.Errorf("%w: invalid item amount", ErrBadInput)
		}
	}
	updateCategoryID := slices.Contains(changedFields, "categoryId")
	if updateCategoryID {
		newCategory = input.CategoryID.Value()
	}
	updateProductVariantID := slices.Contains(changedFields, "productVariantId")
	newProductVariantID := productVariantID
	if updateProductVariantID {
		newProductVariantID = input.ProductVariantID.Value()
	}
	learningScope := model.ProductLearningScopeItemOnly
	if input.ProductLearning != nil {
		learningScope = *input.ProductLearning
	}
	if !learningScope.IsValid() || input.VariantName != nil && learningScope != model.ProductLearningScopeVariantName {
		return nil, fmt.Errorf("%w: invalid product learning scope", ErrBadInput)
	}
	if updateProductVariantID && newProductVariantID != nil {
		variantUUID, err := gofrsuuid.FromString(*newProductVariantID)
		if err != nil {
			return nil, ErrNotFound
		}
		variant, err := models.ProductVariants.Query(sm.Where(psql.And(
			models.ProductVariants.Columns.ID.EQ(psql.Arg(variantUUID)),
			models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(observedItem.HouseholdID)),
			models.ProductVariants.Columns.Archived.EQ(psql.Arg(false)),
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if err := variant.LoadProductConcept(ctx, executor); err != nil {
			return nil, err
		}
		if variant.R.ProductConcept == nil || variant.R.ProductConcept.Archived {
			return nil, ErrNotFound
		}
	}
	if learningScope != model.ProductLearningScopeItemOnly && newProductVariantID == nil {
		return nil, fmt.Errorf("%w: product learning requires a linked variant", ErrBadInput)
	}
	var weights []domain.Weight
	var appliedRuleID *string
	var appliedRuleVersion = ruleVersion
	newAllocationOverride := allocationOverride
	if input.Allocations.IsSet() {
		weights, err = allocationWeights(input.Allocations.Value())
		newAllocationOverride = true
	} else if (updateCategoryID || updateProductVariantID) && !allocationOverride {
		weights, appliedRuleID, appliedRuleVersion, err = allocationRuleWeights(ctx, tx, householdID, newCategory, newProductVariantID, normalizeMerchant(merchant))
		if err == nil && appliedRuleID == nil {
			weights, err = loadWeights(ctx, tx, householdID, ruleVersion)
		}
	} else {
		allocations, queryErr := models.ItemAllocations.Query(
			sm.Where(models.ItemAllocations.Columns.ItemID.EQ(psql.Arg(itemUUID))),
			sm.OrderBy(models.ItemAllocations.Columns.PoolID),
		).All(ctx, executor)
		if queryErr != nil {
			return nil, queryErr
		}
		for _, allocation := range allocations {
			weight := domain.Weight{PoolID: allocation.PoolID.String(), Value: allocation.Weight}
			if len(weights) == 0 {
				if allocation.RuleID.Valid {
					ruleID := allocation.RuleID.V.String()
					appliedRuleID = &ruleID
				}
				appliedRuleVersion = allocation.RuleVersion
			}
			weights = append(weights, weight)
		}
		if len(weights) == 0 {
			weights, err = loadWeights(ctx, tx, householdID, ruleVersion)
		}
	}
	if err != nil {
		return nil, err
	}
	allocated, err := domain.Allocate(newAmount, weights)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	var adjustedPayments []model.PurchasePaymentInput
	updatePayments := false
	if newAmount != item.AmountYen {
		purchaseItems, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, executor)
		if err != nil {
			return nil, err
		}
		var currentTotal int64
		for _, purchaseItem := range purchaseItems {
			var ok bool
			currentTotal, ok = checkedAdd(currentTotal, purchaseItem.AmountYen)
			if !ok || currentTotal > maxMoney {
				return nil, fmt.Errorf("%w: purchase total is too large", ErrBadInput)
			}
		}
		remainingTotal := currentTotal - item.AmountYen
		newTotal, ok := checkedAdd(remainingTotal, newAmount)
		if !ok || newTotal > maxMoney {
			return nil, fmt.Errorf("%w: purchase total is too large", ErrBadInput)
		}
		var legacyAccountID *string
		if purchase.PaymentAccountID.Valid {
			id := purchase.PaymentAccountID.V.String()
			legacyAccountID = &id
		}
		payments, err := purchasePayments(ctx, tx, householdID, purchaseID, legacyAccountID, currentTotal)
		if err != nil {
			return nil, err
		}
		if len(payments) > 0 {
			paymentWeights := make([]domain.Weight, 0, len(payments))
			for _, payment := range payments {
				paymentWeights = append(paymentWeights, domain.Weight{PoolID: payment.PaymentAccountID, Value: int64(payment.AmountYen)})
			}
			paymentAmounts, err := domain.Allocate(newTotal, paymentWeights)
			if err != nil {
				return nil, fmt.Errorf("%w: %w", ErrBadInput, err)
			}
			paymentAmountByAccount := make(map[string]int64, len(paymentAmounts))
			for _, paymentAmount := range paymentAmounts {
				paymentAmountByAccount[paymentAmount.PoolID] = paymentAmount.Amount
			}
			adjustedPayments = make([]model.PurchasePaymentInput, 0, len(payments))
			for _, payment := range payments {
				if amount := paymentAmountByAccount[payment.PaymentAccountID]; amount > 0 {
					adjustedPayments = append(adjustedPayments, model.PurchasePaymentInput{PaymentAccountID: payment.PaymentAccountID, AmountYen: graphLong(amount)})
				}
			}
			updatePayments = true
		}
	}
	newRevision := revision + 1
	updatedFields, err := setFieldRevisions(fieldRevisions, changedFields, newRevision)
	if err != nil {
		return nil, err
	}
	if learningScope == model.ProductLearningScopeVariantName {
		variantName := strings.TrimSpace(trimOptional(input.VariantName))
		variantKey := normalizeProductText(variantName)
		if variantKey == "" || len(variantName) > 240 {
			return nil, fmt.Errorf("%w: invalid normalized product name", ErrBadInput)
		}
		variantUUID, err := gofrsuuid.FromString(*newProductVariantID)
		if err != nil {
			return nil, ErrNotFound
		}
		householdUUID := observedItem.HouseholdID
		if _, err := models.ProductVariants.Update((&models.ProductVariantSetter{
			Name: &variantName, NormalizedName: &variantKey,
		}).UpdateMod(),
			um.Where(psql.And(
				models.ProductVariants.Columns.ID.EQ(psql.Arg(variantUUID)),
				models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			)),
			um.SetCol("revision").To(models.ProductVariants.Columns.Revision.Plus(psql.Arg(int64(1)))),
			um.SetCol("updated_at").To(psql.F("now")),
		).Exec(ctx, executor); err != nil {
			return nil, err
		}
		variant, err := models.ProductVariants.Query(sm.Where(models.ProductVariants.Columns.ID.EQ(psql.Arg(variantUUID)))).One(ctx, executor)
		if err != nil {
			return nil, err
		}
		if err := writeEvent(ctx, tx, householdID, "product.variant.renamed", *newProductVariantID, variant.Revision, []string{"name"}, user.MemberID); err != nil {
			return nil, err
		}
	}
	itemName, itemAmount := newName, newAmount
	categoryValue := sql.Null[gofrsuuid.UUID]{}
	if newCategory != nil {
		categoryUUID, err := gofrsuuid.FromString(*newCategory)
		if err != nil {
			return nil, ErrNotFound
		}
		categoryValue = sql.Null[gofrsuuid.UUID]{V: categoryUUID, Valid: true}
	}
	variantValue := sql.Null[gofrsuuid.UUID]{}
	if newProductVariantID != nil {
		variantUUID, err := gofrsuuid.FromString(*newProductVariantID)
		if err != nil {
			return nil, ErrNotFound
		}
		variantValue = sql.Null[gofrsuuid.UUID]{V: variantUUID, Valid: true}
	}
	fieldRevisionValue := types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(updatedFields)}
	if _, err := models.PurchaseItems.Update((&models.PurchaseItemSetter{
		Name: &itemName, AmountYen: &itemAmount, CategoryID: &categoryValue,
		ProductVariantID: &variantValue, AllocationOverride: &newAllocationOverride,
		Revision: &newRevision, FieldRevisions: &fieldRevisionValue,
		QuantityNumerator: &newMeasure.quantityNumerator, QuantityDenominator: &newMeasure.quantityDenominator, QuantityUnit: &newMeasure.quantityUnit,
		PrintedUnitPriceYen: &newMeasure.printedUnitPriceYen, PrintedUnitPriceQuantityNumerator: &newMeasure.printedUnitPriceQuantityNumerator,
		PrintedUnitPriceQuantityDenominator: &newMeasure.printedUnitPriceQuantityDenominator, PrintedUnitPriceUnit: &newMeasure.printedUnitPriceUnit,
	}).UpdateMod(),
		um.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemUUID))),
		um.SetCol("updated_at").To(psql.F("now")),
	).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if updateProductVariantID && newProductVariantID != nil {
		merchantKey := normalizeMerchant(merchant)
		if learningScope == model.ProductLearningScopeMerchantAlias {
			if merchantKey == "" {
				return nil, fmt.Errorf("%w: merchant-specific learning requires a merchant", ErrBadInput)
			}
			var aliasID string
			if err := upsertProductAlias(ctx, tx, householdID, *newProductVariantID, merchantKey, name, normalizeProductText(name), &aliasID); err != nil {
				return nil, err
			}
			aliasUUID, err := gofrsuuid.FromString(aliasID)
			if err != nil {
				return nil, ErrNotFound
			}
			alias, err := models.ProductAliases.Query(sm.Where(models.ProductAliases.Columns.ID.EQ(psql.Arg(aliasUUID)))).One(ctx, executor)
			if err != nil {
				return nil, err
			}
			if err := writeEvent(ctx, tx, householdID, "product.alias.learned", aliasID, alias.Revision, []string{"alias", "variantId", "merchant"}, user.MemberID); err != nil {
				return nil, err
			}
		}
		correctionUUID := generatedUUID(newID())
		variantUUID, err := gofrsuuid.FromString(*newProductVariantID)
		if err != nil {
			return nil, ErrNotFound
		}
		actorUUID, err := gofrsuuid.FromString(user.MemberID)
		if err != nil {
			return nil, err
		}
		var previousVariant sql.Null[gofrsuuid.UUID]
		if productVariantID != nil {
			previousUUID, err := gofrsuuid.FromString(*productVariantID)
			if err != nil {
				return nil, ErrNotFound
			}
			previousVariant = sql.Null[gofrsuuid.UUID]{V: previousUUID, Valid: true}
		}
		correctionScope := string(learningScope)
		if _, err := models.ProductCorrections.Insert(&models.ProductCorrectionSetter{
			ID: &correctionUUID, HouseholdID: &observedItem.HouseholdID, PurchaseID: &purchaseUUID,
			ItemID: &itemUUID, RawName: &name, PreviousProductVariantID: &previousVariant,
			ProductVariantID: &variantUUID, LearningScope: &correctionScope, MerchantKey: &merchantKey, ActorID: &actorUUID,
		}).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	if _, err := models.ItemAllocations.Delete(dm.Where(models.ItemAllocations.Columns.ItemID.EQ(psql.Arg(itemUUID)))).Exec(ctx, executor); err != nil {
		return nil, err
	}
	for _, allocation := range allocated {
		poolUUID := generatedUUID(allocation.PoolID)
		var ruleUUID sql.Null[gofrsuuid.UUID]
		if appliedRuleID != nil {
			ruleUUID = sql.Null[gofrsuuid.UUID]{V: generatedUUID(*appliedRuleID), Valid: true}
		}
		if _, err := models.ItemAllocations.Insert(&models.ItemAllocationSetter{
			HouseholdID: &observedItem.HouseholdID, PurchaseID: &purchaseUUID, ItemID: &itemUUID,
			PoolID: &poolUUID, Weight: &allocation.Weight, AmountYen: &allocation.Amount,
			RuleVersion: &appliedRuleVersion, RuleID: &ruleUUID,
		}).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	if updatePayments {
		if err := replacePurchasePaymentRows(ctx, tx, householdID, purchaseID, adjustedPayments); err != nil {
			return nil, err
		}
	}
	if _, err := models.Purchases.Update(um.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseUUID))),
		um.SetCol("revision").To(models.Purchases.Columns.Revision.Plus(psql.Arg(int64(1)))),
		um.SetCol("updated_at").To(psql.F("now")),
	).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, householdID, "purchase.item.updated", input.ItemID, newRevision, changedFields, user.MemberID); err != nil {
		return nil, err
	}
	if status == string(model.PurchaseStatusPosted) {
		if err := syncPostedPurchaseAccounting(ctx, tx, purchaseID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	loadedPurchase, err := loadPurchase(ctx, r.DB, purchaseID)
	if err != nil {
		return nil, err
	}
	for _, item := range loadedPurchase.Items {
		if item.ID == input.ItemID {
			return item, nil
		}
	}
	return nil, ErrNotFound
}
