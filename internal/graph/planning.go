package graph

import (
	"context"
	"database/sql"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

func (r *mutationResolver) createTag(ctx context.Context, input model.CreateTagInput) (*model.Tag, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 80 {
		return nil, fmt.Errorf("%w: invalid tag name", ErrBadInput)
	}
	householdID, err := gofrsuuid.FromString(input.HouseholdID)
	if err != nil {
		return nil, ErrNotFound
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	id := generatedUUID(newID())
	revision := int64(1)
	row, err := models.Tags.Insert(&models.TagSetter{ID: &id, HouseholdID: &householdID, Name: &name, Revision: &revision}).One(ctx, executor)
	if err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "tag.created", id.String(), revision, []string{"name"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return tagModel(row), nil
}

func (r *mutationResolver) updateTag(ctx context.Context, input model.UpdateTagInput) (*model.Tag, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	id, err := gofrsuuid.FromString(input.TagID)
	if err != nil {
		return nil, ErrNotFound
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 80 {
		return nil, fmt.Errorf("%w: invalid tag name", ErrBadInput)
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	tag, err := models.Tags.Query(sm.Where(models.Tags.Columns.ID.EQ(psql.Arg(id))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if _, err := requireMember(ctx, r.DB, tag.HouseholdID.String()); err != nil {
		return nil, err
	}
	if int(tag.Revision) != input.BaseRevision {
		return nil, ErrConflict
	}
	revision := tag.Revision + 1
	if _, err := models.Tags.Update((&models.TagSetter{Name: &name, Revision: &revision}).UpdateMod(), um.Where(models.Tags.Columns.ID.EQ(psql.Arg(id)))).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, tag.HouseholdID.String(), "tag.updated", input.TagID, revision, []string{"name"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tag.Name, tag.Revision = name, revision
	return tagModel(tag), nil
}

func (r *mutationResolver) archiveTag(ctx context.Context, tagID string, baseRevision int, archived bool) (*model.Tag, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	id, err := gofrsuuid.FromString(tagID)
	if err != nil {
		return nil, ErrNotFound
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	tag, err := models.Tags.Query(sm.Where(models.Tags.Columns.ID.EQ(psql.Arg(id))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if _, err := requireMember(ctx, r.DB, tag.HouseholdID.String()); err != nil {
		return nil, err
	}
	if int(tag.Revision) != baseRevision {
		return nil, ErrConflict
	}
	revision := tag.Revision + 1
	if _, err := models.Tags.Update((&models.TagSetter{Archived: &archived, Revision: &revision}).UpdateMod(), um.Where(models.Tags.Columns.ID.EQ(psql.Arg(id)))).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, tag.HouseholdID.String(), "tag.archived", tagID, revision, []string{"archived"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tag.Archived, tag.Revision = archived, revision
	return tagModel(tag), nil
}

func tagModel(row *models.Tag) *model.Tag {
	return &model.Tag{ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), Name: row.Name, Archived: row.Archived, Revision: int(row.Revision)}
}

func (r *queryResolver) tags(ctx context.Context, householdID string, includeArchived bool) ([]*model.Tag, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, ErrNotFound
	}
	var rows models.TagSlice
	if includeArchived {
		rows, err = models.Tags.Query(sm.Where(models.Tags.Columns.HouseholdID.EQ(psql.Arg(hhID))), sm.OrderBy(models.Tags.Columns.Name)).All(ctx, bobpgx.NewPool(r.DB))
	} else {
		rows, err = models.Tags.Query(sm.Where(psql.And(models.Tags.Columns.HouseholdID.EQ(psql.Arg(hhID)), models.Tags.Columns.Archived.EQ(psql.Arg(false)))), sm.OrderBy(models.Tags.Columns.Name)).All(ctx, bobpgx.NewPool(r.DB))
	}
	if err != nil {
		return nil, err
	}
	result := make([]*model.Tag, 0, len(rows))
	for _, row := range rows {
		result = append(result, tagModel(row))
	}
	return result, nil
}

func (r *mutationResolver) bulkUpdatePurchaseItems(ctx context.Context, input model.BulkUpdatePurchaseItemsInput) ([]*model.PurchaseItem, error) {
	changedFields := bulkPurchaseItemChangedFieldKeys(input.ChangedFields)
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if len(input.Targets) == 0 || len(input.Targets) > 100 || input.Patch == nil {
		return nil, fmt.Errorf("%w: select between one and one hundred items", ErrBadInput)
	}
	if err := validateChangedFields(changedFields, "categoryId", "productVariantId", "allocations", "tags"); err != nil {
		return nil, err
	}
	for _, field := range changedFields {
		if field == "allocations" && input.Patch.Allocations == nil || field == "tags" && input.Patch.TagIds == nil {
			return nil, fmt.Errorf("%w: changed field has no value", ErrBadInput)
		}
	}
	if input.Patch.CategoryID != nil && !slices.Contains(changedFields, "categoryId") || input.Patch.ProductVariantID != nil && !slices.Contains(changedFields, "productVariantId") || input.Patch.Allocations != nil && !slices.Contains(changedFields, "allocations") || input.Patch.TagIds != nil && !slices.Contains(changedFields, "tags") {
		return nil, fmt.Errorf("%w: patch contains fields missing from changedFields", ErrBadInput)
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
			if purchase.Status == "CLOSED" {
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
		if purchase.Status == "POSTED" {
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
	changed := bulkPurchaseItemChangedFieldKeys(input.ChangedFields)
	newRevision := item.Revision + 1
	fieldRevisions, err := setFieldRevisions(item.FieldRevisions.Val, changed, newRevision)
	if err != nil {
		return 0, err
	}
	categoryValue := item.CategoryID
	if slices.Contains(changed, "categoryId") {
		categoryValue = sql.Null[gofrsuuid.UUID]{}
		if input.Patch.CategoryID != nil {
			id, err := gofrsuuid.FromString(*input.Patch.CategoryID)
			if err != nil {
				return 0, ErrNotFound
			}
			categoryValue = sql.Null[gofrsuuid.UUID]{V: id, Valid: true}
		}
	}
	variantValue := item.ProductVariantID
	if slices.Contains(changed, "productVariantId") {
		variantValue = sql.Null[gofrsuuid.UUID]{}
		if input.Patch.ProductVariantID != nil {
			id, err := gofrsuuid.FromString(*input.Patch.ProductVariantID)
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
	allocationOverride := item.AllocationOverride || slices.Contains(changed, "allocations")
	allocationChanged := slices.Contains(changed, "allocations") || !item.AllocationOverride && (slices.Contains(changed, "categoryId") || slices.Contains(changed, "productVariantId"))
	var allocated []domain.AllocationAmount
	var weights []domain.Weight
	var appliedRuleID *string
	var appliedRuleVersion = ruleVersion
	if allocationChanged {
		if slices.Contains(changed, "allocations") {
			weights, err = allocationWeights(input.Patch.Allocations)
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
	if slices.Contains(changed, "tags") {
		if _, err := models.PurchaseItemTags.Delete(dm.Where(models.PurchaseItemTags.Columns.ItemID.EQ(psql.Arg(item.ID)))).Exec(ctx, executor); err != nil {
			return 0, err
		}
		seen := make(map[gofrsuuid.UUID]struct{}, len(input.Patch.TagIds))
		for _, tagID := range input.Patch.TagIds {
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

func (r *mutationResolver) setBudget(ctx context.Context, input model.SetBudgetInput) (*model.Budget, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 120 || input.AmountYen <= 0 || int64(input.AmountYen) > maxMoney {
		return nil, fmt.Errorf("%w: invalid budget", ErrBadInput)
	}
	starts, err := parseDate(input.StartsOn)
	if err != nil {
		return nil, err
	}
	ends := sql.Null[time.Time]{}
	if input.EndsOn != nil {
		end, err := parseDate(*input.EndsOn)
		if err != nil || end.Before(starts) {
			return nil, fmt.Errorf("%w: invalid budget end date", ErrBadInput)
		}
		ends = sql.Null[time.Time]{V: end, Valid: true}
	}
	hhID, err := gofrsuuid.FromString(input.HouseholdID)
	if err != nil {
		return nil, ErrNotFound
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(hhID))), sm.ForUpdate()).One(ctx, executor); err != nil {
		return nil, err
	}
	var row *models.Budget
	if input.BudgetID == nil {
		id := generatedUUID(newID())
		amount := int64(input.AmountYen)
		var category sql.Null[gofrsuuid.UUID]
		if input.CategoryID != nil {
			category = sql.Null[gofrsuuid.UUID]{V: generatedUUID(*input.CategoryID), Valid: true}
		}
		row, err = models.Budgets.Insert(&models.BudgetSetter{ID: &id, HouseholdID: &hhID, CategoryID: &category, Name: &name, AmountYen: &amount, StartsOn: &starts, EndsOn: &ends}).One(ctx, executor)
	} else {
		id, parseErr := gofrsuuid.FromString(*input.BudgetID)
		if parseErr != nil || input.BaseRevision == nil {
			return nil, fmt.Errorf("%w: budget update requires id and revision", ErrBadInput)
		}
		row, err = models.Budgets.Query(sm.Where(psql.And(models.Budgets.Columns.ID.EQ(psql.Arg(id)), models.Budgets.Columns.HouseholdID.EQ(psql.Arg(hhID)))), sm.ForUpdate()).One(ctx, executor)
		if err == nil && int(row.Revision) != *input.BaseRevision {
			return nil, ErrConflict
		}
		if err == nil {
			amount, revision := int64(input.AmountYen), row.Revision+1
			var category sql.Null[gofrsuuid.UUID]
			if input.CategoryID != nil {
				category = sql.Null[gofrsuuid.UUID]{V: generatedUUID(*input.CategoryID), Valid: true}
			}
			_, err = models.Budgets.Update((&models.BudgetSetter{CategoryID: &category, Name: &name, AmountYen: &amount, StartsOn: &starts, EndsOn: &ends, Revision: &revision}).UpdateMod(), um.Where(models.Budgets.Columns.ID.EQ(psql.Arg(id))), um.SetCol("updated_at").To(psql.F("now"))).Exec(ctx, executor)
			if err == nil {
				row, err = models.Budgets.Query(sm.Where(models.Budgets.Columns.ID.EQ(psql.Arg(id)))).One(ctx, executor)
			}
		}
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "budget.updated", row.ID.String(), row.Revision, []string{"name", "amountYen", "categoryId", "startsOn", "endsOn"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return budgetModel(ctx, r.DB, row, nil)
}

func (r *mutationResolver) archiveBudget(ctx context.Context, budgetID string, baseRevision int, archived bool) (*model.Budget, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	id, err := gofrsuuid.FromString(budgetID)
	if err != nil {
		return nil, ErrNotFound
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	row, err := models.Budgets.Query(sm.Where(models.Budgets.Columns.ID.EQ(psql.Arg(id))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if _, err := requireMember(ctx, r.DB, row.HouseholdID.String()); err != nil {
		return nil, err
	}
	if int(row.Revision) != baseRevision {
		return nil, ErrConflict
	}
	revision := row.Revision + 1
	if _, err := models.Budgets.Update((&models.BudgetSetter{Archived: &archived, Revision: &revision}).UpdateMod(), um.Where(models.Budgets.Columns.ID.EQ(psql.Arg(id)))).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, row.HouseholdID.String(), "budget.archived", budgetID, revision, []string{"archived"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	row.Archived, row.Revision = archived, revision
	return budgetModel(ctx, r.DB, row, nil)
}

func (r *queryResolver) budgets(ctx context.Context, householdID, period string) ([]*model.Budget, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	month, err := parseAccountingPeriod(period)
	if err != nil {
		return nil, fmt.Errorf("%w: period must use YYYY-MM", ErrBadInput)
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, ErrNotFound
	}
	rows, err := models.Budgets.Query(sm.Where(psql.And(models.Budgets.Columns.HouseholdID.EQ(psql.Arg(hhID)), models.Budgets.Columns.Archived.EQ(psql.Arg(false)), models.Budgets.Columns.StartsOn.LT(psql.Arg(month.AddDate(0, 1, 0))), psql.Or(models.Budgets.Columns.EndsOn.IsNull(), models.Budgets.Columns.EndsOn.GTE(psql.Arg(month))))), sm.OrderBy(models.Budgets.Columns.Name)).All(ctx, queryExecutor(r.DB))
	if err != nil {
		return nil, err
	}
	result := make([]*model.Budget, 0, len(rows))
	for _, row := range rows {
		budget, err := budgetModel(ctx, r.DB, row, &month)
		if err != nil {
			return nil, err
		}
		result = append(result, budget)
	}
	return result, nil
}

func budgetModel(ctx context.Context, db *pgxpool.Pool, row *models.Budget, month *time.Time) (*model.Budget, error) {
	result := &model.Budget{ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), Name: row.Name, AmountYen: graphLong(row.AmountYen), StartsOn: row.StartsOn.Format(time.DateOnly), Archived: row.Archived, Revision: int(row.Revision)}
	if row.CategoryID.Valid {
		id := row.CategoryID.V.String()
		result.CategoryID = &id
	}
	if row.EndsOn.Valid {
		end := row.EndsOn.V.Format(time.DateOnly)
		result.EndsOn = &end
	}
	if month == nil {
		return result, nil
	}
	start, end := month.UTC(), month.AddDate(0, 1, 0).UTC()
	purchases, err := models.Purchases.Query(sm.Where(psql.And(models.Purchases.Columns.HouseholdID.EQ(psql.Arg(row.HouseholdID)), models.Purchases.Columns.OccurredAt.GTE(psql.Arg(start)), models.Purchases.Columns.OccurredAt.LT(psql.Arg(end)), psql.Or(models.Purchases.Columns.Status.EQ(psql.Arg("POSTED")), models.Purchases.Columns.Status.EQ(psql.Arg("CLOSED")))))).All(ctx, queryExecutor(db))
	if err != nil {
		return nil, err
	}
	var actual int64
	for _, purchase := range purchases {
		items, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchase.ID)))).All(ctx, queryExecutor(db))
		if err != nil {
			return nil, err
		}
		itemsByID := make(map[gofrsuuid.UUID]*models.PurchaseItem, len(items))
		for _, item := range items {
			itemsByID[item.ID] = item
			if row.CategoryID.Valid && (!item.CategoryID.Valid || row.CategoryID.V != item.CategoryID.V) {
				continue
			}
			var ok bool
			actual, ok = checkedAdd(actual, item.AmountYen)
			if !ok {
				return nil, fmt.Errorf("%w: budget actual exceeds int64", ErrBadInput)
			}
		}
		corrections, err := models.Corrections.Query(sm.Where(models.Corrections.Columns.PurchaseID.EQ(psql.Arg(purchase.ID)))).All(ctx, queryExecutor(db))
		if err != nil {
			return nil, err
		}
		for _, correction := range corrections {
			var categoryID sql.Null[gofrsuuid.UUID]
			if correction.ItemID.Valid {
				if item, ok := itemsByID[correction.ItemID.V]; ok {
					categoryID = item.CategoryID
				}
			}
			if row.CategoryID.Valid && (!categoryID.Valid || row.CategoryID.V != categoryID.V) {
				continue
			}
			var ok bool
			actual, ok = checkedAdd(actual, correction.AmountDeltaYen)
			if !ok {
				return nil, fmt.Errorf("%w: budget actual exceeds int64", ErrBadInput)
			}
		}
	}
	result.ActualYen = graphLong(actual)
	result.RemainingYen = graphLong(row.AmountYen - actual)
	return result, nil
}

func (r *mutationResolver) setRecurringExpense(ctx context.Context, input model.SetRecurringExpenseInput) (*model.RecurringExpense, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	name, merchant := strings.TrimSpace(input.Name), strings.TrimSpace(input.Merchant)
	if name == "" || len(name) > 160 || len(merchant) > 240 || input.AmountYen <= 0 || int64(input.AmountYen) > maxMoney || !slices.Contains([]string{"WEEKLY", "MONTHLY", "YEARLY"}, input.Cadence) {
		return nil, fmt.Errorf("%w: invalid recurring expense", ErrBadInput)
	}
	starts, err := parseDate(input.StartsOn)
	if err != nil {
		return nil, err
	}
	due, err := parseDate(input.NextDueOn)
	if err != nil || due.Before(starts) {
		return nil, fmt.Errorf("%w: next due date precedes start date", ErrBadInput)
	}
	ends := sql.Null[time.Time]{}
	if input.EndsOn != nil {
		end, err := parseDate(*input.EndsOn)
		if err != nil || end.Before(starts) {
			return nil, fmt.Errorf("%w: invalid recurring expense end date", ErrBadInput)
		}
		ends = sql.Null[time.Time]{V: end, Valid: true}
	}
	hhID, err := gofrsuuid.FromString(input.HouseholdID)
	if err != nil {
		return nil, ErrNotFound
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(hhID))), sm.ForUpdate()).One(ctx, executor); err != nil {
		return nil, err
	}
	var row *models.RecurringExpense
	var category, account sql.Null[gofrsuuid.UUID]
	if input.CategoryID != nil {
		category = sql.Null[gofrsuuid.UUID]{V: generatedUUID(*input.CategoryID), Valid: true}
	}
	if input.PaymentAccountID != nil {
		account = sql.Null[gofrsuuid.UUID]{V: generatedUUID(*input.PaymentAccountID), Valid: true}
	}
	amount := int64(input.AmountYen)
	if input.ExpenseID == nil {
		id := generatedUUID(newID())
		row, err = models.RecurringExpenses.Insert(&models.RecurringExpenseSetter{ID: &id, HouseholdID: &hhID, Name: &name, Merchant: &merchant, AmountYen: &amount, CategoryID: &category, PaymentAccountID: &account, Cadence: &input.Cadence, StartsOn: &starts, NextDueOn: &due, EndsOn: &ends}).One(ctx, executor)
	} else {
		id, parseErr := gofrsuuid.FromString(*input.ExpenseID)
		if parseErr != nil || input.BaseRevision == nil {
			return nil, fmt.Errorf("%w: recurring expense update requires id and revision", ErrBadInput)
		}
		row, err = models.RecurringExpenses.Query(sm.Where(psql.And(models.RecurringExpenses.Columns.ID.EQ(psql.Arg(id)), models.RecurringExpenses.Columns.HouseholdID.EQ(psql.Arg(hhID)))), sm.ForUpdate()).One(ctx, executor)
		if err == nil && int(row.Revision) != *input.BaseRevision {
			return nil, ErrConflict
		}
		if err == nil {
			revision := row.Revision + 1
			_, err = models.RecurringExpenses.Update((&models.RecurringExpenseSetter{Name: &name, Merchant: &merchant, AmountYen: &amount, CategoryID: &category, PaymentAccountID: &account, Cadence: &input.Cadence, StartsOn: &starts, NextDueOn: &due, EndsOn: &ends, Revision: &revision}).UpdateMod(), um.Where(models.RecurringExpenses.Columns.ID.EQ(psql.Arg(id))), um.SetCol("updated_at").To(psql.F("now"))).Exec(ctx, executor)
			if err == nil {
				row, err = models.RecurringExpenses.Query(sm.Where(models.RecurringExpenses.Columns.ID.EQ(psql.Arg(id)))).One(ctx, executor)
			}
		}
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "recurring_expense.updated", row.ID.String(), row.Revision, []string{"name", "merchant", "amountYen", "categoryId", "paymentAccountId", "cadence", "startsOn", "nextDueOn", "endsOn"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return recurringExpenseModel(row), nil
}

func (r *mutationResolver) archiveRecurringExpense(ctx context.Context, expenseID string, baseRevision int, archived bool) (*model.RecurringExpense, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	id, err := gofrsuuid.FromString(expenseID)
	if err != nil {
		return nil, ErrNotFound
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	row, err := models.RecurringExpenses.Query(sm.Where(models.RecurringExpenses.Columns.ID.EQ(psql.Arg(id))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if _, err := requireMember(ctx, r.DB, row.HouseholdID.String()); err != nil {
		return nil, err
	}
	if int(row.Revision) != baseRevision {
		return nil, ErrConflict
	}
	revision := row.Revision + 1
	if _, err := models.RecurringExpenses.Update((&models.RecurringExpenseSetter{Archived: &archived, Revision: &revision}).UpdateMod(), um.Where(models.RecurringExpenses.Columns.ID.EQ(psql.Arg(id)))).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, row.HouseholdID.String(), "recurring_expense.archived", expenseID, revision, []string{"archived"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	row.Archived, row.Revision = archived, revision
	return recurringExpenseModel(row), nil
}

func recurringExpenseModel(row *models.RecurringExpense) *model.RecurringExpense {
	result := &model.RecurringExpense{ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), Name: row.Name, Merchant: row.Merchant, AmountYen: graphLong(row.AmountYen), Currency: row.Currency, Cadence: row.Cadence, StartsOn: row.StartsOn.Format(time.DateOnly), NextDueOn: row.NextDueOn.Format(time.DateOnly), Archived: row.Archived, Revision: int(row.Revision)}
	if row.CategoryID.Valid {
		id := row.CategoryID.V.String()
		result.CategoryID = &id
	}
	if row.PaymentAccountID.Valid {
		id := row.PaymentAccountID.V.String()
		result.PaymentAccountID = &id
	}
	if row.EndsOn.Valid {
		date := row.EndsOn.V.Format(time.DateOnly)
		result.EndsOn = &date
	}
	return result
}

func (r *queryResolver) recurringExpenses(ctx context.Context, householdID string, includeArchived bool) ([]*model.RecurringExpense, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, ErrNotFound
	}
	var rows models.RecurringExpenseSlice
	if includeArchived {
		rows, err = models.RecurringExpenses.Query(sm.Where(models.RecurringExpenses.Columns.HouseholdID.EQ(psql.Arg(hhID))), sm.OrderBy(models.RecurringExpenses.Columns.NextDueOn)).All(ctx, queryExecutor(r.DB))
	} else {
		rows, err = models.RecurringExpenses.Query(sm.Where(psql.And(models.RecurringExpenses.Columns.HouseholdID.EQ(psql.Arg(hhID)), models.RecurringExpenses.Columns.Archived.EQ(psql.Arg(false)))), sm.OrderBy(models.RecurringExpenses.Columns.NextDueOn)).All(ctx, queryExecutor(r.DB))
	}
	if err != nil {
		return nil, err
	}
	result := make([]*model.RecurringExpense, 0, len(rows))
	for _, row := range rows {
		result = append(result, recurringExpenseModel(row))
	}
	return result, nil
}

func (r *queryResolver) recurringExpenseConnection(ctx context.Context, householdID string, includeArchived bool, after, before *string, first, last *int) (*model.RecurringExpenseConnection, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	pageSize, backward, err := pageDirection(after, before, first, last)
	if err != nil {
		return nil, err
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, ErrNotFound
	}
	executor := queryExecutor(r.DB)
	filters := []bob.Expression{models.RecurringExpenses.Columns.HouseholdID.EQ(psql.Arg(hhID))}
	if !includeArchived {
		filters = append(filters, models.RecurringExpenses.Columns.Archived.EQ(psql.Arg(false)))
	}
	baseFilter := psql.And(filters...)
	count, err := models.RecurringExpenses.Query(sm.Where(baseFilter)).Count(ctx, executor)
	if err != nil {
		return nil, err
	}
	for _, boundary := range []struct {
		cursor   *string
		isBefore bool
	}{{cursor: after}, {cursor: before, isBefore: true}} {
		afterID, err := cursorID(boundary.cursor, "RecurringExpense")
		if err != nil {
			return nil, err
		}
		if afterID == nil {
			continue
		}
		cursorID, err := gofrsuuid.FromString(*afterID)
		if err != nil {
			return nil, fmt.Errorf("parse recurring expense cursor: %w", err)
		}
		cursor, err := models.RecurringExpenses.Query(sm.Where(psql.And(
			models.RecurringExpenses.Columns.ID.EQ(psql.Arg(cursorID)),
			baseFilter,
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		filters = append(filters, recurringExpenseCursorFilter(cursor, boundary.isBefore))
	}
	var rows []*models.RecurringExpense
	if backward {
		rows, err = models.RecurringExpenses.Query(
			sm.Where(psql.And(filters...)),
			sm.OrderBy(models.RecurringExpenses.Columns.NextDueOn).Desc(),
			sm.OrderBy(models.RecurringExpenses.Columns.ID).Desc(),
			sm.Limit(pageSize+1),
		).All(ctx, executor)
	} else {
		rows, err = models.RecurringExpenses.Query(
			sm.Where(psql.And(filters...)),
			sm.OrderBy(models.RecurringExpenses.Columns.NextDueOn),
			sm.OrderBy(models.RecurringExpenses.Columns.ID),
			sm.Limit(pageSize+1),
		).All(ctx, executor)
	}
	if err != nil {
		return nil, err
	}
	hasNextPage := len(rows) > pageSize
	if hasNextPage {
		rows = rows[:pageSize]
	}
	if backward {
		slices.Reverse(rows)
	}
	connection := &model.RecurringExpenseConnection{
		Edges:      make([]*model.RecurringExpenseEdge, 0, len(rows)),
		TotalCount: int(count),
		PageInfo:   &model.PageInfo{HasNextPage: backward && before != nil && *before != "" || !backward && hasNextPage, HasPreviousPage: backward && hasNextPage || !backward && after != nil && *after != ""},
	}
	for _, row := range rows {
		connection.Edges = append(connection.Edges, &model.RecurringExpenseEdge{
			Cursor: encodeCursor("RecurringExpense", row.ID.String()),
			Node:   recurringExpenseModel(row),
		})
	}
	if len(rows) > 0 {
		start, end := encodeCursor("RecurringExpense", rows[0].ID.String()), encodeCursor("RecurringExpense", rows[len(rows)-1].ID.String())
		connection.PageInfo.StartCursor, connection.PageInfo.EndCursor = &start, &end
	}
	return connection, nil
}

func recurringExpenseCursorFilter(cursor *models.RecurringExpense, isBefore bool) bob.Expression {
	if isBefore {
		return psql.Or(
			models.RecurringExpenses.Columns.NextDueOn.LT(psql.Arg(cursor.NextDueOn)),
			psql.And(
				models.RecurringExpenses.Columns.NextDueOn.EQ(psql.Arg(cursor.NextDueOn)),
				models.RecurringExpenses.Columns.ID.LT(psql.Arg(cursor.ID)),
			),
		)
	}
	return psql.Or(
		models.RecurringExpenses.Columns.NextDueOn.GT(psql.Arg(cursor.NextDueOn)),
		psql.And(
			models.RecurringExpenses.Columns.NextDueOn.EQ(psql.Arg(cursor.NextDueOn)),
			models.RecurringExpenses.Columns.ID.GT(psql.Arg(cursor.ID)),
		),
	)
}
