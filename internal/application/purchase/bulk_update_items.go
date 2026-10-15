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
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/txcleanup"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const (
	bulkItemFieldCategoryID       = "categoryId"
	bulkItemFieldProductVariantID = "productVariantId"
	bulkItemFieldAllocations      = "allocations"
	bulkItemFieldTags             = "tags"
)

type BulkItemTarget struct {
	ItemID       string
	BaseRevision int
}

type BulkItemPatch struct {
	CategoryIDSet       bool
	CategoryID          *string
	ProductVariantIDSet bool
	ProductVariantID    *string
	AllocationsSet      bool
	Allocations         []domain.Weight
	TagIDsSet           bool
	TagIDs              []string
}

type BulkUpdateItemsCommand struct {
	ActorID string
	Targets []BulkItemTarget
	Patch   BulkItemPatch
}

type BulkUpdateItemsResult struct {
	Items []BulkUpdatedItem
}

type BulkUpdatedItem struct {
	ID         string
	PurchaseID string
}

func (s *Service) BulkUpdateItems(ctx context.Context, command BulkUpdateItemsCommand) (BulkUpdateItemsResult, error) {
	if len(command.Targets) == 0 || len(command.Targets) > 100 {
		return BulkUpdateItemsResult{}, fmt.Errorf("%w: select between one and one hundred items", ErrBadInput)
	}
	fields := make([]string, 0, 4)
	if command.Patch.CategoryIDSet {
		fields = append(fields, bulkItemFieldCategoryID)
	}
	if command.Patch.ProductVariantIDSet {
		fields = append(fields, bulkItemFieldProductVariantID)
	}
	if command.Patch.AllocationsSet {
		fields = append(fields, bulkItemFieldAllocations)
	}
	if command.Patch.TagIDsSet {
		fields = append(fields, bulkItemFieldTags)
	}
	if len(fields) == 0 {
		return BulkUpdateItemsResult{}, fmt.Errorf("%w: patch must contain a field", ErrBadInput)
	}
	if command.Patch.AllocationsSet {
		if _, err := domain.Allocate(1, command.Patch.Allocations); err != nil {
			return BulkUpdateItemsResult{}, fmt.Errorf("%w: %w", ErrBadInput, err)
		}
	}
	actorID, err := parseUUID(command.ActorID, "actor")
	if err != nil {
		return BulkUpdateItemsResult{}, err
	}
	seen := make(map[string]struct{}, len(command.Targets))
	targetIDs := make([]gofrsuuid.UUID, len(command.Targets))
	for index, target := range command.Targets {
		if _, exists := seen[target.ItemID]; exists {
			return BulkUpdateItemsResult{}, fmt.Errorf("%w: duplicate item target", ErrBadInput)
		}
		seen[target.ItemID] = struct{}{}
		targetIDs[index], err = parseUUID(target.ItemID, "item")
		if err != nil {
			return BulkUpdateItemsResult{}, ErrNotFound
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return BulkUpdateItemsResult{}, err
	}
	defer txcleanup.Rollback(ctx, tx)
	executor := bobpgx.NewTx(tx, nil)
	observedItems, err := models.PurchaseItems.Query(
		sm.Where(models.PurchaseItems.Columns.ID.In(jsonUUIDArgs(targetIDs)...)),
		sm.OrderBy(models.PurchaseItems.Columns.ID),
	).All(ctx, executor)
	if err != nil {
		return BulkUpdateItemsResult{}, err
	}
	if len(observedItems) != len(targetIDs) {
		return BulkUpdateItemsResult{}, ErrNotFound
	}
	householdID := observedItems[0].HouseholdID
	purchaseIDs := make([]gofrsuuid.UUID, 0, len(targetIDs))
	purchaseIDSet := make(map[gofrsuuid.UUID]struct{}, len(targetIDs))
	observedByID := make(map[gofrsuuid.UUID]*models.PurchaseItem, len(observedItems))
	for _, item := range observedItems {
		if item.HouseholdID != householdID {
			return BulkUpdateItemsResult{}, fmt.Errorf("%w: all items must belong to one household", ErrBadInput)
		}
		observedByID[item.ID] = item
		if _, exists := purchaseIDSet[item.PurchaseID]; !exists {
			purchaseIDSet[item.PurchaseID] = struct{}{}
			purchaseIDs = append(purchaseIDs, item.PurchaseID)
		}
	}
	slices.SortFunc(purchaseIDs, func(left, right gofrsuuid.UUID) int {
		return strings.Compare(left.String(), right.String())
	})
	if err := requirePurchaseMember(ctx, executor, householdID, actorID); err != nil {
		return BulkUpdateItemsResult{}, err
	}
	observedPurchases, err := models.Purchases.Query(
		sm.Where(models.Purchases.Columns.ID.In(jsonUUIDArgs(purchaseIDs)...)),
		sm.OrderBy(models.Purchases.Columns.ID),
	).All(ctx, executor)
	if err != nil {
		return BulkUpdateItemsResult{}, err
	}
	if len(observedPurchases) != len(purchaseIDs) {
		return BulkUpdateItemsResult{}, ErrNotFound
	}
	observedPurchaseByID := make(map[gofrsuuid.UUID]*models.Purchase, len(observedPurchases))
	periods := make([]string, 0, len(observedPurchases))
	for _, purchase := range observedPurchases {
		observedPurchaseByID[purchase.ID] = purchase
		periods = append(periods, accounting.PeriodFor(purchase.OccurredAt))
	}
	if err := lockPurchaseAccountingPeriods(ctx, executor, householdID, periods...); err != nil {
		return BulkUpdateItemsResult{}, err
	}
	household, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForShare()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return BulkUpdateItemsResult{}, ErrNotFound
	}
	if err != nil {
		return BulkUpdateItemsResult{}, err
	}
	lockedPurchases, err := models.Purchases.Query(
		sm.Where(models.Purchases.Columns.ID.In(jsonUUIDArgs(purchaseIDs)...)),
		sm.OrderBy(models.Purchases.Columns.ID),
		sm.ForUpdate(),
	).All(ctx, executor)
	if err != nil {
		return BulkUpdateItemsResult{}, err
	}
	if len(lockedPurchases) != len(purchaseIDs) {
		return BulkUpdateItemsResult{}, ErrNotFound
	}
	purchases := make(map[gofrsuuid.UUID]*models.Purchase, len(lockedPurchases))
	for _, purchase := range lockedPurchases {
		observed := observedPurchaseByID[purchase.ID]
		if purchase.HouseholdID != householdID || !purchase.OccurredAt.Equal(observed.OccurredAt) {
			return BulkUpdateItemsResult{}, ErrConflict
		}
		if purchase.Status != draftStatus && purchase.Status != postedStatus {
			return BulkUpdateItemsResult{}, fmt.Errorf("%w: closed purchases require a correction", ErrConflict)
		}
		purchases[purchase.ID] = purchase
	}
	items, err := models.PurchaseItems.Query(
		sm.Where(models.PurchaseItems.Columns.ID.In(jsonUUIDArgs(targetIDs)...)),
		sm.OrderBy(models.PurchaseItems.Columns.ID),
		sm.ForUpdate(),
	).All(ctx, executor)
	if err != nil {
		return BulkUpdateItemsResult{}, err
	}
	if len(items) != len(targetIDs) {
		return BulkUpdateItemsResult{}, ErrNotFound
	}
	lockedItems := make(map[gofrsuuid.UUID]*models.PurchaseItem, len(items))
	for _, item := range items {
		observed := observedByID[item.ID]
		if item.HouseholdID != householdID || item.PurchaseID != observed.PurchaseID {
			return BulkUpdateItemsResult{}, ErrConflict
		}
		lockedItems[item.ID] = item
	}
	itemsInCommandOrder := make([]*models.PurchaseItem, len(targetIDs))
	for index, targetID := range targetIDs {
		item := lockedItems[targetID]
		if int(item.Revision) != command.Targets[index].BaseRevision {
			return BulkUpdateItemsResult{}, ErrConflict
		}
		itemsInCommandOrder[index] = item
	}
	for _, item := range itemsInCommandOrder {
		if err := s.updateBulkItem(ctx, tx, executor, item, purchases[item.PurchaseID], command.Patch, fields, household.DefaultAllocationVersion); err != nil {
			return BulkUpdateItemsResult{}, err
		}
		if err := accounting.RecordEvent(ctx, executor, householdID, item.ID, "purchase.item.updated", item.Revision+1, fields, actorID); err != nil {
			return BulkUpdateItemsResult{}, err
		}
	}
	for _, purchaseID := range purchaseIDs {
		purchase := purchases[purchaseID]
		if _, err := models.Purchases.Update(um.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchase.ID))), um.SetCol("revision").To(models.Purchases.Columns.Revision.Plus(psql.Arg(int64(1)))), um.SetCol("updated_at").To(psql.F("now"))).Exec(ctx, executor); err != nil {
			return BulkUpdateItemsResult{}, err
		}
		if purchase.Status == postedStatus {
			if err := syncPostedPurchaseAccounting(ctx, executor, purchase); err != nil {
				return BulkUpdateItemsResult{}, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return BulkUpdateItemsResult{}, err
	}
	result := BulkUpdateItemsResult{Items: make([]BulkUpdatedItem, 0, len(itemsInCommandOrder))}
	for _, item := range itemsInCommandOrder {
		result.Items = append(result.Items, BulkUpdatedItem{ID: item.ID.String(), PurchaseID: item.PurchaseID.String()})
	}
	return result, nil
}

func (s *Service) updateBulkItem(ctx context.Context, tx pgx.Tx, executor bob.Executor, item *models.PurchaseItem, purchase *models.Purchase, patch BulkItemPatch, fields []string, ruleVersion int64) error {
	categoryID := item.CategoryID
	if patch.CategoryIDSet {
		categoryID = sql.Null[gofrsuuid.UUID]{}
		if patch.CategoryID != nil {
			id, err := parseUUID(*patch.CategoryID, "category")
			if err != nil {
				return ErrNotFound
			}
			categoryID = sql.Null[gofrsuuid.UUID]{V: id, Valid: true}
		}
	}
	variantID := item.ProductVariantID
	if patch.ProductVariantIDSet {
		variantID = sql.Null[gofrsuuid.UUID]{}
		if patch.ProductVariantID != nil {
			id, err := parseUUID(*patch.ProductVariantID, "product variant")
			if err != nil {
				return ErrNotFound
			}
			_, err = models.ProductVariants.Query(sm.Where(psql.And(models.ProductVariants.Columns.ID.EQ(psql.Arg(id)), models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(item.HouseholdID)), models.ProductVariants.Columns.Archived.EQ(psql.Arg(false))))).One(ctx, executor)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
			variantID = sql.Null[gofrsuuid.UUID]{V: id, Valid: true}
		}
	}
	allocationOverride := item.AllocationOverride || patch.AllocationsSet
	plan, err := buildItemChangePlan(ctx, tx, executor, item, purchase, itemChangeInput{
		fields: fields, amount: item.AmountYen, categoryID: categoryID, productVariantID: variantID,
		allocationOverride: allocationOverride, categoryOrVariantChanged: patch.CategoryIDSet || patch.ProductVariantIDSet,
		allocationsSet: patch.AllocationsSet, requestedWeights: patch.Allocations,
		defaultAllocationVersion: ruleVersion,
	})
	if err != nil {
		return err
	}
	fieldValue := itemChangeFieldValue(plan)
	if _, err := models.PurchaseItems.Update((&models.PurchaseItemSetter{CategoryID: &plan.categoryID, ProductVariantID: &plan.productVariantID, AllocationOverride: &plan.allocationOverride, Revision: &plan.revision, FieldRevisions: &fieldValue}).UpdateMod(), um.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(item.ID))), um.SetCol("updated_at").To(psql.F("now"))).Exec(ctx, executor); err != nil {
		return err
	}
	if err := applyItemAllocationPlan(ctx, executor, item, plan); err != nil {
		return err
	}
	if patch.TagIDsSet {
		if _, err := models.PurchaseItemTags.Delete(dm.Where(models.PurchaseItemTags.Columns.ItemID.EQ(psql.Arg(item.ID)))).Exec(ctx, executor); err != nil {
			return err
		}
		seen := make(map[gofrsuuid.UUID]struct{}, len(patch.TagIDs))
		for _, tagID := range patch.TagIDs {
			id, err := parseUUID(tagID, "tag")
			if err != nil {
				return fmt.Errorf("%w: invalid tag id", ErrBadInput)
			}
			if _, exists := seen[id]; exists {
				return fmt.Errorf("%w: duplicate tag id", ErrBadInput)
			}
			seen[id] = struct{}{}
			tag, err := models.Tags.Query(sm.Where(psql.And(models.Tags.Columns.ID.EQ(psql.Arg(id)), models.Tags.Columns.HouseholdID.EQ(psql.Arg(item.HouseholdID)), models.Tags.Columns.Archived.EQ(psql.Arg(false))))).One(ctx, executor)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
			if _, err := models.PurchaseItemTags.Insert(&models.PurchaseItemTagSetter{HouseholdID: &item.HouseholdID, PurchaseID: &item.PurchaseID, ItemID: &item.ID, TagID: &tag.ID}).Exec(ctx, executor); err != nil {
				return err
			}
		}
	}
	return nil
}
