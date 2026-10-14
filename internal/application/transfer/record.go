package transfer

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
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/application/txcleanup"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

var (
	ErrBadInput = apperror.ErrBadInput
	ErrConflict = apperror.ErrConflict
	ErrNotFound = apperror.ErrNotFound
)

const (
	loanKind      = "LOAN"
	repaymentKind = "REPAYMENT"
)

const maxMoney int64 = 1_000_000_000_000

type Command struct {
	ActorID              string
	HouseholdID          string
	FromMemberID         string
	ToMemberID           string
	Kind                 string
	LoanID               *string
	PaymentAccountID     *string
	DestinationAccountID *string
	AmountYen            int64
	OccurredAt           time.Time
	Description          string
	IdempotencyKey       string
	RequestFingerprint   [sha256.Size]byte
	SourceDocumentID     string
	SourceReference      string
	SourceAccountID      string
}

type Result struct {
	LedgerEntryID string
}

type Workflow struct {
	db *pgxpool.Pool
}

func New(db *pgxpool.Pool) *Workflow {
	return &Workflow{db: db}
}

func (w *Workflow) Do(ctx context.Context, command Command) (Result, error) {
	statementProvided, validationErr := validateCommandRules(command)
	if validationErr != nil {
		return Result{}, validationErr
	}

	householdID, err := parseID(command.HouseholdID, "household")
	if err != nil {
		return Result{}, err
	}
	actorID, err := parseID(command.ActorID, "actor")
	if err != nil {
		return Result{}, err
	}
	fromMemberID, err := parseID(command.FromMemberID, "source member")
	if err != nil {
		return Result{}, err
	}
	toMemberID, err := parseID(command.ToMemberID, "destination member")
	if err != nil {
		return Result{}, err
	}

	tx, err := w.db.Begin(ctx)
	if err != nil {
		return Result{}, err
	}
	defer txcleanup.Rollback(ctx, tx)
	executor := bobpgx.NewTx(tx, nil)
	for _, memberID := range []gofrsuuid.UUID{actorID, fromMemberID, toMemberID} {
		if err := requireHouseholdMember(ctx, executor, householdID, memberID); err != nil {
			return Result{}, err
		}
	}

	operation := "recordTransfer"
	existing, err := models.IdempotencyKeys.Query(sm.Where(psql.And(
		models.IdempotencyKeys.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.IdempotencyKeys.Columns.Key.EQ(psql.Arg(command.IdempotencyKey)),
		models.IdempotencyKeys.Columns.Operation.EQ(psql.Arg(operation)),
	))).One(ctx, executor)
	if err == nil {
		if !existing.RequestFingerprint.Valid || !slices.Equal(existing.RequestFingerprint.V, command.RequestFingerprint[:]) {
			return Result{}, fmt.Errorf("%w: idempotency key was used with a different request", ErrConflict)
		}
		if !existing.ResourceID.Valid {
			return Result{}, fmt.Errorf("%w: idempotency record has no resource", ErrConflict)
		}
		if statementProvided {
			if err := requireStatementTransferConfirmation(ctx, executor, householdID, existing.ResourceID.V, command.SourceDocumentID, command.SourceReference); err != nil {
				return Result{}, err
			}
		}
		if err := tx.Rollback(ctx); err != nil {
			return Result{}, err
		}
		return Result{LedgerEntryID: existing.ResourceID.V.String()}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Result{}, err
	}
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Result{}, ErrNotFound
		}
		return Result{}, err
	}
	existing, err = models.IdempotencyKeys.Query(sm.Where(psql.And(
		models.IdempotencyKeys.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.IdempotencyKeys.Columns.Key.EQ(psql.Arg(command.IdempotencyKey)),
		models.IdempotencyKeys.Columns.Operation.EQ(psql.Arg(operation)),
	))).One(ctx, executor)
	if err == nil {
		if !existing.RequestFingerprint.Valid || !slices.Equal(existing.RequestFingerprint.V, command.RequestFingerprint[:]) {
			return Result{}, fmt.Errorf("%w: idempotency key was used with a different request", ErrConflict)
		}
		if !existing.ResourceID.Valid {
			return Result{}, fmt.Errorf("%w: idempotency record has no resource", ErrConflict)
		}
		if statementProvided {
			if err := requireStatementTransferConfirmation(ctx, executor, householdID, existing.ResourceID.V, command.SourceDocumentID, command.SourceReference); err != nil {
				return Result{}, err
			}
		}
		if err := tx.Rollback(ctx); err != nil {
			return Result{}, err
		}
		return Result{LedgerEntryID: existing.ResourceID.V.String()}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Result{}, err
	}

	period := accounting.PeriodFor(command.OccurredAt)
	if _, err := models.AccountingPeriods.Insert(&models.AccountingPeriodSetter{
		HouseholdID: &householdID, Period: &period, Status: new("OPEN"), Revision: new(int64(0)),
	}, im.OnConflict("household_id", "period").DoNothing()).Exec(ctx, executor); err != nil {
		return Result{}, err
	}
	periodRow, err := models.AccountingPeriods.Query(sm.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(period)),
	)), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return Result{}, err
	}
	if periodRow.Status == "CLOSED" {
		return Result{}, fmt.Errorf("%w: accounting period is closed", ErrConflict)
	}

	var paymentPoolID *gofrsuuid.UUID
	var paymentPoolKind string
	var paymentAccountID, destinationAccountID *gofrsuuid.UUID
	if command.PaymentAccountID != nil {
		id, parseErr := parseID(*command.PaymentAccountID, "payment account")
		if parseErr != nil {
			return Result{}, parseErr
		}
		account, accountErr := models.Accounts.Query(sm.Where(psql.And(
			models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Accounts.Columns.ID.EQ(psql.Arg(id)),
		))).One(ctx, executor)
		if errors.Is(accountErr, sql.ErrNoRows) {
			return Result{}, ErrNotFound
		}
		if accountErr != nil {
			return Result{}, accountErr
		}
		if account.Kind == "CARD" {
			return Result{}, fmt.Errorf("%w: transfer source must be a non-card account", ErrBadInput)
		}
		if !account.PoolID.Valid {
			return Result{}, fmt.Errorf("%w: payment account is not assigned to a pool", ErrBadInput)
		}
		pool, poolErr := models.Pools.Query(sm.Where(psql.And(
			models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Pools.Columns.ID.EQ(psql.Arg(account.PoolID.V)),
		))).One(ctx, executor)
		if poolErr != nil {
			return Result{}, poolErr
		}
		paymentAccountID = &id
		paymentPoolID = &account.PoolID.V
		paymentPoolKind = pool.Kind
		if pool.Kind == "PERSONAL" {
			fundingRule, ruleErr := models.PoolFundingRules.Query(sm.Where(psql.And(
				models.PoolFundingRules.Columns.HouseholdID.EQ(psql.Arg(householdID)),
				models.PoolFundingRules.Columns.PoolID.EQ(psql.Arg(pool.ID)),
				models.PoolFundingRules.Columns.Version.EQ(psql.Arg(pool.FundingRuleVersion)),
			))).One(ctx, executor)
			if ruleErr != nil {
				return Result{}, ruleErr
			}
			if !fundingRule.OwnerMemberID.Valid || fundingRule.OwnerMemberID.V != fromMemberID {
				return Result{}, fmt.Errorf("%w: personal payment account must belong to the source member", ErrBadInput)
			}
		}
	}
	if command.DestinationAccountID != nil {
		id, parseErr := parseID(*command.DestinationAccountID, "destination account")
		if parseErr != nil {
			return Result{}, parseErr
		}
		account, accountErr := models.Accounts.Query(sm.Where(psql.And(
			models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Accounts.Columns.ID.EQ(psql.Arg(id)),
		))).One(ctx, executor)
		if errors.Is(accountErr, sql.ErrNoRows) {
			return Result{}, ErrNotFound
		}
		if accountErr != nil {
			return Result{}, accountErr
		}
		if !account.PoolID.Valid || account.Kind == "CARD" {
			return Result{}, fmt.Errorf("%w: transfer destination must be a non-card account assigned to a pool", ErrBadInput)
		}
		pool, poolErr := models.Pools.Query(sm.Where(psql.And(
			models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Pools.Columns.ID.EQ(psql.Arg(account.PoolID.V)),
		))).One(ctx, executor)
		if poolErr != nil {
			return Result{}, poolErr
		}
		if pool.Kind == "PERSONAL" {
			fundingRule, ruleErr := models.PoolFundingRules.Query(sm.Where(psql.And(
				models.PoolFundingRules.Columns.HouseholdID.EQ(psql.Arg(householdID)),
				models.PoolFundingRules.Columns.PoolID.EQ(psql.Arg(pool.ID)),
				models.PoolFundingRules.Columns.Version.EQ(psql.Arg(pool.FundingRuleVersion)),
			))).One(ctx, executor)
			if ruleErr != nil {
				return Result{}, ruleErr
			}
			if !fundingRule.OwnerMemberID.Valid || fundingRule.OwnerMemberID.V != toMemberID {
				return Result{}, fmt.Errorf("%w: personal destination account must belong to the receiving member", ErrBadInput)
			}
		}
		destinationAccountID = &id
	}
	if paymentAccountID != nil && destinationAccountID != nil && *paymentAccountID == *destinationAccountID {
		return Result{}, fmt.Errorf("%w: transfer source and destination accounts must differ", ErrBadInput)
	}
	var statementEvidence *statementTransferEvidence
	if statementProvided {
		statementEvidence, err = loadStatementTransferEvidence(ctx, executor, householdID, command.SourceDocumentID, command.SourceReference, command.Kind)
		if err != nil {
			return Result{}, err
		}
		statementAccountID, parseErr := parseID(command.SourceAccountID, "statement account")
		if parseErr != nil {
			return Result{}, parseErr
		}
		if err := validateStatementTransfer(statementEvidence.Entry, statementAccountID, paymentAccountID, destinationAccountID, command.AmountYen, command.OccurredAt); err != nil {
			return Result{}, err
		}
	}

	var loanID *gofrsuuid.UUID
	if command.Kind == repaymentKind {
		id, parseErr := parseID(*command.LoanID, "loan")
		if parseErr != nil {
			return Result{}, parseErr
		}
		loan, loanErr := models.Transfers.Query(sm.Where(psql.And(
			models.Transfers.Columns.ID.EQ(psql.Arg(id)),
			models.Transfers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Transfers.Columns.Kind.EQ(psql.Arg(loanKind)),
		)), sm.ForUpdate()).One(ctx, executor)
		if loanErr != nil {
			return Result{}, ErrNotFound
		}
		if fromMemberID != loan.ToMemberID || toMemberID != loan.FromMemberID {
			return Result{}, fmt.Errorf("%w: repayment direction does not match the loan", ErrBadInput)
		}
		repayments, repaymentErr := models.Transfers.Query(sm.Where(psql.And(
			models.Transfers.Columns.LoanID.EQ(psql.Arg(id)),
			models.Transfers.Columns.Kind.EQ(psql.Arg(repaymentKind)),
		))).All(ctx, executor)
		if repaymentErr != nil {
			return Result{}, repaymentErr
		}
		var repaid int64
		for _, repayment := range repayments {
			var ok bool
			repaid, ok = accounting.Add(repaid, repayment.AmountYen)
			if !ok {
				return Result{}, fmt.Errorf("%w: repayment total is too large", ErrBadInput)
			}
		}
		if command.AmountYen > loan.AmountYen-repaid {
			return Result{}, fmt.Errorf("%w: repayment exceeds the outstanding loan", ErrBadInput)
		}
		loanID = &id
	}

	transferID, journalID, ledgerID := newUUID(), newUUID(), newUUID()
	sourceKind := "TRANSFER"
	occurredAt := command.OccurredAt.UTC()
	if _, err := models.Journals.Insert(&models.JournalSetter{
		ID: &journalID, HouseholdID: &householdID, SourceKind: &sourceKind, SourceID: &transferID,
		IdempotencyKey: &command.IdempotencyKey, OccurredAt: &occurredAt, CreatedBy: &actorID,
	}).Exec(ctx, executor); err != nil {
		return Result{}, err
	}
	amount := command.AmountYen
	fromMember, toMember := sql.Null[gofrsuuid.UUID]{V: fromMemberID, Valid: true}, sql.Null[gofrsuuid.UUID]{V: toMemberID, Valid: true}
	journalLines, err := buildJournalLines(command.Kind, amount, fromMemberID, toMemberID, paymentPoolID, paymentAccountID, destinationAccountID)
	if err != nil {
		return Result{}, err
	}
	for _, entry := range journalLines {
		if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
			JournalID: &journalID, LineNo: &entry.LineNo,
			AccountID: nullableID(entry.AccountID), PoolID: nullableID(entry.PoolID), MemberID: nullableID(entry.MemberID),
			DebitYen: &entry.DebitYen, CreditYen: &entry.CreditYen, Memo: &entry.Memo,
		}).Exec(ctx, executor); err != nil {
			return Result{}, err
		}
	}

	description := strings.TrimSpace(command.Description)
	if description == "" {
		description = command.Kind
	}
	ledgerKind, transferKind := "TRANSFER", command.Kind
	if _, err := models.LedgerEntries.Insert(&models.LedgerEntrySetter{
		ID: &ledgerID, HouseholdID: &householdID, JournalID: &journalID, Kind: &ledgerKind,
		OccurredAt: &occurredAt, Description: &description, AmountYen: &amount, FromMemberID: &fromMember, ToMemberID: &toMember,
	}).Exec(ctx, executor); err != nil {
		return Result{}, err
	}
	loanValue, paymentAccountValue := sql.Null[gofrsuuid.UUID]{}, sql.Null[gofrsuuid.UUID]{}
	if loanID != nil {
		loanValue = sql.Null[gofrsuuid.UUID]{V: *loanID, Valid: true}
	}
	if paymentAccountID != nil {
		paymentAccountValue = sql.Null[gofrsuuid.UUID]{V: *paymentAccountID, Valid: true}
	}
	if _, err := models.Transfers.Insert(&models.TransferSetter{
		ID: &transferID, HouseholdID: &householdID, LedgerEntryID: &ledgerID,
		FromMemberID: &fromMemberID, ToMemberID: &toMemberID, Kind: &transferKind,
		AmountYen: &amount, Description: &description, LoanID: &loanValue, PaymentAccountID: &paymentAccountValue,
	}).Exec(ctx, executor); err != nil {
		return Result{}, err
	}
	if statementEvidence != nil {
		if err := recordStatementTransferConfirmation(ctx, executor, householdID, actorID, transferID, statementEvidence.DocumentID, command.SourceReference); err != nil {
			return Result{}, err
		}
	}
	for _, adjustment := range settlementAdjustments(command.Kind, paymentPoolKind, amount, fromMemberID, toMemberID) {
		adjustmentID := newUUID()
		if _, err := models.SettlementAdjustments.Insert(&models.SettlementAdjustmentSetter{
			ID: &adjustmentID, HouseholdID: &householdID, Period: &period, MemberID: &adjustment.memberID,
			SourceKind: &transferKind, SourceID: &transferID, AmountYen: &adjustment.amount,
		}).Exec(ctx, executor); err != nil {
			return Result{}, err
		}
	}
	resourceID := sql.Null[gofrsuuid.UUID]{V: ledgerID, Valid: true}
	fingerprint := sql.Null[[]byte]{V: command.RequestFingerprint[:], Valid: true}
	if _, err := models.IdempotencyKeys.Insert(&models.IdempotencyKeySetter{
		HouseholdID: &householdID, Key: &command.IdempotencyKey, Operation: &operation, ResourceID: &resourceID,
		RequestFingerprint: &fingerprint,
	}).Exec(ctx, executor); err != nil {
		return Result{}, err
	}
	if err := accounting.RecordEvent(ctx, executor, householdID, ledgerID, "transfer.recorded", 1, []string{"amountYen", "kind"}, actorID); err != nil {
		return Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	return Result{LedgerEntryID: ledgerID.String()}, nil
}

func validKind(kind string) bool {
	switch kind {
	case "ADVANCE", loanKind, repaymentKind, "GIFT":
		return true
	default:
		return false
	}
}

type journalLine struct {
	LineNo    int32
	AccountID *gofrsuuid.UUID
	PoolID    *gofrsuuid.UUID
	MemberID  *gofrsuuid.UUID
	DebitYen  int64
	CreditYen int64
	Memo      string
}

type settlementAdjustment struct {
	memberID gofrsuuid.UUID
	amount   int64
}

func validateCommandRules(command Command) (bool, error) {
	if command.AmountYen <= 0 || command.AmountYen > maxMoney || command.IdempotencyKey == "" || !validKind(command.Kind) {
		return false, fmt.Errorf("%w: invalid transfer", ErrBadInput)
	}
	if command.FromMemberID == command.ToMemberID {
		return false, fmt.Errorf("%w: transfer participants must differ", ErrBadInput)
	}
	if command.OccurredAt.IsZero() {
		return false, fmt.Errorf("%w: occurred at is required", ErrBadInput)
	}
	if command.Kind == repaymentKind && command.LoanID == nil {
		return false, fmt.Errorf("%w: repayment requires a loan", ErrBadInput)
	}
	if command.Kind != repaymentKind && command.LoanID != nil {
		return false, fmt.Errorf("%w: loan id is only valid for a repayment", ErrBadInput)
	}
	statementProvided := command.SourceDocumentID != "" || command.SourceReference != "" || command.SourceAccountID != ""
	if statementProvided && (command.SourceDocumentID == "" || command.SourceReference == "" || command.SourceAccountID == "" || command.Kind != loanKind && command.Kind != repaymentKind || len(command.SourceReference) > 256) {
		return false, fmt.Errorf("%w: invalid statement transfer source", ErrBadInput)
	}
	return statementProvided, nil
}

func buildJournalLines(kind string, amount int64, fromMemberID, toMemberID gofrsuuid.UUID, paymentPoolID, paymentAccountID, destinationAccountID *gofrsuuid.UUID) ([]journalLine, error) {
	lines := []journalLine{
		{LineNo: 1, MemberID: &toMemberID, DebitYen: amount, Memo: kind},
		{LineNo: 2, MemberID: &fromMemberID, CreditYen: amount, Memo: kind},
	}
	line := int32(3)
	if paymentAccountID != nil || destinationAccountID != nil {
		if paymentAccountID != nil && paymentPoolID == nil {
			return nil, fmt.Errorf("%w: invalid payment account", ErrBadInput)
		}
		if paymentAccountID != nil {
			lines = append(lines,
				journalLine{LineNo: line, PoolID: paymentPoolID, CreditYen: amount, Memo: "Transfer settlement"},
				journalLine{LineNo: line + 1, DebitYen: amount, Memo: "Transfer settlement offset"},
			)
			line += 2
			lines = append(lines,
				journalLine{LineNo: line, AccountID: paymentAccountID, CreditYen: amount, Memo: "Transfer source account"},
				journalLine{LineNo: line + 1, DebitYen: amount, Memo: "Transfer source account offset"},
			)
			line += 2
		}
		if destinationAccountID != nil {
			lines = append(lines,
				journalLine{LineNo: line, AccountID: destinationAccountID, DebitYen: amount, Memo: "Transfer destination account"},
				journalLine{LineNo: line + 1, CreditYen: amount, Memo: "Transfer destination account offset"},
			)
		}
	}
	return lines, nil
}

func settlementAdjustments(kind, paymentPoolKind string, amount int64, fromMemberID, toMemberID gofrsuuid.UUID) []settlementAdjustment {
	if kind != "ADVANCE" && kind != loanKind && kind != repaymentKind {
		return nil
	}
	if kind == repaymentKind && paymentPoolKind == "SHARED" {
		return []settlementAdjustment{{memberID: toMemberID, amount: amount}}
	}
	return []settlementAdjustment{{memberID: fromMemberID, amount: -amount}, {memberID: toMemberID, amount: amount}}
}

func nullableID(id *gofrsuuid.UUID) *sql.Null[gofrsuuid.UUID] {
	if id == nil {
		return new(sql.Null[gofrsuuid.UUID])
	}
	return &sql.Null[gofrsuuid.UUID]{V: *id, Valid: true}
}

func parseID(value, name string) (gofrsuuid.UUID, error) {
	id, err := gofrsuuid.FromString(value)
	if err != nil {
		return gofrsuuid.Nil, fmt.Errorf("%w: invalid %s id", ErrBadInput, name)
	}
	return id, nil
}

func newUUID() gofrsuuid.UUID {
	return gofrsuuid.Must(gofrsuuid.NewV7())
}

func requireHouseholdMember(ctx context.Context, executor bob.Executor, householdID, memberID gofrsuuid.UUID) error {
	exists, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberID)),
		models.HouseholdMembers.Columns.LeftAt.IsNull(),
	))).Exists(ctx, executor)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}
