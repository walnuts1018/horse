package purchase

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func normalizeCreateCommand(input CreateCommand) (CreateCommand, string, error) {
	if input.SourceReference != nil && (input.SourceDocumentID == nil || strings.ContainsAny(*input.SourceReference, "\r\n")) {
		return CreateCommand{}, "", fmt.Errorf("%w: source reference requires a document and must be valid", ErrBadInput)
	}
	origin := input.Origin
	if origin == "" {
		origin = manualOrigin
		if input.SourceDocumentID != nil {
			origin = "AI"
		}
	}
	if input.DatePrecision == "" {
		input.DatePrecision = "DATE_TIME"
	}
	if input.DatePrecision != "DATE_TIME" && input.DatePrecision != "DATE_ONLY" {
		return CreateCommand{}, "", fmt.Errorf("%w: invalid purchase date precision", ErrBadInput)
	}
	if origin != "AI" && origin != manualOrigin && origin != "UNKNOWN" {
		return CreateCommand{}, "", fmt.Errorf("%w: invalid purchase origin", ErrBadInput)
	}
	input.Merchant = strings.TrimSpace(input.Merchant)
	return input, origin, nil
}

func createPurchaseItems(ctx context.Context, tx pgx.Tx, executor bob.Executor, householdID, purchaseID gofrsuuid.UUID, merchantRuleKey string, ruleVersion int64, defaults []domain.Weight, items []Item) ([]gofrsuuid.UUID, error) {
	itemIDs := make([]gofrsuuid.UUID, 0, len(items))
	for _, item := range items {
		plan, err := prepareCreateItem(item)
		if err != nil {
			return nil, err
		}
		if plan.variantID != nil {
			variant, err := models.ProductVariants.Query(sm.Where(psql.And(
				models.ProductVariants.Columns.ID.EQ(psql.Arg(*plan.variantID)),
				models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			))).One(ctx, executor)
			if errors.Is(err, sql.ErrNoRows) || err == nil && variant.Archived {
				return nil, ErrNotFound
			}
			if err != nil {
				return nil, err
			}
			concept, err := models.ProductConcepts.Query(sm.Where(psql.And(
				models.ProductConcepts.Columns.ID.EQ(psql.Arg(variant.ConceptID)),
				models.ProductConcepts.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			))).One(ctx, executor)
			if errors.Is(err, sql.ErrNoRows) || err == nil && concept.Archived {
				return nil, ErrNotFound
			}
			if err != nil {
				return nil, err
			}
			if plan.categoryID == nil && concept.CategoryID.Valid {
				id := concept.CategoryID.V
				plan.categoryID = &id
			}
		}
		weights, appliedRuleID, appliedRuleVersion := defaults, (*gofrsuuid.UUID)(nil), ruleVersion
		if !plan.allocationOverride {
			ruleWeights, ruleID, revision, err := allocationRuleWeights(ctx, tx, householdID, plan.categoryID, plan.variantID, merchantRuleKey)
			if err != nil {
				return nil, err
			}
			if ruleID != nil {
				weights, appliedRuleID, appliedRuleVersion = ruleWeights, ruleID, revision
			}
		}
		plan, err = allocateCreateItem(plan, weights, appliedRuleID, appliedRuleVersion)
		if err != nil {
			return nil, err
		}
		itemID := newUUID()
		var categoryValue, variantValue sql.Null[gofrsuuid.UUID]
		if plan.categoryID != nil {
			categoryValue = sql.Null[gofrsuuid.UUID]{V: *plan.categoryID, Valid: true}
		}
		if plan.variantID != nil {
			variantValue = sql.Null[gofrsuuid.UUID]{V: *plan.variantID, Valid: true}
		}
		if _, err := models.PurchaseItems.Insert(&models.PurchaseItemSetter{
			ID: &itemID, HouseholdID: &householdID, PurchaseID: &purchaseID, Name: &plan.name,
			AmountYen: &plan.amountYen, CategoryID: &categoryValue, ProductVariantID: &variantValue, AllocationOverride: &plan.allocationOverride,
			QuantityNumerator: &plan.measure.quantityNumerator, QuantityDenominator: &plan.measure.quantityDenominator, QuantityUnit: &plan.measure.quantityUnit,
			PrintedUnitPriceYen: &plan.measure.printedUnitPriceYen, PrintedUnitPriceQuantityNumerator: &plan.measure.printedUnitPriceQuantityNumerator,
			PrintedUnitPriceQuantityDenominator: &plan.measure.printedUnitPriceQuantityDenominator, PrintedUnitPriceUnit: &plan.measure.printedUnitPriceUnit,
		}).Exec(ctx, executor); err != nil {
			return nil, err
		}
		itemIDs = append(itemIDs, itemID)
		for _, allocation := range plan.allocations {
			poolID, err := parseUUID(allocation.PoolID, "allocation pool")
			if err != nil {
				return nil, err
			}
			var ruleID sql.Null[gofrsuuid.UUID]
			if plan.ruleID != nil {
				ruleID = sql.Null[gofrsuuid.UUID]{V: *plan.ruleID, Valid: true}
			}
			if _, err := models.ItemAllocations.Insert(&models.ItemAllocationSetter{
				HouseholdID: &householdID, PurchaseID: &purchaseID, ItemID: &itemID, PoolID: &poolID,
				Weight: &allocation.Weight, AmountYen: &allocation.Amount, RuleVersion: &plan.ruleVersion, RuleID: &ruleID,
			}).Exec(ctx, executor); err != nil {
				return nil, err
			}
		}
	}
	return itemIDs, nil
}
