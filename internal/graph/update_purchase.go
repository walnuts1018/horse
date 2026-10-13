package graph

import (
	"context"
	"database/sql"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (r *mutationResolver) updatePurchase(ctx context.Context, input model.UpdatePurchaseInput) (*model.Purchase, error) {
	changedFields, err := purchasePatchChangedFieldKeys(input.Patch)
	if err != nil {
		return nil, err
	}
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	purchaseUUID, err := gofrsuuid.FromString(input.PurchaseID)
	if err != nil {
		return nil, ErrNotFound
	}
	executor := bobpgx.NewTx(tx, nil)
	observedPurchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseUUID)))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	observedHouseholdID := observedPurchase.HouseholdID.String()
	observedOccurredAt := observedPurchase.OccurredAt
	if _, err := requireMember(ctx, r.DB, observedHouseholdID); err != nil {
		return nil, err
	}
	targetOccurredAt := observedOccurredAt
	if input.Patch.OccurredAt.IsSet() {
		targetOccurredAt, err = parseTime(*input.Patch.OccurredAt.Value())
		if err != nil {
			return nil, err
		}
	}
	if err := lockAccountingPeriods(ctx, tx, observedHouseholdID, accountingPeriod(observedOccurredAt), accountingPeriod(targetOccurredAt)); err != nil {
		return nil, err
	}
	purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseUUID))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	householdID := purchase.HouseholdID.String()
	status := purchase.Status
	if householdID != observedHouseholdID || accountingPeriod(purchase.OccurredAt) != accountingPeriod(observedOccurredAt) {
		return nil, ErrConflict
	}
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(purchase.HouseholdID))), sm.ForShare()).One(ctx, executor); err != nil {
		return nil, err
	}
	if status != string(model.PurchaseStatusDraft) && status != string(model.PurchaseStatusPosted) {
		return nil, fmt.Errorf("%w: closed purchases require a correction", ErrConflict)
	}
	if input.BaseRevision > int(purchase.Revision) {
		return nil, ErrConflict
	}
	if err := validateChangedFields(changedFields, fieldOccurredAt, fieldMerchant, fieldPaymentAllocations); err != nil {
		return nil, err
	}
	conflict, err := valuesConflict(purchase.FieldRevisions.Val, input.BaseRevision, changedFields)
	if err != nil {
		return nil, err
	}
	if conflict {
		return nil, ErrConflict
	}
	newRevision := purchase.Revision + 1
	updatedFields, err := setFieldRevisions(purchase.FieldRevisions.Val, changedFields, newRevision)
	if err != nil {
		return nil, err
	}
	updatePayments := slices.Contains(changedFields, fieldPaymentAllocations)
	paymentAccountID := purchase.PaymentAccountID
	if updatePayments {
		paymentAccountID = sql.Null[gofrsuuid.UUID]{}
	}
	if input.Patch != nil {
		if slices.Contains(changedFields, fieldOccurredAt) {
			occurredAt, err := parseTime(*input.Patch.OccurredAt.Value())
			if err != nil {
				return nil, err
			}
			purchase.OccurredAt = occurredAt
		}
		if slices.Contains(changedFields, fieldMerchant) {
			value := strings.TrimSpace(*input.Patch.Merchant.Value())
			if len(value) > 240 {
				return nil, fmt.Errorf("%w: merchant name is too long", ErrBadInput)
			}
			purchase.Merchant = value
		}
		if updatePayments {
			payments, err := validatePurchasePaymentInputs(ctx, tx, householdID, input.Patch.PaymentAllocations.Value())
			if err != nil {
				return nil, err
			}
			items, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, executor)
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
			if status == string(model.PurchaseStatusPosted) {
				var paid int64
				for _, payment := range payments {
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
			if err := replacePurchasePaymentRows(ctx, tx, householdID, input.PurchaseID, payments); err != nil {
				return nil, err
			}
		}
	}
	fieldRevisions := types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(updatedFields)}
	if _, err := models.Purchases.Update((&models.PurchaseSetter{
		OccurredAt: &purchase.OccurredAt, Merchant: &purchase.Merchant, PaymentAccountID: &paymentAccountID,
		Revision: &newRevision, FieldRevisions: &fieldRevisions,
	}).UpdateMod(),
		um.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseUUID))),
		um.SetCol("updated_at").To(psql.F("now")),
	).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if status == string(model.PurchaseStatusPosted) {
		if err := syncPostedPurchaseAccounting(ctx, tx, input.PurchaseID); err != nil {
			return nil, err
		}
	}
	if err := writeEvent(ctx, tx, householdID, "purchase.updated", input.PurchaseID, newRevision, changedFields, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadPurchase(ctx, r.DB, input.PurchaseID)
}
