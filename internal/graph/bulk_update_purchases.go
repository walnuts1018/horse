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
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type bulkPurchaseTarget struct {
	input          *model.PurchaseUpdateTargetInput
	id             gofrsuuid.UUID
	observedHouse  gofrsuuid.UUID
	observedAt     time.Time
	targetAt       time.Time
	purchase       *models.Purchase
	payments       []model.PurchasePaymentInput
	updatePayments bool
}

func (r *mutationResolver) bulkUpdatePurchases(ctx context.Context, input model.BulkUpdatePurchasesInput) ([]*model.Purchase, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if len(input.Targets) == 0 || len(input.Targets) > 100 {
		return nil, fmt.Errorf("%w: select between one and one hundred purchases", ErrBadInput)
	}
	if err := validateChangedFields(input.ChangedFields, "occurredAt", "merchant", "paymentAllocations"); err != nil {
		return nil, err
	}
	if len(input.ChangedFields) == 0 {
		return nil, fmt.Errorf("%w: select at least one field", ErrBadInput)
	}
	seen := make(map[string]struct{}, len(input.Targets))
	for _, target := range input.Targets {
		if target == nil || target.Patch == nil {
			return nil, fmt.Errorf("%w: missing purchase target or patch", ErrBadInput)
		}
		if _, ok := seen[target.PurchaseID]; ok {
			return nil, fmt.Errorf("%w: duplicate purchase target", ErrBadInput)
		}
		seen[target.PurchaseID] = struct{}{}
		if err := validatePurchasePatchFields(target.Patch, input.ChangedFields); err != nil {
			return nil, err
		}
	}

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	targets := make([]bulkPurchaseTarget, 0, len(input.Targets))
	var householdID gofrsuuid.UUID
	periods := make([]string, 0, len(input.Targets)*2)
	for _, target := range input.Targets {
		id, parseErr := gofrsuuid.FromString(target.PurchaseID)
		if parseErr != nil {
			return nil, ErrNotFound
		}
		purchase, queryErr := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(id)))).One(ctx, executor)
		if errors.Is(queryErr, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if queryErr != nil {
			return nil, queryErr
		}
		if len(targets) == 0 {
			householdID = purchase.HouseholdID
			if _, err := requireMember(ctx, r.DB, householdID.String()); err != nil {
				return nil, err
			}
		} else if householdID != purchase.HouseholdID {
			return nil, fmt.Errorf("%w: all purchases must belong to one household", ErrBadInput)
		}
		targetAt := purchase.OccurredAt
		if target.Patch.OccurredAt != nil {
			targetAt, err = parseTime(*target.Patch.OccurredAt)
			if err != nil {
				return nil, err
			}
		}
		periods = append(periods, accountingPeriod(purchase.OccurredAt), accountingPeriod(targetAt))
		targets = append(targets, bulkPurchaseTarget{
			input: target, id: id, observedHouse: purchase.HouseholdID,
			observedAt: purchase.OccurredAt, targetAt: targetAt,
		})
	}
	if err := lockAccountingPeriods(ctx, tx, householdID.String(), periods...); err != nil {
		return nil, err
	}
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForShare()).One(ctx, executor); err != nil {
		return nil, err
	}
	lockOrder := slices.Clone(targets)
	slices.SortFunc(lockOrder, func(left, right bulkPurchaseTarget) int {
		return strings.Compare(left.id.String(), right.id.String())
	})
	purchases := make(map[gofrsuuid.UUID]*models.Purchase, len(targets))
	preparedTargets := make(map[gofrsuuid.UUID]bulkPurchaseTarget, len(targets))
	for _, target := range lockOrder {
		purchase, queryErr := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(target.id))), sm.ForUpdate()).One(ctx, executor)
		if errors.Is(queryErr, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if queryErr != nil {
			return nil, queryErr
		}
		if purchase.HouseholdID != target.observedHouse || !purchase.OccurredAt.Equal(target.observedAt) {
			return nil, ErrConflict
		}
		if purchase.Status != string(model.PurchaseStatusDraft) && purchase.Status != string(model.PurchaseStatusPosted) {
			return nil, fmt.Errorf("%w: closed purchases require a correction", ErrConflict)
		}
		if target.input.BaseRevision > int(purchase.Revision) {
			return nil, ErrConflict
		}
		conflict, err := valuesConflict(purchase.FieldRevisions.Val, target.input.BaseRevision, input.ChangedFields)
		if err != nil {
			return nil, err
		}
		if conflict {
			return nil, ErrConflict
		}
		if target.input.Patch.OccurredAt != nil {
			target.targetAt, err = parseTime(*target.input.Patch.OccurredAt)
			if err != nil {
				return nil, err
			}
		}
		if target.input.Patch.Merchant != nil && len(strings.TrimSpace(*target.input.Patch.Merchant)) > 240 {
			return nil, fmt.Errorf("%w: merchant name is too long", ErrBadInput)
		}
		target.updatePayments = slices.Contains(input.ChangedFields, "paymentAllocations")
		if target.updatePayments {
			target.payments, err = validatePurchasePaymentInputs(ctx, tx, householdID.String(), target.input.Patch.PaymentAllocations)
			if err != nil {
				return nil, err
			}
			items, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(target.id)))).All(ctx, executor)
			if err != nil {
				return nil, err
			}
			var total int64
			for _, item := range items {
				var ok bool
				total, ok = checkedAdd(total, item.AmountYen)
				if !ok {
					return nil, fmt.Errorf("%w: purchase total is too large", ErrBadInput)
				}
			}
			if purchase.Status == string(model.PurchaseStatusPosted) {
				var paid int64
				for _, payment := range target.payments {
					var ok bool
					paid, ok = checkedAdd(paid, int64(payment.AmountYen))
					if !ok {
						return nil, fmt.Errorf("%w: payment total is too large", ErrBadInput)
					}
				}
				if paid != total {
					return nil, fmt.Errorf("%w: payment allocations must equal purchase total", ErrBadInput)
				}
			}
		}
		target.purchase = purchase
		purchases[target.id] = purchase
		preparedTargets[target.id] = target
	}

	for i := range targets {
		target := &targets[i]
		*target = preparedTargets[target.id]
		target.purchase = purchases[target.id]
		patch := target.input.Patch
		purchase := target.purchase
		newRevision := purchase.Revision + 1
		fieldRevisions, err := setFieldRevisions(purchase.FieldRevisions.Val, input.ChangedFields, newRevision)
		if err != nil {
			return nil, err
		}
		occurredAt := purchase.OccurredAt
		if slices.Contains(input.ChangedFields, "occurredAt") {
			occurredAt = target.targetAt
		}
		merchant := purchase.Merchant
		if slices.Contains(input.ChangedFields, "merchant") {
			merchant = strings.TrimSpace(*patch.Merchant)
		}
		paymentAccountID := purchase.PaymentAccountID
		if target.updatePayments {
			paymentAccountID = sql.Null[gofrsuuid.UUID]{}
			if err := replacePurchasePaymentRows(ctx, tx, householdID.String(), target.input.PurchaseID, target.payments); err != nil {
				return nil, err
			}
		}
		fieldValue := types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(fieldRevisions)}
		if _, err := models.Purchases.Update((&models.PurchaseSetter{
			OccurredAt: &occurredAt, Merchant: &merchant, PaymentAccountID: &paymentAccountID,
			Revision: &newRevision, FieldRevisions: &fieldValue,
		}).UpdateMod(), um.Where(models.Purchases.Columns.ID.EQ(psql.Arg(target.id))), um.SetCol("updated_at").To(psql.F("now"))).Exec(ctx, executor); err != nil {
			return nil, err
		}
		purchase.OccurredAt = occurredAt
		purchase.Merchant = merchant
		if purchase.Status == string(model.PurchaseStatusPosted) {
			if err := syncPostedPurchaseAccounting(ctx, tx, target.input.PurchaseID); err != nil {
				return nil, err
			}
		}
		if err := writeEvent(ctx, tx, householdID.String(), "purchase.updated", target.input.PurchaseID, newRevision, input.ChangedFields, user.MemberID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	result := make([]*model.Purchase, 0, len(targets))
	for _, target := range targets {
		purchase, err := loadPurchase(ctx, r.DB, target.input.PurchaseID)
		if err != nil {
			return nil, err
		}
		result = append(result, purchase)
	}
	return result, nil
}

func validatePurchasePatchFields(patch *model.BulkPurchasePatch, changedFields []string) error {
	if patch.OccurredAt != nil && !slices.Contains(changedFields, "occurredAt") ||
		patch.Merchant != nil && !slices.Contains(changedFields, "merchant") ||
		patch.PaymentAllocations != nil && !slices.Contains(changedFields, "paymentAllocations") {
		return fmt.Errorf("%w: patch contains fields missing from changedFields", ErrBadInput)
	}
	for _, field := range changedFields {
		switch field {
		case "occurredAt":
			if patch.OccurredAt == nil {
				return fmt.Errorf("%w: changed field has no value", ErrBadInput)
			}
		case "merchant":
			if patch.Merchant == nil {
				return fmt.Errorf("%w: changed field has no value", ErrBadInput)
			}
		case "paymentAllocations":
			if patch.PaymentAllocations == nil {
				return fmt.Errorf("%w: changed field has no value", ErrBadInput)
			}
		}
	}
	return nil
}
