package graph

import (
	"context"
	"database/sql"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"slices"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob"
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

func (r *mutationResolver) bulkUpdatePurchaseItems(ctx context.Context, input model.BulkUpdatePurchaseItemsInput) ([]*model.PurchaseItem, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if len(input.Targets) == 0 || len(input.Targets) > 100 || input.Patch == nil {
		return nil, fmt.Errorf("%w: select between one and one hundred items", ErrBadInput)
	}
	changedFields, err := bulkPurchaseItemChangedFieldKeys(input.Patch)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(input.Targets))
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	items := make([]*models.PurchaseItem, 0, len(input.Targets))
	purchases := make(map[gofrsuuid.UUID]*models.Purchase)
	for _, target := range input.Targets {
		if target == nil {
			return nil, fmt.Errorf("%w: missing item target", ErrBadInput)
		}
		if _, ok := seen[target.ItemID]; ok {
			return nil, fmt.Errorf("%w: duplicate item target", ErrBadInput)
		}
		seen[target.ItemID] = struct{}{}
	}
	firstItemID, err := gofrsuuid.FromString(input.Targets[0].ItemID)
	if err != nil {
		return nil, ErrNotFound
	}
	firstItem, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(firstItemID)))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	householdID := firstItem.HouseholdID
	if _, err := requireMember(ctx, r.DB, householdID.String()); err != nil {
		return nil, err
	}
	household, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	for _, target := range input.Targets {
		id, parseErr := gofrsuuid.FromString(target.ItemID)
		if parseErr != nil {
			return nil, ErrNotFound
		}
		observedItem, queryErr := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(id)))).One(ctx, executor)
		if errors.Is(queryErr, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if queryErr != nil {
			return nil, queryErr
		}
		if householdID != observedItem.HouseholdID {
			return nil, fmt.Errorf("%w: all items must belong to one household", ErrBadInput)
		}
		if _, ok := purchases[observedItem.PurchaseID]; !ok {
			purchase, purchaseErr := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(observedItem.PurchaseID))), sm.ForUpdate()).One(ctx, executor)
			if purchaseErr != nil {
				return nil, purchaseErr
			}
			if purchase.Status == statusClosed {
				return nil, fmt.Errorf("%w: closed purchases require a correction", ErrConflict)
			}
			if err := lockAccountingPeriods(ctx, tx, householdID.String(), accountingPeriod(purchase.OccurredAt)); err != nil {
				return nil, err
			}
			purchases[observedItem.PurchaseID] = purchase
		}
		item, queryErr := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(id))), sm.ForUpdate()).One(ctx, executor)
		if errors.Is(queryErr, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if queryErr != nil {
			return nil, queryErr
		}
		if int(item.Revision) != target.BaseRevision {
			return nil, ErrConflict
		}
		items = append(items, item)
	}
	for _, item := range items {
		if _, err := updateBulkItem(ctx, tx, executor, item, purchases[item.PurchaseID], input, household.DefaultAllocationVersion); err != nil {
			return nil, err
		}
		if err := writeEvent(ctx, tx, householdID.String(), "purchase.item.updated", item.ID.String(), item.Revision+1, changedFields, user.MemberID); err != nil {
			return nil, err
		}
	}
	for _, purchase := range purchases {
		if _, err := models.Purchases.Update(um.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchase.ID))), um.SetCol("revision").To(models.Purchases.Columns.Revision.Plus(psql.Arg(int64(1)))), um.SetCol("updated_at").To(psql.F("now"))).Exec(ctx, executor); err != nil {
			return nil, err
		}
		if purchase.Status == statusPosted {
			if err := syncPostedPurchaseAccounting(ctx, tx, purchase.ID.String()); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	result := make([]*model.PurchaseItem, 0, len(items))
	for _, item := range items {
		loaded, err := loadPurchase(ctx, r.DB, item.PurchaseID.String())
		if err != nil {
			return nil, err
		}
		for _, updated := range loaded.Items {
			if updated.ID == item.ID.String() {
				result = append(result, updated)
				break
			}
		}
	}
	return result, nil
}

func updateBulkItem(ctx context.Context, tx pgx.Tx, executor bob.Executor, item *models.PurchaseItem, purchase *models.Purchase, input model.BulkUpdatePurchaseItemsInput, ruleVersion int64) (int64, error) {
	changed, err := bulkPurchaseItemChangedFieldKeys(input.Patch)
	if err != nil {
		return 0, err
	}
	newRevision := item.Revision + 1
	fieldRevisions, err := setFieldRevisions(item.FieldRevisions.Val, changed, newRevision)
	if err != nil {
		return 0, err
	}
	categoryValue := item.CategoryID
	if slices.Contains(changed, fieldCategoryID) {
		categoryValue = sql.Null[gofrsuuid.UUID]{}
		if input.Patch.CategoryID.Value() != nil {
			id, err := gofrsuuid.FromString(*input.Patch.CategoryID.Value())
			if err != nil {
				return 0, ErrNotFound
			}
			categoryValue = sql.Null[gofrsuuid.UUID]{V: id, Valid: true}
		}
	}
	variantValue := item.ProductVariantID
	if slices.Contains(changed, fieldProductVariantID) {
		variantValue = sql.Null[gofrsuuid.UUID]{}
		if input.Patch.ProductVariantID.Value() != nil {
			id, err := gofrsuuid.FromString(*input.Patch.ProductVariantID.Value())
			if err != nil {
				return 0, ErrNotFound
			}
			variant, err := models.ProductVariants.Query(sm.Where(psql.And(models.ProductVariants.Columns.ID.EQ(psql.Arg(id)), models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(item.HouseholdID)), models.ProductVariants.Columns.Archived.EQ(psql.Arg(false))))).One(ctx, executor)
			if errors.Is(err, sql.ErrNoRows) || err == nil && variant.Archived {
				return 0, ErrNotFound
			}
			if err != nil {
				return 0, err
			}
			variantValue = sql.Null[gofrsuuid.UUID]{V: id, Valid: true}
		}
	}
	fieldValue := types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(fieldRevisions)}
	allocationOverride := item.AllocationOverride || slices.Contains(changed, fieldAllocations)
	allocationChanged := slices.Contains(changed, fieldAllocations) || !item.AllocationOverride && (slices.Contains(changed, fieldCategoryID) || slices.Contains(changed, fieldProductVariantID))
	var allocated []domain.AllocationAmount
	var weights []domain.Weight
	var appliedRuleID *string
	var appliedRuleVersion = ruleVersion
	if allocationChanged {
		if slices.Contains(changed, fieldAllocations) {
			weights, err = allocationWeights(input.Patch.Allocations.Value())
		} else {
			var categoryID, variantID *string
			if categoryValue.Valid {
				categoryID = new(categoryValue.V.String())
			}
			if variantValue.Valid {
				variantID = new(variantValue.V.String())
			}
			weights, appliedRuleID, appliedRuleVersion, err = allocationRuleWeights(ctx, tx, item.HouseholdID.String(), categoryID, variantID, normalizeMerchant(purchase.Merchant))
			if err == nil && appliedRuleID == nil {
				weights, err = loadWeights(ctx, tx, item.HouseholdID.String(), ruleVersion)
			}
		}
		if err != nil {
			return 0, err
		}
		allocated, err = domain.Allocate(item.AmountYen, weights)
		if err != nil {
			return 0, fmt.Errorf("%w: %w", ErrBadInput, err)
		}
	}
	if _, err := models.PurchaseItems.Update((&models.PurchaseItemSetter{CategoryID: &categoryValue, ProductVariantID: &variantValue, AllocationOverride: &allocationOverride, Revision: &newRevision, FieldRevisions: &fieldValue}).UpdateMod(), um.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(item.ID))), um.SetCol("updated_at").To(psql.F("now"))).Exec(ctx, executor); err != nil {
		return 0, err
	}
	if allocationChanged {
		if _, err := models.ItemAllocations.Delete(dm.Where(models.ItemAllocations.Columns.ItemID.EQ(psql.Arg(item.ID)))).Exec(ctx, executor); err != nil {
			return 0, err
		}
		for _, allocation := range allocated {
			poolID := generatedUUID(allocation.PoolID)
			var ruleID sql.Null[gofrsuuid.UUID]
			if appliedRuleID != nil {
				ruleID = sql.Null[gofrsuuid.UUID]{V: generatedUUID(*appliedRuleID), Valid: true}
			}
			if _, err := models.ItemAllocations.Insert(&models.ItemAllocationSetter{HouseholdID: &item.HouseholdID, PurchaseID: &item.PurchaseID, ItemID: &item.ID, PoolID: &poolID, Weight: &allocation.Weight, AmountYen: &allocation.Amount, RuleVersion: &appliedRuleVersion, RuleID: &ruleID}).Exec(ctx, executor); err != nil {
				return 0, err
			}
		}
	}
	if slices.Contains(changed, fieldTags) {
		if _, err := models.PurchaseItemTags.Delete(dm.Where(models.PurchaseItemTags.Columns.ItemID.EQ(psql.Arg(item.ID)))).Exec(ctx, executor); err != nil {
			return 0, err
		}
		tagIDs := input.Patch.TagIds.Value()
		seen := make(map[gofrsuuid.UUID]struct{}, len(tagIDs))
		for _, tagID := range tagIDs {
			id, err := gofrsuuid.FromString(tagID)
			if err != nil {
				return 0, fmt.Errorf("%w: invalid tag id", ErrBadInput)
			}
			if _, ok := seen[id]; ok {
				return 0, fmt.Errorf("%w: duplicate tag id", ErrBadInput)
			}
			seen[id] = struct{}{}
			tag, err := models.Tags.Query(sm.Where(psql.And(models.Tags.Columns.ID.EQ(psql.Arg(id)), models.Tags.Columns.HouseholdID.EQ(psql.Arg(item.HouseholdID)), models.Tags.Columns.Archived.EQ(psql.Arg(false))))).One(ctx, executor)
			if errors.Is(err, sql.ErrNoRows) {
				return 0, ErrNotFound
			}
			if err != nil {
				return 0, err
			}
			if _, err := models.PurchaseItemTags.Insert(&models.PurchaseItemTagSetter{HouseholdID: &item.HouseholdID, PurchaseID: &item.PurchaseID, ItemID: &item.ID, TagID: &tag.ID}).Exec(ctx, executor); err != nil {
				return 0, err
			}
		}
	}
	return newRevision, nil
}
