package purchase

import (
	"context"
	"database/sql"
	stdjson "encoding/json"
	"errors"
	"fmt"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/application/accounting"
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
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	first, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(targetIDs[0])))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return BulkUpdateItemsResult{}, ErrNotFound
	}
	if err != nil {
		return BulkUpdateItemsResult{}, err
	}
	householdID := first.HouseholdID
	if err := requirePurchaseMember(ctx, executor, householdID, actorID); err != nil {
		return BulkUpdateItemsResult{}, err
	}
	household, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return BulkUpdateItemsResult{}, ErrNotFound
	}
	if err != nil {
		return BulkUpdateItemsResult{}, err
	}
	items := make([]*models.PurchaseItem, 0, len(command.Targets))
	purchases := make(map[gofrsuuid.UUID]*models.Purchase)
	for index, target := range command.Targets {
		observed, queryErr := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(targetIDs[index])))).One(ctx, executor)
		if errors.Is(queryErr, sql.ErrNoRows) {
			return BulkUpdateItemsResult{}, ErrNotFound
		}
		if queryErr != nil {
			return BulkUpdateItemsResult{}, queryErr
		}
		if observed.HouseholdID != householdID {
			return BulkUpdateItemsResult{}, fmt.Errorf("%w: all items must belong to one household", ErrBadInput)
		}
		purchase, ok := purchases[observed.PurchaseID]
		if !ok {
			purchase, queryErr = models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(observed.PurchaseID))), sm.ForUpdate()).One(ctx, executor)
			if queryErr != nil {
				if errors.Is(queryErr, sql.ErrNoRows) {
					return BulkUpdateItemsResult{}, ErrNotFound
				}
				return BulkUpdateItemsResult{}, queryErr
			}
			if purchase.Status == correctionStatusClosed {
				return BulkUpdateItemsResult{}, fmt.Errorf("%w: closed purchases require a correction", ErrConflict)
			}
			if err := lockPurchaseAccountingPeriods(ctx, executor, householdID, accounting.PeriodFor(purchase.OccurredAt)); err != nil {
				return BulkUpdateItemsResult{}, err
			}
			purchases[observed.PurchaseID] = purchase
		}
		item, queryErr := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(targetIDs[index]))), sm.ForUpdate()).One(ctx, executor)
		if errors.Is(queryErr, sql.ErrNoRows) {
			return BulkUpdateItemsResult{}, ErrNotFound
		}
		if queryErr != nil {
			return BulkUpdateItemsResult{}, queryErr
		}
		if int(item.Revision) != target.BaseRevision {
			return BulkUpdateItemsResult{}, ErrConflict
		}
		items = append(items, item)
	}
	for _, item := range items {
		if err := s.updateBulkItem(ctx, tx, executor, item, purchases[item.PurchaseID], command.Patch, fields, household.DefaultAllocationVersion); err != nil {
			return BulkUpdateItemsResult{}, err
		}
		if err := accounting.RecordEvent(ctx, executor, householdID, item.ID, "purchase.item.updated", item.Revision+1, fields, actorID); err != nil {
			return BulkUpdateItemsResult{}, err
		}
	}
	for _, purchase := range purchases {
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
	result := BulkUpdateItemsResult{Items: make([]BulkUpdatedItem, 0, len(items))}
	for _, item := range items {
		result.Items = append(result.Items, BulkUpdatedItem{ID: item.ID.String(), PurchaseID: item.PurchaseID.String()})
	}
	return result, nil
}

func (s *Service) updateBulkItem(ctx context.Context, tx pgx.Tx, executor bob.Executor, item *models.PurchaseItem, purchase *models.Purchase, patch BulkItemPatch, fields []string, ruleVersion int64) error {
	newRevision := item.Revision + 1
	fieldRevisions, err := purchaseUpdateFieldRevisions(item.FieldRevisions.Val, fields, newRevision)
	if err != nil {
		return err
	}
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
	fieldValue := types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(fieldRevisions)}
	allocationOverride := item.AllocationOverride || patch.AllocationsSet
	allocationChanged := patch.AllocationsSet || !item.AllocationOverride && (patch.CategoryIDSet || patch.ProductVariantIDSet)
	var allocations []domain.AllocationAmount
	var weights []domain.Weight
	var appliedRuleID *gofrsuuid.UUID
	appliedRuleVersion := ruleVersion
	if allocationChanged {
		if patch.AllocationsSet {
			weights = patch.Allocations
		} else {
			var category, variant *gofrsuuid.UUID
			if categoryID.Valid {
				category = &categoryID.V
			}
			if variantID.Valid {
				variant = &variantID.V
			}
			weights, appliedRuleID, appliedRuleVersion, err = allocationRuleWeights(ctx, tx, item.HouseholdID, category, variant, normalizeMerchant(purchase.Merchant))
			if err == nil && appliedRuleID == nil {
				weights, err = loadWeights(ctx, tx, item.HouseholdID, ruleVersion)
			}
		}
		if err != nil {
			return err
		}
		allocations, err = domain.Allocate(item.AmountYen, weights)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrBadInput, err)
		}
	}
	if _, err := models.PurchaseItems.Update((&models.PurchaseItemSetter{CategoryID: &categoryID, ProductVariantID: &variantID, AllocationOverride: &allocationOverride, Revision: &newRevision, FieldRevisions: &fieldValue}).UpdateMod(), um.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(item.ID))), um.SetCol("updated_at").To(psql.F("now"))).Exec(ctx, executor); err != nil {
		return err
	}
	if allocationChanged {
		if _, err := models.ItemAllocations.Delete(dm.Where(models.ItemAllocations.Columns.ItemID.EQ(psql.Arg(item.ID)))).Exec(ctx, executor); err != nil {
			return err
		}
		for _, allocation := range allocations {
			poolID, err := parseUUID(allocation.PoolID, "pool")
			if err != nil {
				return fmt.Errorf("%w: invalid allocation pool", ErrBadInput)
			}
			var ruleID sql.Null[gofrsuuid.UUID]
			if appliedRuleID != nil {
				ruleID = sql.Null[gofrsuuid.UUID]{V: *appliedRuleID, Valid: true}
			}
			if _, err := models.ItemAllocations.Insert(&models.ItemAllocationSetter{HouseholdID: &item.HouseholdID, PurchaseID: &item.PurchaseID, ItemID: &item.ID, PoolID: &poolID, Weight: &allocation.Weight, AmountYen: &allocation.Amount, RuleVersion: &appliedRuleVersion, RuleID: &ruleID}).Exec(ctx, executor); err != nil {
				return err
			}
		}
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
