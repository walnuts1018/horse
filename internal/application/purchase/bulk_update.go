package purchase

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
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
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
	command        BulkUpdateTarget
	id             gofrsuuid.UUID
	observedHouse  gofrsuuid.UUID
	observedAt     time.Time
	targetAt       time.Time
	purchase       *models.Purchase
	payments       []Payment
	updatePayments bool
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
		if target.command.BaseRevision > purchase.Revision {
			return BulkUpdateResult{}, ErrConflict
		}
		conflict, err := purchaseUpdateValuesConflict(purchase.FieldRevisions.Val, target.command.BaseRevision, target.command.ChangedFields)
		if err != nil {
			return BulkUpdateResult{}, err
		}
		if conflict {
			return BulkUpdateResult{}, ErrConflict
		}
		target.updatePayments = slices.Contains(target.command.ChangedFields, updateFieldPaymentAllocations)
		if target.updatePayments {
			payments := *target.command.PaymentAllocations
			items, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(target.id)))).All(ctx, executor)
			if err != nil {
				return BulkUpdateResult{}, err
			}
			var total int64
			for _, item := range items {
				var ok bool
				total, ok = checkedAdd(total, item.AmountYen)
				if !ok {
					return BulkUpdateResult{}, fmt.Errorf("%w: purchase total is too large", ErrBadInput)
				}
			}
			if purchase.Status == postedStatus {
				var paid int64
				for _, payment := range payments {
					var ok bool
					paid, ok = checkedAdd(paid, payment.AmountYen)
					if !ok {
						return BulkUpdateResult{}, fmt.Errorf("%w: payment total is too large", ErrBadInput)
					}
				}
				if paid != total {
					return BulkUpdateResult{}, fmt.Errorf("%w: payment allocations must equal purchase total", ErrBadInput)
				}
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
		newRevision := purchase.Revision + 1
		fieldRevisions, err := purchaseUpdateFieldRevisions(purchase.FieldRevisions.Val, target.command.ChangedFields, newRevision)
		if err != nil {
			return BulkUpdateResult{}, err
		}
		if target.command.OccurredAt != nil {
			purchase.OccurredAt = target.targetAt
		}
		if target.command.DatePrecision != nil {
			purchase.DatePrecision = *target.command.DatePrecision
		}
		if target.command.Merchant != nil {
			purchase.Merchant = strings.TrimSpace(*target.command.Merchant)
		}
		paymentAccountID := purchase.PaymentAccountID
		if target.updatePayments {
			paymentAccountID = sql.Null[gofrsuuid.UUID]{}
			if err := replaceUpdatePayments(ctx, executor, purchase, target.payments); err != nil {
				return BulkUpdateResult{}, err
			}
		}
		fieldValue := types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(fieldRevisions)}
		if _, err := models.Purchases.Update((&models.PurchaseSetter{
			OccurredAt: &purchase.OccurredAt, DatePrecision: &purchase.DatePrecision, Merchant: &purchase.Merchant, PaymentAccountID: &paymentAccountID,
			Revision: &newRevision, FieldRevisions: &fieldValue,
		}).UpdateMod(), um.Where(models.Purchases.Columns.ID.EQ(psql.Arg(target.id))), um.SetCol("updated_at").To(psql.F("now"))).Exec(ctx, executor); err != nil {
			return BulkUpdateResult{}, err
		}
		if purchase.Status == postedStatus {
			if err := syncPostedPurchaseAccounting(ctx, executor, purchase); err != nil {
				return BulkUpdateResult{}, err
			}
		}
		if err := insertPurchaseUpdateEvent(ctx, executor, householdID, actorID, target.id, newRevision, target.command.ChangedFields); err != nil {
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
	if len(target.ChangedFields) == 0 {
		return fmt.Errorf("%w: patch must contain a field", ErrBadInput)
	}
	seen := make(map[string]struct{}, len(target.ChangedFields))
	for _, field := range target.ChangedFields {
		if field != updateFieldOccurredAt && field != updateFieldMerchant && field != updateFieldPaymentAllocations {
			return fmt.Errorf("%w: invalid changed field", ErrBadInput)
		}
		if _, exists := seen[field]; exists {
			return fmt.Errorf("%w: duplicate changed field", ErrBadInput)
		}
		seen[field] = struct{}{}
	}
	if slices.Contains(target.ChangedFields, updateFieldOccurredAt) && target.OccurredAt == nil {
		return fmt.Errorf("%w: occurredAt is required", ErrBadInput)
	}
	if target.DatePrecision != nil {
		if !slices.Contains(target.ChangedFields, updateFieldOccurredAt) || target.OccurredAt == nil || !validDatePrecision(*target.DatePrecision) {
			return fmt.Errorf("%w: valid date precision requires occurredAt", ErrBadInput)
		}
	}
	if slices.Contains(target.ChangedFields, updateFieldMerchant) && target.Merchant == nil {
		return fmt.Errorf("%w: merchant is required", ErrBadInput)
	}
	if slices.Contains(target.ChangedFields, updateFieldPaymentAllocations) && target.PaymentAllocations == nil {
		return fmt.Errorf("%w: payment allocations are required", ErrBadInput)
	}
	return nil
}
