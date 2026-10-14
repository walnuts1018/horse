package purchase

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
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

func (s *Service) SaveItemEdits(ctx context.Context, command SaveItemEditsCommand) (SaveItemEditsResult, error) {
	if (len(command.Items) == 0 && command.PurchaseUpdate == nil) || len(command.Items) > 200 {
		return SaveItemEditsResult{}, fmt.Errorf("%w: save requires a purchase patch or up to two hundred item patches", ErrBadInput)
	}
	actorID, err := parseUUID(command.ActorID, "actor")
	if err != nil {
		return SaveItemEditsResult{}, err
	}
	purchaseID, err := parseUUID(command.PurchaseID, "purchase")
	if err != nil {
		return SaveItemEditsResult{}, err
	}
	itemIDs := make([]gofrsuuid.UUID, len(command.Items))
	seen := make(map[gofrsuuid.UUID]struct{}, len(command.Items))
	for index, item := range command.Items {
		itemID, parseErr := parseUUID(item.ItemID, "item")
		if parseErr != nil {
			return SaveItemEditsResult{}, ErrNotFound
		}
		if _, exists := seen[itemID]; exists {
			return SaveItemEditsResult{}, fmt.Errorf("%w: duplicate item target", ErrBadInput)
		}
		seen[itemID] = struct{}{}
		itemIDs[index] = itemID
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return SaveItemEditsResult{}, err
	}
	defer txcleanup.Rollback(ctx, tx)
	executor := bobpgx.NewTx(tx, nil)
	observedPurchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseID)))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return SaveItemEditsResult{}, ErrNotFound
	}
	if err != nil {
		return SaveItemEditsResult{}, err
	}
	householdID := observedPurchase.HouseholdID
	if err := requirePurchaseMember(ctx, executor, householdID, actorID); err != nil {
		return SaveItemEditsResult{}, err
	}
	if observedPurchase.Status != draftStatus && observedPurchase.Status != postedStatus {
		return SaveItemEditsResult{}, fmt.Errorf("%w: closed purchases require a correction", ErrConflict)
	}
	targetOccurredAt := observedPurchase.OccurredAt
	if command.PurchaseUpdate != nil && command.PurchaseUpdate.OccurredAt != nil {
		targetOccurredAt = *command.PurchaseUpdate.OccurredAt
	}
	if err := lockPurchaseAccountingPeriods(ctx, executor, householdID,
		accounting.PeriodFor(observedPurchase.OccurredAt), accounting.PeriodFor(targetOccurredAt)); err != nil {
		return SaveItemEditsResult{}, err
	}
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForShare()).One(ctx, executor); err != nil {
		return SaveItemEditsResult{}, err
	}
	purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseID))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return SaveItemEditsResult{}, ErrNotFound
	}
	if err != nil {
		return SaveItemEditsResult{}, err
	}
	if !purchase.OccurredAt.Equal(observedPurchase.OccurredAt) || purchase.HouseholdID != householdID {
		return SaveItemEditsResult{}, ErrConflict
	}
	if purchase.Status != draftStatus && purchase.Status != postedStatus {
		return SaveItemEditsResult{}, fmt.Errorf("%w: closed purchases require a correction", ErrConflict)
	}
	lockOrder := slices.Clone(itemIDs)
	slices.SortFunc(lockOrder, func(left, right gofrsuuid.UUID) int {
		return strings.Compare(left.String(), right.String())
	})
	for _, itemID := range lockOrder {
		item, queryErr := models.PurchaseItems.Query(sm.Where(psql.And(
			models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemID)),
			models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseID)),
		)), sm.ForUpdate()).One(ctx, executor)
		if errors.Is(queryErr, sql.ErrNoRows) {
			return SaveItemEditsResult{}, ErrNotFound
		}
		if queryErr != nil {
			return SaveItemEditsResult{}, queryErr
		}
		if item.HouseholdID != householdID {
			return SaveItemEditsResult{}, ErrNotFound
		}
	}

	result := SaveItemEditsResult{Items: make([]BulkUpdatedItem, 0, len(command.Items))}
	for index, item := range command.Items {
		item.ItemID = itemIDs[index].String()
		item.ActorID = actorID.String()
		updated, err := s.updateItem(ctx, item, tx)
		if err != nil {
			return SaveItemEditsResult{}, err
		}
		result.Items = append(result.Items, BulkUpdatedItem{ID: itemIDs[index].String(), PurchaseID: updated.PurchaseID})
	}
	if command.PurchaseUpdate != nil {
		purchaseUpdate := *command.PurchaseUpdate
		purchaseUpdate.ActorID = actorID.String()
		purchaseUpdate.PurchaseID = purchaseID.String()
		if _, err := s.update(ctx, purchaseUpdate, tx); err != nil {
			return SaveItemEditsResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return SaveItemEditsResult{}, err
	}
	return result, nil
}
