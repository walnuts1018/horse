package purchase

import (
	"context"
	"crypto/sha256"
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
	"github.com/walnuts1018/horse/internal/application/settlement"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const correctionSourceKind = "CORRECTION"
const correctionStatusClosed = "CLOSED"
const correctionMaxMoney int64 = 1_000_000_000_000

var correctionErrMissingPurchaseFundingSnapshot = settlement.ErrMissingPurchaseFundingSnapshot

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

func sameAllocationWeights(inputs []Allocation, saved []domain.Weight) bool {
	if len(inputs) != len(saved) {
		return false
	}
	requested := make(map[string]int64, len(inputs))
	requestedGCD := int64(0)
	savedGCD := int64(0)
	for _, input := range inputs {
		if input.PoolID == "" || input.Weight <= 0 {
			return false
		}
		if _, exists := requested[input.PoolID]; exists {
			return false
		}
		value := input.Weight
		requested[input.PoolID] = value
		requestedGCD = positiveGCD(requestedGCD, value)
	}
	savedByPool := make(map[string]int64, len(saved))
	for _, weight := range saved {
		if weight.PoolID == "" || weight.Value <= 0 {
			return false
		}
		if _, exists := savedByPool[weight.PoolID]; exists {
			return false
		}
		savedByPool[weight.PoolID] = weight.Value
		savedGCD = positiveGCD(savedGCD, weight.Value)
	}
	if requestedGCD == 0 || savedGCD == 0 {
		return false
	}
	for poolID, value := range requested {
		if savedByPool[poolID]/savedGCD != value/requestedGCD {
			return false
		}
	}
	return true
}

func SameAllocationWeights(inputs []Allocation, saved []domain.Weight) bool {
	return sameAllocationWeights(inputs, saved)
}

func positiveGCD(a, b int64) int64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
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

func (s *Service) Correct(ctx context.Context, input CorrectionCommand, fingerprint [sha256.Size]byte) (CorrectionResult, error) {
	purchaseID, itemID, delta := input.PurchaseID, input.ItemID, input.AmountDeltaYen
	if delta == 0 || delta > correctionMaxMoney || delta < -correctionMaxMoney || strings.TrimSpace(input.Reason) == "" || input.IdempotencyKey == "" {
		return CorrectionResult{}, fmt.Errorf("%w: invalid correction", ErrBadInput)
	}
	householdUUID, err := parseUUID(input.HouseholdID, "household")
	if err != nil {
		return CorrectionResult{}, err
	}
	purchaseUUID, err := parseUUID(purchaseID, "purchase")
	if err != nil {
		return CorrectionResult{}, err
	}
	actorUUID, err := parseUUID(input.ActorID, "actor")
	if err != nil {
		return CorrectionResult{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CorrectionResult{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(actorUUID)),
	))).Exists(ctx, executor)
	if err != nil {
		return CorrectionResult{}, err
	}
	if !member {
		return CorrectionResult{}, ErrNotFound
	}
	purchaseRow, err := models.Purchases.Query(sm.Where(psql.And(
		models.Purchases.Columns.ID.EQ(psql.Arg(purchaseUUID)),
		models.Purchases.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
	)), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return CorrectionResult{}, ErrNotFound
	}
	if err != nil {
		return CorrectionResult{}, err
	}
	operation := "correctPurchase"
	existing, err := models.IdempotencyKeys.Query(sm.Where(psql.And(
		models.IdempotencyKeys.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.IdempotencyKeys.Columns.Key.EQ(psql.Arg(input.IdempotencyKey)),
		models.IdempotencyKeys.Columns.Operation.EQ(psql.Arg(operation)),
	))).One(ctx, executor)
	if err == nil && existing.ResourceID.Valid {
		if err := validateIdempotencyFingerprint(existing, fingerprint); err != nil {
			return CorrectionResult{}, err
		}
		if err := tx.Rollback(ctx); err != nil {
			return CorrectionResult{}, err
		}
		return CorrectionResult{PurchaseID: purchaseID}, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return CorrectionResult{}, err
	}
	if purchaseRow.Status == draftStatus {
		return CorrectionResult{}, fmt.Errorf("%w: corrections require a posted purchase", ErrBadInput)
	}
	originalPeriod := accounting.PeriodFor(purchaseRow.OccurredAt)
	if err := ensureAccountingPeriod(ctx, executor, householdUUID, originalPeriod); err != nil {
		return CorrectionResult{}, err
	}
	originalPeriodRow, err := models.AccountingPeriods.Query(sm.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(originalPeriod)),
	)), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return CorrectionResult{}, err
	}
	effectivePeriod := originalPeriod
	correctionAt := time.Now()
	if originalPeriodRow.Status == correctionStatusClosed {
		effectivePeriod = accounting.PeriodFor(correctionAt)
		if err := lockCorrectionAccountingPeriods(ctx, executor, householdUUID, effectivePeriod); err != nil {
			return CorrectionResult{}, err
		}
	}
	if originalPeriodRow.Status != "OPEN" && originalPeriodRow.Status != correctionStatusClosed {
		return CorrectionResult{}, fmt.Errorf("%w: invalid accounting period state", ErrConflict)
	}
	effectivePeriodRow, err := models.AccountingPeriods.Query(sm.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(effectivePeriod)),
	)), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return CorrectionResult{}, err
	}
	if effectivePeriodRow.Status == correctionStatusClosed {
		return CorrectionResult{}, fmt.Errorf("%w: correction period is closed", ErrConflict)
	}
	var itemUUIDValue sql.Null[gofrsuuid.UUID]
	if itemID != nil {
		itemUUID, err := parseUUID(*itemID, "purchase item")
		if err != nil {
			return CorrectionResult{}, err
		}
		if _, err := models.PurchaseItems.Query(sm.Where(psql.And(
			models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemUUID)),
			models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)),
		))).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
			return CorrectionResult{}, ErrNotFound
		} else if err != nil {
			return CorrectionResult{}, err
		}
		itemUUIDValue = sql.Null[gofrsuuid.UUID]{V: itemUUID, Valid: true}
	}
	weights, err := correctionAllocationWeights(ctx, tx, purchaseID, itemID)
	if err != nil {
		return CorrectionResult{}, err
	}
	if len(input.Allocations) > 0 && !sameAllocationWeights(input.Allocations, weights) {
		return CorrectionResult{}, fmt.Errorf("%w: corrections must use the saved purchase allocation", ErrBadInput)
	}
	if delta < 0 {
		refundablePurchaseTotal, refundableByItem, err := refundablePurchaseAmounts(ctx, executor, purchaseUUID)
		if err != nil {
			return CorrectionResult{}, err
		}
		if refundablePurchaseTotal < 0 || -delta > refundablePurchaseTotal {
			return CorrectionResult{}, fmt.Errorf("%w: refund exceeds the remaining purchase amount", ErrBadInput)
		}
		if itemID != nil {
			itemUUID := mustCorrectionUUID(*itemID)
			refundableItemTotal := refundableByItem[itemUUID]
			if refundableItemTotal < 0 || -delta > refundableItemTotal {
				return CorrectionResult{}, fmt.Errorf("%w: refund exceeds the remaining item amount", ErrBadInput)
			}
		}
	}
	shares, err := domain.Allocate(abs(delta), weights)
	if err != nil {
		return CorrectionResult{}, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	correctionUUID, journalUUID, ledgerUUID := newUUID(), newUUID(), newUUID()
	userUUID := actorUUID
	sourceKind := correctionSourceKind
	if _, err := models.Journals.Insert(&models.JournalSetter{
		ID: &journalUUID, HouseholdID: &householdUUID, SourceKind: &sourceKind, SourceID: &correctionUUID,
		IdempotencyKey: &input.IdempotencyKey, OccurredAt: &correctionAt, CreatedBy: &userUUID,
	}).Exec(ctx, executor); err != nil {
		return CorrectionResult{}, err
	}
	postings := make([]accounting.Posting, 0, len(shares)+len(input.PaymentAllocations)+1)
	for _, share := range shares {
		if share.Amount == 0 {
			continue
		}
		poolUUID := mustCorrectionUUID(share.PoolID)
		posting := accounting.Posting{PoolID: &poolUUID, AmountYen: share.Amount, Memo: input.Reason}
		if delta > 0 {
			posting.Side = accounting.Debit
		} else {
			posting.Side = accounting.Credit
		}
		postings = append(postings, posting)
	}
	var payments []Payment
	if len(input.PaymentAllocations) > 0 {
		payments, err = validateCorrectionPayments(ctx, executor, householdUUID, input.PaymentAllocations)
		if err != nil {
			return CorrectionResult{}, err
		}
		var paid int64
		for _, payment := range payments {
			var ok bool
			paid, ok = checkedAdd(paid, payment.AmountYen)
			if !ok {
				return CorrectionResult{}, fmt.Errorf("%w: payment total is too large", ErrBadInput)
			}
		}
		if paid != abs(delta) {
			return CorrectionResult{}, fmt.Errorf("%w: correction payments must equal the correction amount", ErrBadInput)
		}
	} else {
		items, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, executor)
		if err != nil {
			return CorrectionResult{}, err
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
		payments, err = correctionPurchasePayments(ctx, executor, householdUUID, purchaseUUID, originalAccountID, originalTotal)
		if err != nil {
			return CorrectionResult{}, err
		}
		if len(payments) > 0 {
			weights := make([]domain.Weight, 0, len(payments))
			for _, payment := range payments {
				weights = append(weights, domain.Weight{PoolID: payment.AccountID, Value: payment.AmountYen})
			}
			allocatedPayments, err := domain.Allocate(abs(delta), weights)
			if err != nil {
				return CorrectionResult{}, err
			}
			payments = payments[:0]
			for _, payment := range allocatedPayments {
				if payment.Amount > 0 {
					payments = append(payments, Payment{AccountID: payment.PoolID, AmountYen: payment.Amount})
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
			accountUUID := mustCorrectionUUID(payment.AccountID)
			posting := accounting.Posting{AccountID: &accountUUID, AmountYen: payment.AmountYen, Memo: "Correction payment"}
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
		return CorrectionResult{}, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	if err := accounting.InsertEntries(ctx, executor, journalUUID, entries); err != nil {
		return CorrectionResult{}, err
	}
	correctionLedgerKind := correctionSourceKind
	noMemberValue := sql.Null[gofrsuuid.UUID]{}
	purchaseValue := sql.Null[gofrsuuid.UUID]{V: purchaseUUID, Valid: true}
	amount := abs(delta)
	if _, err := models.LedgerEntries.Insert(&models.LedgerEntrySetter{
		ID: &ledgerUUID, HouseholdID: &householdUUID, JournalID: &journalUUID, Kind: &correctionLedgerKind,
		OccurredAt: &correctionAt, Description: &input.Reason, AmountYen: &amount,
		FromMemberID: &noMemberValue, ToMemberID: &noMemberValue, PoolID: &noMemberValue, SourcePurchaseID: &purchaseValue,
	}).Exec(ctx, executor); err != nil {
		return CorrectionResult{}, err
	}
	if _, err := models.Corrections.Insert(&models.CorrectionSetter{
		ID: &correctionUUID, HouseholdID: &householdUUID, PurchaseID: &purchaseUUID, ItemID: &itemUUIDValue,
		AmountDeltaYen: &delta, Reason: &input.Reason, JournalID: &journalUUID, CreatedBy: &userUUID,
		EffectivePeriod: new(sql.Null[string]{V: effectivePeriod, Valid: true}),
	}).Exec(ctx, executor); err != nil {
		return CorrectionResult{}, err
	}
	if err := snapshotCorrectionMemberBurden(ctx, executor, input.HouseholdID, correctionUUID, purchaseUUID, itemUUIDValue, shares, delta); err != nil {
		return CorrectionResult{}, err
	}
	for index, payment := range payments {
		accountUUID := mustCorrectionUUID(payment.AccountID)
		line := int32(index + 1)
		paymentAmount := payment.AmountYen
		if _, err := models.CorrectionPayments.Insert(&models.CorrectionPaymentSetter{
			CorrectionID: &correctionUUID, LineNo: &line, AccountID: &accountUUID, AmountYen: &paymentAmount,
		}).Exec(ctx, executor); err != nil {
			return CorrectionResult{}, err
		}
	}
	fundingExpenses, rules, err := correctionFundingExpense(ctx, executor, householdUUID, purchaseUUID, itemUUIDValue, shares)
	if err != nil {
		return CorrectionResult{}, err
	}
	changes, err := domain.CalculateExpenseAdjustments(fundingExpenses, rules)
	if err != nil {
		return CorrectionResult{}, err
	}
	for _, change := range changes {
		amount := change.MonthlyAdjustmentYen - change.AdvanceCreditYen
		if delta < 0 {
			amount = -amount
		}
		if amount == 0 {
			continue
		}
		adjustmentID, memberUUID := newUUID(), mustCorrectionUUID(change.MemberID)
		if _, err := models.SettlementAdjustments.Insert(&models.SettlementAdjustmentSetter{
			ID: &adjustmentID, HouseholdID: &householdUUID, Period: &effectivePeriod, MemberID: &memberUUID,
			SourceKind: &sourceKind, SourceID: &correctionUUID, AmountYen: &amount,
		}).Exec(ctx, executor); err != nil {
			return CorrectionResult{}, err
		}
	}
	ledgerResource := sql.Null[gofrsuuid.UUID]{V: ledgerUUID, Valid: true}
	requestFingerprint := sql.Null[[]byte]{V: fingerprint[:], Valid: true}
	if _, err := models.IdempotencyKeys.Insert(&models.IdempotencyKeySetter{
		HouseholdID: &householdUUID, Key: &input.IdempotencyKey, Operation: &operation, ResourceID: &ledgerResource, RequestFingerprint: &requestFingerprint,
	}).Exec(ctx, executor); err != nil {
		return CorrectionResult{}, err
	}
	if err := accounting.RecordEvent(ctx, executor, householdUUID, purchaseUUID, "purchase.corrected", 1, []string{"correction"}, actorUUID); err != nil {
		return CorrectionResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CorrectionResult{}, err
	}
	return CorrectionResult{PurchaseID: purchaseID}, nil
}

func lockCorrectionAccountingPeriods(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, periods ...string) error {
	slices.Sort(periods)
	periods = slices.Compact(periods)
	for _, period := range periods {
		if err := ensureAccountingPeriod(ctx, executor, householdID, period); err != nil {
			return err
		}
		row, err := models.AccountingPeriods.Query(sm.Where(psql.And(
			models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.AccountingPeriods.Columns.Period.EQ(psql.Arg(period)),
		)), sm.ForUpdate()).One(ctx, executor)
		if err != nil {
			return err
		}
		if row.Status == correctionStatusClosed {
			return ErrConflict
		}
	}
	return nil
}

func validateCorrectionPayments(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, inputs []Payment) ([]Payment, error) {
	result := make([]Payment, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		if input.AccountID == "" || input.AmountYen <= 0 || input.AmountYen > correctionMaxMoney {
			return nil, fmt.Errorf("%w: payment allocations require an account and positive amount", ErrBadInput)
		}
		if _, exists := seen[input.AccountID]; exists {
			return nil, fmt.Errorf("%w: payment account may appear only once", ErrBadInput)
		}
		seen[input.AccountID] = struct{}{}
		accountID, err := parseUUID(input.AccountID, "payment account")
		if err != nil {
			return nil, fmt.Errorf("%w: invalid payment account id", ErrBadInput)
		}
		_, err = models.Accounts.Query(sm.Where(psql.And(
			models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Accounts.Columns.ID.EQ(psql.Arg(accountID)),
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		result = append(result, input)
	}
	return result, nil
}

func correctionPurchasePayments(ctx context.Context, executor bob.Executor, householdID, purchaseID gofrsuuid.UUID, legacyAccountID *string, total int64) ([]Payment, error) {
	rows, err := models.PurchasePayments.Query(sm.Where(psql.And(
		models.PurchasePayments.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.PurchasePayments.Columns.PurchaseID.EQ(psql.Arg(purchaseID)),
	)), sm.OrderBy(models.PurchasePayments.Columns.LineNo)).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	payments := make([]Payment, 0, len(rows))
	var paid int64
	for _, row := range rows {
		var ok bool
		paid, ok = checkedAdd(paid, row.AmountYen)
		if !ok {
			return nil, fmt.Errorf("%w: payment total is too large", ErrBadInput)
		}
		payments = append(payments, Payment{AccountID: row.AccountID.String(), AmountYen: row.AmountYen})
	}
	if len(payments) == 0 && legacyAccountID != nil {
		payments = append(payments, Payment{AccountID: *legacyAccountID, AmountYen: total})
		paid = total
	}
	if len(payments) > 0 && paid != total {
		return nil, fmt.Errorf("%w: payment allocations must equal purchase total", ErrBadInput)
	}
	return payments, nil
}

func mustCorrectionUUID(value string) gofrsuuid.UUID {
	return gofrsuuid.Must(gofrsuuid.FromString(value))
}

func abs(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
