package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func correctionAllocationWeights(ctx context.Context, tx pgx.Tx, purchaseID string, itemID *string) ([]domain.Weight, error) {
	purchaseUUID, err := gofrsuuid.FromString(purchaseID)
	if err != nil {
		return nil, err
	}
	var rows models.ItemAllocationSlice
	executor := bobpgx.NewTx(tx, nil)
	if itemID != nil {
		itemUUID, parseErr := gofrsuuid.FromString(*itemID)
		if parseErr != nil {
			return nil, parseErr
		}
		rows, err = models.ItemAllocations.Query(sm.Where(psql.And(
			models.ItemAllocations.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)),
			models.ItemAllocations.Columns.ItemID.EQ(psql.Arg(itemUUID)),
		))).All(ctx, executor)
	} else {
		rows, err = models.ItemAllocations.Query(sm.Where(models.ItemAllocations.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, executor)
	}
	if err != nil {
		return nil, err
	}
	poolTotals := make(map[string]int64)
	for _, row := range rows {
		value := row.AmountYen
		if itemID != nil {
			value = row.Weight
		}
		poolID := row.PoolID.String()
		var ok bool
		poolTotals[poolID], ok = checkedAdd(poolTotals[poolID], value)
		if !ok {
			return nil, fmt.Errorf("%w: allocation total is too large", ErrBadInput)
		}
	}
	weights := make([]domain.Weight, 0, len(poolTotals))
	for poolID, amount := range poolTotals {
		if amount > 0 {
			weights = append(weights, domain.Weight{PoolID: poolID, Value: amount})
		}
	}
	slices.SortFunc(weights, func(a, b domain.Weight) int { return strings.Compare(a.PoolID, b.PoolID) })
	if len(weights) == 0 {
		return nil, fmt.Errorf("%w: purchase has no saved allocation", ErrBadInput)
	}
	return weights, nil
}

func sameAllocationWeights(inputs []*model.AllocationInput, saved []domain.Weight) bool {
	if len(inputs) != len(saved) {
		return false
	}
	requested := make(map[string]int64, len(inputs))
	for _, input := range inputs {
		if input == nil || input.PoolID == "" || input.Weight <= 0 {
			return false
		}
		if _, exists := requested[input.PoolID]; exists {
			return false
		}
		requested[input.PoolID] = int64(input.Weight)
	}
	for _, weight := range saved {
		if requested[weight.PoolID] != weight.Value {
			return false
		}
	}
	return true
}

func refundablePurchaseAmounts(ctx context.Context, executor bob.Executor, purchaseUUID gofrsuuid.UUID) (int64, map[gofrsuuid.UUID]int64, error) {
	items, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, executor)
	if err != nil {
		return 0, nil, err
	}
	total := int64(0)
	byItem := make(map[gofrsuuid.UUID]int64, len(items))
	for _, item := range items {
		var ok bool
		total, ok = checkedAdd(total, item.AmountYen)
		if !ok {
			return 0, nil, fmt.Errorf("%w: purchase total is too large", ErrBadInput)
		}
		byItem[item.ID], ok = checkedAdd(byItem[item.ID], item.AmountYen)
		if !ok {
			return 0, nil, fmt.Errorf("%w: item total is too large", ErrBadInput)
		}
	}
	corrections, err := models.Corrections.Query(sm.Where(models.Corrections.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, executor)
	if err != nil {
		return 0, nil, err
	}
	for _, correction := range corrections {
		var ok bool
		total, ok = checkedAdd(total, correction.AmountDeltaYen)
		if !ok {
			return 0, nil, fmt.Errorf("%w: refundable purchase total is too large", ErrBadInput)
		}
		if correction.ItemID.Valid {
			byItem[correction.ItemID.V], ok = checkedAdd(byItem[correction.ItemID.V], correction.AmountDeltaYen)
			if !ok {
				return 0, nil, fmt.Errorf("%w: refundable item total is too large", ErrBadInput)
			}
		}
	}
	return total, byItem, nil
}

func (r *mutationResolver) correctPurchase(ctx context.Context, input model.CorrectPurchaseInput) (*model.Purchase, error) {
	purchaseID, itemID, err := correctionTarget(input.Target)
	if err != nil {
		return nil, err
	}
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	delta := int64(input.AmountDeltaYen)
	if delta == 0 || delta > maxMoney || delta < -maxMoney || strings.TrimSpace(input.Reason) == "" || input.IdempotencyKey == "" {
		return nil, fmt.Errorf("%w: invalid correction", ErrBadInput)
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	householdUUID, purchaseUUID := generatedUUID(input.HouseholdID), generatedUUID(purchaseID)
	purchaseRow, err := models.Purchases.Query(sm.Where(psql.And(
		models.Purchases.Columns.ID.EQ(psql.Arg(purchaseUUID)),
		models.Purchases.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
	)), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	operation := "correctPurchase"
	fingerprint, err := mutationRequestFingerprint(input)
	if err != nil {
		return nil, err
	}
	existing, err := models.IdempotencyKeys.Query(sm.Where(psql.And(
		models.IdempotencyKeys.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.IdempotencyKeys.Columns.Key.EQ(psql.Arg(input.IdempotencyKey)),
		models.IdempotencyKeys.Columns.Operation.EQ(psql.Arg(operation)),
	))).One(ctx, executor)
	if err == nil && existing.ResourceID.Valid {
		if err := validateIdempotencyFingerprint(existing, fingerprint); err != nil {
			return nil, err
		}
		if err := tx.Rollback(ctx); err != nil {
			return nil, err
		}
		return loadPurchase(ctx, r.DB, purchaseID)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if purchaseRow.Status == string(model.PurchaseStatusDraft) {
		return nil, fmt.Errorf("%w: corrections require a posted purchase", ErrBadInput)
	}
	originalPeriod := accountingPeriod(purchaseRow.OccurredAt)
	if err := ensureAccountingPeriod(ctx, executor, householdUUID, originalPeriod); err != nil {
		return nil, err
	}
	originalPeriodRow, err := models.AccountingPeriods.Query(sm.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(originalPeriod)),
	)), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return nil, err
	}
	effectivePeriod := originalPeriod
	correctionAt := time.Now()
	if originalPeriodRow.Status == statusClosed {
		effectivePeriod = accountingPeriod(correctionAt)
		if err := lockAccountingPeriods(ctx, tx, input.HouseholdID, effectivePeriod); err != nil {
			return nil, err
		}
	}
	if originalPeriodRow.Status != "OPEN" && originalPeriodRow.Status != statusClosed {
		return nil, fmt.Errorf("%w: invalid accounting period state", ErrConflict)
	}
	effectivePeriodRow, err := models.AccountingPeriods.Query(sm.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(effectivePeriod)),
	)), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return nil, err
	}
	if effectivePeriodRow.Status == statusClosed {
		return nil, fmt.Errorf("%w: correction period is closed", ErrConflict)
	}
	var itemUUIDValue sql.Null[gofrsuuid.UUID]
	if itemID != nil {
		itemUUID := generatedUUID(*itemID)
		if _, err := models.PurchaseItems.Query(sm.Where(psql.And(
			models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemUUID)),
			models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)),
		))).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		} else if err != nil {
			return nil, err
		}
		itemUUIDValue = sql.Null[gofrsuuid.UUID]{V: itemUUID, Valid: true}
	}
	weights, err := correctionAllocationWeights(ctx, tx, purchaseID, itemID)
	if err != nil {
		return nil, err
	}
	if len(input.Allocations) > 0 && !sameAllocationWeights(input.Allocations, weights) {
		return nil, fmt.Errorf("%w: corrections must use the saved purchase allocation", ErrBadInput)
	}
	if delta < 0 {
		refundablePurchaseTotal, refundableByItem, err := refundablePurchaseAmounts(ctx, executor, purchaseUUID)
		if err != nil {
			return nil, err
		}
		if refundablePurchaseTotal < 0 || -delta > refundablePurchaseTotal {
			return nil, fmt.Errorf("%w: refund exceeds the remaining purchase amount", ErrBadInput)
		}
		if itemID != nil {
			itemUUID := generatedUUID(*itemID)
			refundableItemTotal := refundableByItem[itemUUID]
			if refundableItemTotal < 0 || -delta > refundableItemTotal {
				return nil, fmt.Errorf("%w: refund exceeds the remaining item amount", ErrBadInput)
			}
		}
	}
	shares, err := domain.Allocate(abs(delta), weights)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	correctionID, journalID, ledgerID := newID(), newID(), newID()
	correctionUUID, journalUUID, ledgerUUID := generatedUUID(correctionID), generatedUUID(journalID), generatedUUID(ledgerID)
	userUUID := generatedUUID(user.MemberID)
	sourceKind := sourceKindCorrection
	if _, err := models.Journals.Insert(&models.JournalSetter{
		ID: &journalUUID, HouseholdID: &householdUUID, SourceKind: &sourceKind, SourceID: &correctionUUID,
		IdempotencyKey: &input.IdempotencyKey, OccurredAt: &correctionAt, CreatedBy: &userUUID,
	}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	postings := make([]accounting.Posting, 0, len(shares)+len(input.PaymentAllocations)+1)
	for _, share := range shares {
		if share.Amount == 0 {
			continue
		}
		poolUUID := generatedUUID(share.PoolID)
		posting := accounting.Posting{PoolID: &poolUUID, AmountYen: share.Amount, Memo: input.Reason}
		if delta > 0 {
			posting.Side = accounting.Debit
		} else {
			posting.Side = accounting.Credit
		}
		postings = append(postings, posting)
	}
	var payments []model.PurchasePaymentInput
	if len(input.PaymentAllocations) > 0 {
		payments, err = validatePurchasePaymentInputs(ctx, tx, input.HouseholdID, input.PaymentAllocations)
		if err != nil {
			return nil, err
		}
		var paid int64
		for _, payment := range payments {
			var ok bool
			paid, ok = checkedAdd(paid, int64(payment.AmountYen))
			if !ok {
				return nil, fmt.Errorf("%w: payment total is too large", ErrBadInput)
			}
		}
		if paid != abs(delta) {
			return nil, fmt.Errorf("%w: correction payments must equal the correction amount", ErrBadInput)
		}
	} else {
		items, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, executor)
		if err != nil {
			return nil, err
		}
		var originalTotal int64
		for _, item := range items {
			originalTotal += item.AmountYen
		}
		var originalAccountID *string
		if purchaseRow.PaymentAccountID.Valid {
			accountID := purchaseRow.PaymentAccountID.V.String()
			originalAccountID = &accountID
		}
		payments, err = purchasePayments(ctx, tx, input.HouseholdID, purchaseID, originalAccountID, originalTotal)
		if err != nil {
			return nil, err
		}
		if len(payments) > 0 {
			weights := make([]domain.Weight, 0, len(payments))
			for _, payment := range payments {
				weights = append(weights, domain.Weight{PoolID: payment.PaymentAccountID, Value: int64(payment.AmountYen)})
			}
			allocatedPayments, err := domain.Allocate(abs(delta), weights)
			if err != nil {
				return nil, err
			}
			payments = payments[:0]
			for _, payment := range allocatedPayments {
				if payment.Amount > 0 {
					payments = append(payments, model.PurchasePaymentInput{PaymentAccountID: payment.PoolID, AmountYen: graphLong(payment.Amount)})
				}
			}
		}
	}
	if len(payments) == 0 {
		posting := accounting.Posting{AmountYen: abs(delta), Memo: "Correction offset"}
		if delta > 0 {
			posting.Side = accounting.Credit
		} else {
			posting.Side = accounting.Debit
		}
		postings = append(postings, posting)
	} else {
		for _, payment := range payments {
			accountUUID := generatedUUID(payment.PaymentAccountID)
			posting := accounting.Posting{AccountID: &accountUUID, AmountYen: int64(payment.AmountYen), Memo: "Correction payment"}
			if delta > 0 {
				posting.Side = accounting.Credit
			} else {
				posting.Side = accounting.Debit
			}
			postings = append(postings, posting)
		}
	}
	entries, err := accounting.BuildEntries(postings)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	if err := accounting.InsertEntries(ctx, executor, journalUUID, entries); err != nil {
		return nil, err
	}
	correctionLedgerKind := sourceKindCorrection
	noMemberValue := sql.Null[gofrsuuid.UUID]{}
	purchaseValue := sql.Null[gofrsuuid.UUID]{V: purchaseUUID, Valid: true}
	amount := abs(delta)
	if _, err := models.LedgerEntries.Insert(&models.LedgerEntrySetter{
		ID: &ledgerUUID, HouseholdID: &householdUUID, JournalID: &journalUUID, Kind: &correctionLedgerKind,
		OccurredAt: &correctionAt, Description: &input.Reason, AmountYen: &amount,
		FromMemberID: &noMemberValue, ToMemberID: &noMemberValue, PoolID: &noMemberValue, SourcePurchaseID: &purchaseValue,
	}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if _, err := models.Corrections.Insert(&models.CorrectionSetter{
		ID: &correctionUUID, HouseholdID: &householdUUID, PurchaseID: &purchaseUUID, ItemID: &itemUUIDValue,
		AmountDeltaYen: &delta, Reason: &input.Reason, JournalID: &journalUUID, CreatedBy: &userUUID,
		EffectivePeriod: &sql.Null[string]{V: effectivePeriod, Valid: true},
	}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if err := snapshotCorrectionMemberBurden(ctx, executor, input.HouseholdID, correctionUUID, purchaseUUID, itemUUIDValue, shares, delta); err != nil {
		return nil, err
	}
	for index, payment := range payments {
		accountUUID := generatedUUID(payment.PaymentAccountID)
		line := int32(index + 1)
		paymentAmount := int64(payment.AmountYen)
		if _, err := models.CorrectionPayments.Insert(&models.CorrectionPaymentSetter{
			CorrectionID: &correctionUUID, LineNo: &line, AccountID: &accountUUID, AmountYen: &paymentAmount,
		}).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	fundingExpenses, rules, err := correctionFundingExpense(ctx, executor, householdUUID, purchaseUUID, itemUUIDValue, shares)
	if err != nil {
		return nil, err
	}
	changes, err := domain.CalculateExpenseAdjustments(fundingExpenses, rules)
	if err != nil {
		return nil, err
	}
	for _, change := range changes {
		amount := change.MonthlyAdjustmentYen - change.AdvanceCreditYen
		if delta < 0 {
			amount = -amount
		}
		if amount == 0 {
			continue
		}
		adjustmentID, memberUUID := generatedUUID(newID()), generatedUUID(change.MemberID)
		if _, err := models.SettlementAdjustments.Insert(&models.SettlementAdjustmentSetter{
			ID: &adjustmentID, HouseholdID: &householdUUID, Period: &effectivePeriod, MemberID: &memberUUID,
			SourceKind: &sourceKind, SourceID: &correctionUUID, AmountYen: &amount,
		}).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	ledgerResource := sql.Null[gofrsuuid.UUID]{V: ledgerUUID, Valid: true}
	requestFingerprint := sql.Null[[]byte]{V: fingerprint[:], Valid: true}
	if _, err := models.IdempotencyKeys.Insert(&models.IdempotencyKeySetter{
		HouseholdID: &householdUUID, Key: &input.IdempotencyKey, Operation: &operation, ResourceID: &ledgerResource, RequestFingerprint: &requestFingerprint,
	}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "purchase.corrected", purchaseID, 1, []string{"correction"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadPurchase(ctx, r.DB, purchaseID)
}

func correctionTarget(target *model.CorrectPurchaseTargetInput) (string, *string, error) {
	if target == nil {
		return "", nil, fmt.Errorf("%w: correction target is required", ErrBadInput)
	}
	switch {
	case target.Purchase != nil && target.Item == nil:
		return target.Purchase.PurchaseID, nil, nil
	case target.Item != nil && target.Purchase == nil:
		itemID := target.Item.ItemID
		return target.Item.PurchaseID, &itemID, nil
	default:
		return "", nil, fmt.Errorf("%w: exactly one correction target is required", ErrBadInput)
	}
}

func abs(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
