package purchase

import (
	"context"
	"crypto/sha256"
	"database/sql"
	stdjson "encoding/json"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/settlement"
	"github.com/walnuts1018/horse/internal/application/txcleanup"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const correctionSourceKind = "CORRECTION"
const correctionStatusClosed = "CLOSED"
const correctionMaxMoney int64 = 1_000_000_000_000

var correctionErrMissingPurchaseFundingSnapshot = settlement.ErrMissingPurchaseFundingSnapshot

type statementRefundEvidence struct {
	DocumentID     gofrsuuid.UUID
	AmountDeltaYen int64
	Region         *ingestion.Region
	Observation    stdjson.RawMessage
}

func loadStatementRefundEvidence(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, documentID, reference string) (*statementRefundEvidence, error) {
	documentUUID, err := parseUUID(documentID, "source document")
	if err != nil {
		return nil, err
	}
	document, err := models.SourceDocuments.Query(sm.Where(models.SourceDocuments.Columns.ID.EQ(psql.Arg(documentUUID))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) || err == nil && document.HouseholdID != householdID {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if document.SourceKind != "card" && document.SourceKind != "bank" {
		return nil, fmt.Errorf("%w: refunds require a card or bank statement", ErrBadInput)
	}
	_, err = models.DocumentConfirmations.Query(sm.Where(psql.And(
		models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(documentUUID)),
		models.DocumentConfirmations.Columns.SourceReference.EQ(psql.Arg(reference)),
	))).One(ctx, executor)
	if err == nil {
		return nil, fmt.Errorf("%w: statement row is already confirmed", ErrConflict)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	job, err := models.ExtractionJobs.Query(sm.Where(psql.And(
		models.ExtractionJobs.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.ExtractionJobs.Columns.SourceDocumentID.EQ(psql.Arg(documentUUID)),
		models.ExtractionJobs.Columns.Status.EQ(psql.Arg("succeeded")),
	)), sm.OrderBy(models.ExtractionJobs.Columns.Version).Desc(), sm.Limit(1)).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: statement extraction is unavailable", ErrBadInput)
	}
	if err != nil {
		return nil, err
	}
	if !job.Result.Valid {
		return nil, fmt.Errorf("%w: statement extraction is unavailable", ErrBadInput)
	}
	var verification ingestion.Verification
	if err := json.Unmarshal(job.Result.V.Val, &verification); err != nil {
		return nil, err
	}
	index := slices.IndexFunc(verification.Statements, func(entry ingestion.StatementEntry) bool {
		return entry.SourceReference == reference || entry.SourceReference == "" && reference == fmt.Sprintf("row:%d", entry.Row)
	})
	if index < 0 {
		return nil, ErrNotFound
	}
	entry := verification.Statements[index]
	if entry.Kind != "REFUND" || entry.Amount >= 0 || entry.Region == nil {
		return nil, fmt.Errorf("%w: statement row is not a refundable negative amount", ErrBadInput)
	}
	observation, err := json.Marshal(struct {
		JobID      string                   `json:"jobId"`
		Statement  ingestion.StatementEntry `json:"statement"`
		SourceHash string                   `json:"sourceHash"`
	}{JobID: job.ID.String(), Statement: entry, SourceHash: document.Sha256})
	if err != nil {
		return nil, err
	}
	return &statementRefundEvidence{DocumentID: documentUUID, AmountDeltaYen: entry.Amount, Region: entry.Region, Observation: observation}, nil
}

func isUniqueViolation(err error) bool {
	pgError, ok := errors.AsType[*pgconn.PgError](err)
	return ok && pgError.Code == "23505"
}

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

type correctionRequest struct {
	input         CorrectionCommand
	householdID   gofrsuuid.UUID
	purchaseID    gofrsuuid.UUID
	actorID       gofrsuuid.UUID
	delta         int64
	fromStatement bool
	operation     string
}

func validateCorrectionRequest(input CorrectionCommand) (correctionRequest, error) {
	purchaseID, delta := input.PurchaseID, input.AmountDeltaYen
	fromStatement := input.SourceDocumentID != "" || input.SourceReference != ""
	if delta == 0 && !fromStatement || delta > correctionMaxMoney || delta < -correctionMaxMoney || strings.TrimSpace(input.Reason) == "" || input.IdempotencyKey == "" || fromStatement && (input.SourceDocumentID == "" || input.SourceReference == "" || len(input.SourceReference) > 256) {
		return correctionRequest{}, fmt.Errorf("%w: invalid correction", ErrBadInput)
	}
	householdUUID, err := parseUUID(input.HouseholdID, "household")
	if err != nil {
		return correctionRequest{}, err
	}
	purchaseUUID, err := parseUUID(purchaseID, "purchase")
	if err != nil {
		return correctionRequest{}, err
	}
	actorUUID, err := parseUUID(input.ActorID, "actor")
	if err != nil {
		return correctionRequest{}, err
	}
	operation := "correctPurchase"
	if fromStatement {
		operation = "correctPurchaseFromStatement"
	}
	return correctionRequest{
		input: input, householdID: householdUUID, purchaseID: purchaseUUID, actorID: actorUUID,
		delta: delta, fromStatement: fromStatement, operation: operation,
	}, nil
}

func lockCorrectionPurchase(ctx context.Context, executor bob.Executor, request correctionRequest) (*models.Purchase, error) {
	member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(request.householdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(request.actorID)),
		models.HouseholdMembers.Columns.LeftAt.IsNull(),
	))).Exists(ctx, executor)
	if err != nil {
		return nil, err
	}
	if !member {
		return nil, ErrNotFound
	}
	purchase, err := models.Purchases.Query(sm.Where(psql.And(
		models.Purchases.Columns.ID.EQ(psql.Arg(request.purchaseID)),
		models.Purchases.Columns.HouseholdID.EQ(psql.Arg(request.householdID)),
	)), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return purchase, nil
}

func correctionIdempotencyReplay(ctx context.Context, tx pgx.Tx, executor bob.Executor, request correctionRequest, fingerprint [sha256.Size]byte) (bool, error) {
	existing, err := models.IdempotencyKeys.Query(sm.Where(psql.And(
		models.IdempotencyKeys.Columns.HouseholdID.EQ(psql.Arg(request.householdID)),
		models.IdempotencyKeys.Columns.Key.EQ(psql.Arg(request.input.IdempotencyKey)),
		models.IdempotencyKeys.Columns.Operation.EQ(psql.Arg(request.operation)),
	))).One(ctx, executor)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if !existing.ResourceID.Valid {
		return false, nil
	}
	if err := validateIdempotencyFingerprint(existing, fingerprint); err != nil {
		return false, err
	}
	if err := tx.Rollback(ctx); err != nil {
		return false, err
	}
	return true, nil
}

type correctionPlan struct {
	delta             int64
	statementEvidence *statementRefundEvidence
	effectivePeriod   string
	occurredAt        time.Time
	itemID            sql.Null[gofrsuuid.UUID]
	shares            []domain.AllocationAmount
}

func prepareCorrectionPlan(ctx context.Context, tx pgx.Tx, executor bob.Executor, request correctionRequest, purchase *models.Purchase) (correctionPlan, error) {
	plan := correctionPlan{delta: request.delta}
	if request.fromStatement {
		evidence, err := loadStatementRefundEvidence(ctx, executor, request.householdID, request.input.SourceDocumentID, request.input.SourceReference)
		if err != nil {
			return correctionPlan{}, err
		}
		plan.statementEvidence = evidence
		plan.delta = evidence.AmountDeltaYen
		if plan.delta == 0 || plan.delta > 0 || plan.delta < -correctionMaxMoney {
			return correctionPlan{}, fmt.Errorf("%w: statement row is not a refundable negative amount", ErrBadInput)
		}
	}
	if purchase.Status == draftStatus {
		return correctionPlan{}, fmt.Errorf("%w: corrections require a posted purchase", ErrBadInput)
	}
	originalPeriod := accounting.PeriodFor(purchase.OccurredAt)
	if err := ensureAccountingPeriod(ctx, executor, request.householdID, originalPeriod); err != nil {
		return correctionPlan{}, err
	}
	originalPeriodRow, err := models.AccountingPeriods.Query(sm.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(request.householdID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(originalPeriod)),
	)), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return correctionPlan{}, err
	}
	plan.effectivePeriod = originalPeriod
	plan.occurredAt = time.Now()
	if originalPeriodRow.Status == correctionStatusClosed {
		plan.effectivePeriod = accounting.PeriodFor(plan.occurredAt)
		if err := lockCorrectionAccountingPeriods(ctx, executor, request.householdID, plan.effectivePeriod); err != nil {
			return correctionPlan{}, err
		}
	}
	if originalPeriodRow.Status != "OPEN" && originalPeriodRow.Status != correctionStatusClosed {
		return correctionPlan{}, fmt.Errorf("%w: invalid accounting period state", ErrConflict)
	}
	effectivePeriodRow, err := models.AccountingPeriods.Query(sm.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(request.householdID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(plan.effectivePeriod)),
	)), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return correctionPlan{}, err
	}
	if effectivePeriodRow.Status == correctionStatusClosed {
		return correctionPlan{}, fmt.Errorf("%w: correction period is closed", ErrConflict)
	}
	if request.input.ItemID != nil {
		itemUUID, err := parseUUID(*request.input.ItemID, "purchase item")
		if err != nil {
			return correctionPlan{}, err
		}
		if _, err := models.PurchaseItems.Query(sm.Where(psql.And(
			models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemUUID)),
			models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(request.purchaseID)),
		))).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
			return correctionPlan{}, ErrNotFound
		} else if err != nil {
			return correctionPlan{}, err
		}
		plan.itemID = sql.Null[gofrsuuid.UUID]{V: itemUUID, Valid: true}
	}
	weights, err := correctionAllocationWeights(ctx, tx, request.input.PurchaseID, request.input.ItemID)
	if err != nil {
		return correctionPlan{}, err
	}
	if len(request.input.Allocations) > 0 && !sameAllocationWeights(request.input.Allocations, weights) {
		return correctionPlan{}, fmt.Errorf("%w: corrections must use the saved purchase allocation", ErrBadInput)
	}
	if plan.delta < 0 {
		refundablePurchaseTotal, refundableByItem, err := refundablePurchaseAmounts(ctx, executor, request.purchaseID)
		if err != nil {
			return correctionPlan{}, err
		}
		if refundablePurchaseTotal < 0 || -plan.delta > refundablePurchaseTotal {
			return correctionPlan{}, fmt.Errorf("%w: refund exceeds the remaining purchase amount", ErrBadInput)
		}
		if request.input.ItemID != nil {
			refundableItemTotal := refundableByItem[mustCorrectionUUID(*request.input.ItemID)]
			if refundableItemTotal < 0 || -plan.delta > refundableItemTotal {
				return correctionPlan{}, fmt.Errorf("%w: refund exceeds the remaining item amount", ErrBadInput)
			}
		}
	}
	plan.shares, err = domain.Allocate(abs(plan.delta), weights)
	if err != nil {
		return correctionPlan{}, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	return plan, nil
}

func persistCorrection(ctx context.Context, executor bob.Executor, request correctionRequest, purchase *models.Purchase, plan correctionPlan, fingerprint [sha256.Size]byte) error {
	input := request.input
	householdUUID, purchaseUUID, actorUUID := request.householdID, request.purchaseID, request.actorID
	delta, statementEvidence := plan.delta, plan.statementEvidence
	effectivePeriod, correctionAt := plan.effectivePeriod, plan.occurredAt
	itemUUIDValue, shares := plan.itemID, plan.shares
	var err error
	correctionUUID, journalUUID, ledgerUUID := newUUID(), newUUID(), newUUID()
	userUUID := actorUUID
	sourceKind := correctionSourceKind
	if _, err := models.Journals.Insert(&models.JournalSetter{
		ID: &journalUUID, HouseholdID: &householdUUID, SourceKind: &sourceKind, SourceID: &correctionUUID,
		IdempotencyKey: &input.IdempotencyKey, OccurredAt: &correctionAt, CreatedBy: &userUUID,
	}).Exec(ctx, executor); err != nil {
		return err
	}
	var payments []Payment
	if len(input.PaymentAllocations) > 0 {
		payments, err = validateCorrectionPayments(ctx, executor, householdUUID, input.PaymentAllocations)
		if err != nil {
			return err
		}
		var paid int64
		for _, payment := range payments {
			var ok bool
			paid, ok = checkedAdd(paid, payment.AmountYen)
			if !ok {
				return fmt.Errorf("%w: payment total is too large", ErrBadInput)
			}
		}
		if paid != abs(delta) {
			return fmt.Errorf("%w: correction payments must equal the correction amount", ErrBadInput)
		}
	} else {
		items, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, executor)
		if err != nil {
			return err
		}
		var originalTotal int64
		for _, item := range items {
			originalTotal += item.AmountYen
		}
		var originalAccountID *string
		if purchase.PaymentAccountID.Valid {
			accountID := purchase.PaymentAccountID.V.String()
			originalAccountID = &accountID
		}
		payments, err = correctionPurchasePayments(ctx, executor, householdUUID, purchaseUUID, originalAccountID, originalTotal)
		if err != nil {
			return err
		}
		if len(payments) > 0 {
			weights := make([]domain.Weight, 0, len(payments))
			for _, payment := range payments {
				weights = append(weights, domain.Weight{PoolID: payment.AccountID, Value: payment.AmountYen})
			}
			allocatedPayments, err := domain.Allocate(abs(delta), weights)
			if err != nil {
				return err
			}
			payments = payments[:0]
			for _, payment := range allocatedPayments {
				if payment.Amount > 0 {
					payments = append(payments, Payment{AccountID: payment.PoolID, AmountYen: payment.Amount})
				}
			}
		}
	}
	postings := buildCorrectionPostings(shares, delta, input.Reason, payments)
	entries, err := accounting.BuildEntries(postings)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	if err := accounting.InsertEntries(ctx, executor, journalUUID, entries); err != nil {
		return err
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
		return err
	}
	if _, err := models.Corrections.Insert(&models.CorrectionSetter{
		ID: &correctionUUID, HouseholdID: &householdUUID, PurchaseID: &purchaseUUID, ItemID: &itemUUIDValue,
		AmountDeltaYen: &delta, Reason: &input.Reason, JournalID: &journalUUID, CreatedBy: &userUUID,
		EffectivePeriod: new(sql.Null[string]{V: effectivePeriod, Valid: true}),
	}).Exec(ctx, executor); err != nil {
		return err
	}
	if statementEvidence != nil {
		correctionIDValue := sql.Null[gofrsuuid.UUID]{V: correctionUUID, Valid: true}
		if _, err := models.DocumentConfirmations.Insert(&models.DocumentConfirmationSetter{
			SourceDocumentID: &statementEvidence.DocumentID, SourceReference: &input.SourceReference,
			HouseholdID: &householdUUID, CorrectionID: &correctionIDValue, CreatedBy: &actorUUID,
		}).Exec(ctx, executor); err != nil {
			if isUniqueViolation(err) {
				return ErrConflict
			}
			return err
		}
		var region sql.Null[types.JSON[stdjson.RawMessage]]
		if statementEvidence.Region != nil {
			encoded, err := json.Marshal(statementEvidence.Region)
			if err != nil {
				return err
			}
			region = sql.Null[types.JSON[stdjson.RawMessage]]{V: types.JSON[stdjson.RawMessage]{Val: encoded}, Valid: true}
		}
		observationJSON := types.JSON[stdjson.RawMessage]{Val: statementEvidence.Observation}
		if _, err := models.CorrectionEvidenceLinks.Insert(&models.CorrectionEvidenceLinkSetter{
			HouseholdID: &householdUUID, SourceDocumentID: &statementEvidence.DocumentID, SourceReference: &input.SourceReference,
			CorrectionID: &correctionUUID, EvidenceRegion: &region, Observation: &observationJSON,
		}).Exec(ctx, executor); err != nil {
			if isUniqueViolation(err) {
				return ErrConflict
			}
			return err
		}
	}
	if err := snapshotCorrectionMemberBurden(ctx, executor, input.HouseholdID, correctionUUID, purchaseUUID, itemUUIDValue, shares, delta); err != nil {
		return err
	}
	for index, payment := range payments {
		accountUUID := mustCorrectionUUID(payment.AccountID)
		line := int32(index + 1)
		paymentAmount := payment.AmountYen
		if _, err := models.CorrectionPayments.Insert(&models.CorrectionPaymentSetter{
			CorrectionID: &correctionUUID, LineNo: &line, AccountID: &accountUUID, AmountYen: &paymentAmount,
		}).Exec(ctx, executor); err != nil {
			return err
		}
	}
	fundingExpenses, rules, err := correctionFundingExpense(ctx, executor, householdUUID, purchaseUUID, itemUUIDValue, shares)
	if err != nil {
		return err
	}
	changes, err := domain.CalculateExpenseAdjustments(fundingExpenses, rules)
	if err != nil {
		return err
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
			return err
		}
	}
	ledgerResource := sql.Null[gofrsuuid.UUID]{V: ledgerUUID, Valid: true}
	requestFingerprint := sql.Null[[]byte]{V: fingerprint[:], Valid: true}
	if _, err := models.IdempotencyKeys.Insert(&models.IdempotencyKeySetter{
		HouseholdID: &householdUUID, Key: &input.IdempotencyKey, Operation: &request.operation, ResourceID: &ledgerResource, RequestFingerprint: &requestFingerprint,
	}).Exec(ctx, executor); err != nil {
		return err
	}
	if err := accounting.RecordEvent(ctx, executor, householdUUID, purchaseUUID, "purchase.corrected", 1, []string{"correction"}, actorUUID); err != nil {
		return err
	}
	return nil
}

func (s *Service) Correct(ctx context.Context, input CorrectionCommand, fingerprint [sha256.Size]byte) (CorrectionResult, error) {
	request, err := validateCorrectionRequest(input)
	if err != nil {
		return CorrectionResult{}, err
	}
	purchaseID := request.input.PurchaseID
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CorrectionResult{}, err
	}
	defer txcleanup.Rollback(ctx, tx)
	executor := bobpgx.NewTx(tx, nil)
	purchaseRow, err := lockCorrectionPurchase(ctx, executor, request)
	if err != nil {
		return CorrectionResult{}, err
	}
	replayed, err := correctionIdempotencyReplay(ctx, tx, executor, request, fingerprint)
	if err != nil {
		return CorrectionResult{}, err
	}
	if replayed {
		return CorrectionResult{PurchaseID: purchaseID}, nil
	}
	plan, err := prepareCorrectionPlan(ctx, tx, executor, request, purchaseRow)
	if err != nil {
		return CorrectionResult{}, err
	}
	if err := persistCorrection(ctx, executor, request, purchaseRow, plan, fingerprint); err != nil {
		return CorrectionResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CorrectionResult{}, err
	}
	return CorrectionResult{PurchaseID: purchaseID}, nil
}

func buildCorrectionPostings(shares []domain.AllocationAmount, delta int64, reason string, payments []Payment) []accounting.Posting {
	postings := make([]accounting.Posting, 0, len(shares)+len(payments)+1)
	poolSide, paymentSide := accounting.Credit, accounting.Debit
	if delta > 0 {
		poolSide, paymentSide = accounting.Debit, accounting.Credit
	}
	for _, share := range shares {
		if share.Amount == 0 {
			continue
		}
		poolUUID := mustCorrectionUUID(share.PoolID)
		postings = append(postings, accounting.Posting{PoolID: &poolUUID, Side: poolSide, AmountYen: share.Amount, Memo: reason})
	}
	if len(payments) == 0 {
		return append(postings, accounting.Posting{Side: paymentSide, AmountYen: abs(delta), Memo: "Correction offset"})
	}
	for _, payment := range payments {
		accountUUID := mustCorrectionUUID(payment.AccountID)
		postings = append(postings, accounting.Posting{AccountID: &accountUUID, Side: paymentSide, AmountYen: payment.AmountYen, Memo: "Correction payment"})
	}
	return postings
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
