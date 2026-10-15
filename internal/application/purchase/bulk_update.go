package purchase

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/inputvalidation"
	"github.com/walnuts1018/horse/internal/application/txcleanup"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type BulkUpdateCommand struct {
	ActorID string
	Targets []BulkUpdateTarget
}

type BulkUpdateTarget struct {
	PurchaseID         string
	BaseRevision       int64
	ChangedFields      []string
	OccurredAt         *time.Time
	DatePrecision      *string
	Merchant           *string `validate:"omitempty,max=240"`
	PaymentAllocations *[]Payment
}

type BulkUpdateResult struct {
	PurchaseIDs []string
}

type bulkUpdateTargetState struct {
	command       BulkUpdateTarget
	id            gofrsuuid.UUID
	observedHouse gofrsuuid.UUID
	observedAt    time.Time
	targetAt      time.Time
	purchase      *models.Purchase
	plan          purchaseUpdatePlan
	payments      []Payment
}

func (s *Service) BulkUpdate(ctx context.Context, command BulkUpdateCommand) (BulkUpdateResult, error) {
	if len(command.Targets) == 0 || len(command.Targets) > 100 {
		return BulkUpdateResult{}, fmt.Errorf("%w: select between one and one hundred purchases", ErrBadInput)
	}
	actorID, err := parseUUID(command.ActorID, "actor")
	if err != nil {
		return BulkUpdateResult{}, err
	}
	seen := make(map[string]struct{}, len(command.Targets))
	for _, target := range command.Targets {
		if _, exists := seen[target.PurchaseID]; exists {
			return BulkUpdateResult{}, fmt.Errorf("%w: duplicate purchase target", ErrBadInput)
		}
		seen[target.PurchaseID] = struct{}{}
		if err := validateBulkPurchaseFields(target); err != nil {
			return BulkUpdateResult{}, err
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return BulkUpdateResult{}, err
	}
	defer txcleanup.Rollback(ctx, tx)
	executor := bobpgx.NewTx(tx, nil)
	targets := make([]bulkUpdateTargetState, 0, len(command.Targets))
	var householdID gofrsuuid.UUID
	periods := make([]string, 0, len(command.Targets)*2)
	for _, target := range command.Targets {
		id, err := parseUUID(target.PurchaseID, "purchase")
		if err != nil {
			return BulkUpdateResult{}, ErrNotFound
		}
		purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(id)))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return BulkUpdateResult{}, ErrNotFound
		}
		if err != nil {
			return BulkUpdateResult{}, err
		}
		if len(targets) == 0 {
			householdID = purchase.HouseholdID
			if err := requirePurchaseMember(ctx, executor, householdID, actorID); err != nil {
				return BulkUpdateResult{}, err
			}
		} else if householdID != purchase.HouseholdID {
			return BulkUpdateResult{}, fmt.Errorf("%w: all purchases must belong to one household", ErrBadInput)
		}
		targetAt := purchase.OccurredAt
		if target.OccurredAt != nil {
			targetAt = *target.OccurredAt
		}
		periods = append(periods, accounting.PeriodFor(purchase.OccurredAt), accounting.PeriodFor(targetAt))
		targets = append(targets, bulkUpdateTargetState{
			command: target, id: id, observedHouse: purchase.HouseholdID,
			observedAt: purchase.OccurredAt, targetAt: targetAt,
		})
	}
	if err := lockPurchaseAccountingPeriods(ctx, executor, householdID, periods...); err != nil {
		return BulkUpdateResult{}, err
	}
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForShare()).One(ctx, executor); err != nil {
		return BulkUpdateResult{}, err
	}
	lockOrder := slices.Clone(targets)
	slices.SortFunc(lockOrder, func(left, right bulkUpdateTargetState) int {
		return strings.Compare(left.id.String(), right.id.String())
	})
	prepared := make(map[gofrsuuid.UUID]bulkUpdateTargetState, len(targets))
	for _, target := range lockOrder {
		purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(target.id))), sm.ForUpdate()).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return BulkUpdateResult{}, ErrNotFound
		}
		if err != nil {
			return BulkUpdateResult{}, err
		}
		if purchase.HouseholdID != target.observedHouse || !purchase.OccurredAt.Equal(target.observedAt) {
			return BulkUpdateResult{}, ErrConflict
		}
		if purchase.Status != draftStatus && purchase.Status != postedStatus {
			return BulkUpdateResult{}, fmt.Errorf("%w: closed purchases require a correction", ErrConflict)
		}
		target.plan, err = planPurchaseUpdate(
			purchase, target.observedAt, target.command.BaseRevision, target.command.ChangedFields,
			target.command.OccurredAt, target.command.DatePrecision, target.command.Merchant,
		)
		if err != nil {
			return BulkUpdateResult{}, err
		}
		if target.plan.updatesPayments {
			payments := *target.command.PaymentAllocations
			if err := validatePurchaseUpdatePaymentAmounts(ctx, executor, purchase, purchase.Status, payments); err != nil {
				return BulkUpdateResult{}, err
			}
			target.payments, err = validatePayments(ctx, tx, householdID, payments)
			if err != nil {
				return BulkUpdateResult{}, err
			}
		}
		target.purchase = purchase
		prepared[target.id] = target
	}
	for index := range targets {
		target := prepared[targets[index].id]
		purchase := target.purchase
		if err := applyPurchaseUpdatePlan(ctx, executor, purchase, target.plan, target.payments); err != nil {
			return BulkUpdateResult{}, err
		}
		if purchase.Status == postedStatus {
			if err := syncPostedPurchaseAccounting(ctx, executor, purchase); err != nil {
				return BulkUpdateResult{}, err
			}
		}
		if err := insertPurchaseUpdateEvent(ctx, executor, householdID, actorID, target.id, target.plan.revision, target.command.ChangedFields); err != nil {
			return BulkUpdateResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return BulkUpdateResult{}, err
	}
	result := BulkUpdateResult{PurchaseIDs: make([]string, 0, len(command.Targets))}
	for _, target := range command.Targets {
		result.PurchaseIDs = append(result.PurchaseIDs, target.PurchaseID)
	}
	return result, nil
}

func validateBulkPurchaseFields(target BulkUpdateTarget) error {
	if err := inputvalidation.Check(target); err != nil {
		return err
	}
	return validatePurchaseUpdateFields(purchaseUpdateValidationInput{
		changedFields: target.ChangedFields, occurredAt: target.OccurredAt, datePrecision: target.DatePrecision,
		merchant: target.Merchant, paymentAllocations: target.PaymentAllocations,
	})
}
