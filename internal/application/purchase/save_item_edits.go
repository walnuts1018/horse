package purchase

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/txcleanup"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type SaveItemEditsCommand struct {
	ActorID        string
	PurchaseID     string
	Items          []UpdateItemCommand
	PurchaseUpdate *UpdateCommand
}

type SaveItemEditsResult struct {
	Items []BulkUpdatedItem
}

type saveItemEditsPlan struct {
	actorID        gofrsuuid.UUID
	purchaseID     gofrsuuid.UUID
	items          []UpdateItemCommand
	itemIDs        []gofrsuuid.UUID
	itemLockOrder  []gofrsuuid.UUID
	purchaseUpdate *UpdateCommand
}

type saveItemEditsTargets struct {
	purchase *models.Purchase
	items    map[gofrsuuid.UUID]purchaseItemUpdateState
}

func planSaveItemEdits(command SaveItemEditsCommand) (saveItemEditsPlan, error) {
	if (len(command.Items) == 0 && command.PurchaseUpdate == nil) || len(command.Items) > 200 {
		return saveItemEditsPlan{}, fmt.Errorf("%w: save requires a purchase patch or up to two hundred item patches", ErrBadInput)
	}
	actorID, err := parseUUID(command.ActorID, "actor")
	if err != nil {
		return saveItemEditsPlan{}, err
	}
	purchaseID, err := parseUUID(command.PurchaseID, "purchase")
	if err != nil {
		return saveItemEditsPlan{}, err
	}
	itemIDs := make([]gofrsuuid.UUID, len(command.Items))
	seen := make(map[gofrsuuid.UUID]struct{}, len(command.Items))
	for index, item := range command.Items {
		itemID, parseErr := parseUUID(item.ItemID, "item")
		if parseErr != nil {
			return saveItemEditsPlan{}, ErrNotFound
		}
		if _, exists := seen[itemID]; exists {
			return saveItemEditsPlan{}, fmt.Errorf("%w: duplicate item target", ErrBadInput)
		}
		seen[itemID] = struct{}{}
		itemIDs[index] = itemID
	}
	items := slices.Clone(command.Items)
	for index := range items {
		items[index].ItemID = itemIDs[index].String()
		items[index].ActorID = actorID.String()
	}
	var purchaseUpdate *UpdateCommand
	if command.PurchaseUpdate != nil {
		update := *command.PurchaseUpdate
		update.ActorID = actorID.String()
		update.PurchaseID = purchaseID.String()
		purchaseUpdate = &update
	}
	itemLockOrder := slices.Clone(itemIDs)
	slices.SortFunc(itemLockOrder, func(left, right gofrsuuid.UUID) int { return cmp.Compare(left.String(), right.String()) })
	return saveItemEditsPlan{
		actorID: actorID, purchaseID: purchaseID, items: items, itemIDs: itemIDs,
		itemLockOrder: itemLockOrder, purchaseUpdate: purchaseUpdate,
	}, nil
}

func (s *Service) SaveItemEdits(ctx context.Context, command SaveItemEditsCommand) (SaveItemEditsResult, error) {
	plan, err := planSaveItemEdits(command)
	if err != nil {
		return SaveItemEditsResult{}, err
	}
	return s.applySaveItemEdits(ctx, plan)
}

func (s *Service) applySaveItemEdits(ctx context.Context, plan saveItemEditsPlan) (SaveItemEditsResult, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return SaveItemEditsResult{}, err
	}
	defer txcleanup.Rollback(ctx, tx)
	executor := bobpgx.NewTx(tx, nil)
	targets, err := lockSaveItemEditsTargets(ctx, executor, plan)
	if err != nil {
		return SaveItemEditsResult{}, err
	}

	result := SaveItemEditsResult{Items: make([]BulkUpdatedItem, 0, len(plan.items))}
	accountingChanged := false
	for index, item := range plan.items {
		if err := validateUpdateItemCommand(item); err != nil {
			return SaveItemEditsResult{}, err
		}
		state, ok := targets.items[plan.itemIDs[index]]
		if !ok {
			return SaveItemEditsResult{}, ErrNotFound
		}
		if err := requirePurchaseMember(ctx, executor, state.householdID, plan.actorID); err != nil {
			return SaveItemEditsResult{}, err
		}
		if err := validatePurchaseItemUpdateState(item, state); err != nil {
			return SaveItemEditsResult{}, err
		}
		if err := planAndApplyPurchaseItemUpdate(ctx, tx, executor, plan.actorID, item, state, true, &accountingChanged); err != nil {
			return SaveItemEditsResult{}, err
		}
		result.Items = append(result.Items, BulkUpdatedItem{ID: plan.itemIDs[index].String(), PurchaseID: state.purchase.ID.String()})
	}
	if plan.purchaseUpdate != nil {
		if _, err := s.update(ctx, *plan.purchaseUpdate, tx, true, &accountingChanged); err != nil {
			return SaveItemEditsResult{}, err
		}
	}
	merchantChanged := plan.purchaseUpdate != nil && slices.Contains(plan.purchaseUpdate.ChangedFields, updateFieldMerchant)
	if targets.purchase.Status == postedStatus && (accountingChanged || merchantChanged) {
		purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(plan.purchaseID)))).One(ctx, executor)
		if err != nil {
			return SaveItemEditsResult{}, err
		}
		if accountingChanged {
			if err := syncPostedPurchaseAccounting(ctx, executor, purchase); err != nil {
				return SaveItemEditsResult{}, err
			}
		} else {
			total, err := purchaseItemsTotal(ctx, executor, plan.purchaseID)
			if err != nil {
				return SaveItemEditsResult{}, err
			}
			if err := updatePostedPurchaseLedgerEntries(ctx, executor, purchase, total); err != nil {
				return SaveItemEditsResult{}, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return SaveItemEditsResult{}, err
	}
	return result, nil
}

func lockSaveItemEditsTargets(ctx context.Context, executor bob.Executor, plan saveItemEditsPlan) (saveItemEditsTargets, error) {
	observedPurchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(plan.purchaseID)))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return saveItemEditsTargets{}, ErrNotFound
	}
	if err != nil {
		return saveItemEditsTargets{}, err
	}
	householdID := observedPurchase.HouseholdID
	if err := requirePurchaseMember(ctx, executor, householdID, plan.actorID); err != nil {
		return saveItemEditsTargets{}, err
	}
	if observedPurchase.Status != draftStatus && observedPurchase.Status != postedStatus {
		return saveItemEditsTargets{}, fmt.Errorf("%w: closed purchases require a correction", ErrConflict)
	}
	targetOccurredAt := observedPurchase.OccurredAt
	if plan.purchaseUpdate != nil && plan.purchaseUpdate.OccurredAt != nil {
		targetOccurredAt = *plan.purchaseUpdate.OccurredAt
	}
	if err := lockPurchaseAccountingPeriods(ctx, executor, householdID,
		accounting.PeriodFor(observedPurchase.OccurredAt), accounting.PeriodFor(targetOccurredAt)); err != nil {
		return saveItemEditsTargets{}, err
	}
	household, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForShare()).One(ctx, executor)
	if err != nil {
		return saveItemEditsTargets{}, err
	}
	purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(plan.purchaseID))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return saveItemEditsTargets{}, ErrNotFound
	}
	if err != nil {
		return saveItemEditsTargets{}, err
	}
	if !purchase.OccurredAt.Equal(observedPurchase.OccurredAt) || purchase.HouseholdID != householdID {
		return saveItemEditsTargets{}, ErrConflict
	}
	if purchase.Status != draftStatus && purchase.Status != postedStatus {
		return saveItemEditsTargets{}, fmt.Errorf("%w: closed purchases require a correction", ErrConflict)
	}
	items := make(map[gofrsuuid.UUID]purchaseItemUpdateState, len(plan.itemIDs))
	for _, itemID := range plan.itemLockOrder {
		item, queryErr := models.PurchaseItems.Query(sm.Where(psql.And(
			models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemID)),
			models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(plan.purchaseID)),
		)), sm.ForUpdate()).One(ctx, executor)
		if errors.Is(queryErr, sql.ErrNoRows) {
			return saveItemEditsTargets{}, ErrNotFound
		}
		if queryErr != nil {
			return saveItemEditsTargets{}, queryErr
		}
		if item.HouseholdID != householdID {
			return saveItemEditsTargets{}, ErrNotFound
		}
		items[itemID] = purchaseItemUpdateState{
			householdID: householdID, household: household, purchase: purchase, item: item, status: purchase.Status,
		}
	}
	return saveItemEditsTargets{purchase: purchase, items: items}, nil
}
