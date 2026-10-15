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
	loanKind        = "LOAN"
	repaymentKind   = "REPAYMENT"
	recordOperation = "recordTransfer"
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

	operation := recordOperation
	result, found, err := replayTransfer(ctx, executor, householdID, command, statementProvided)
	if err != nil {
		return Result{}, err
	}
	if found {
		if err := tx.Rollback(ctx); err != nil {
			return Result{}, err
		}
		return result, nil
	}
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Result{}, ErrNotFound
		}
		return Result{}, err
	}
	result, found, err = replayTransfer(ctx, executor, householdID, command, statementProvided)
	if err != nil {
		return Result{}, err
	}
	if found {
		if err := tx.Rollback(ctx); err != nil {
			return Result{}, err
		}
		return result, nil
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
	paymentAccount, err := loadTransferAccount(ctx, executor, householdID, command.PaymentAccountID, fromMemberID, paymentAccountRole)
	if err != nil {
		return Result{}, err
	}
	if paymentAccount != nil {
		paymentAccountID = &paymentAccount.id
		paymentPoolID = &paymentAccount.poolID
		paymentPoolKind = paymentAccount.poolKind
	}
	destinationAccount, err := loadTransferAccount(ctx, executor, householdID, command.DestinationAccountID, toMemberID, destinationAccountRole)
	if err != nil {
		return Result{}, err
	}
	if destinationAccount != nil {
		destinationAccountID = &destinationAccount.id
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
		id, err := validateRepayment(ctx, executor, householdID, fromMemberID, toMemberID, command)
		if err != nil {
			return Result{}, err
		}
		loanID = &id
	}

	transferID, journalID, ledgerID := newUUID(), newUUID(), newUUID()
	description := strings.TrimSpace(command.Description)
	if description == "" {
		description = command.Kind
	}
	if err := recordTransferAccounting(ctx, executor, transferPosting{
		command: command, householdID: householdID, actorID: actorID,
		fromMemberID: fromMemberID, toMemberID: toMemberID,
		paymentPoolID: paymentPoolID, paymentAccountID: paymentAccountID, destinationAccountID: destinationAccountID,
		loanID: loanID, transferID: transferID, journalID: journalID, ledgerID: ledgerID, description: description,
	}); err != nil {
		return Result{}, err
	}
	transferKind := command.Kind
	amount := command.AmountYen
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

func replayTransfer(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, command Command, statementProvided bool) (Result, bool, error) {
	existing, err := models.IdempotencyKeys.Query(sm.Where(psql.And(
		models.IdempotencyKeys.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.IdempotencyKeys.Columns.Key.EQ(psql.Arg(command.IdempotencyKey)),
		models.IdempotencyKeys.Columns.Operation.EQ(psql.Arg(recordOperation)),
	))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, err
	}
	if !existing.RequestFingerprint.Valid || !slices.Equal(existing.RequestFingerprint.V, command.RequestFingerprint[:]) {
		return Result{}, false, fmt.Errorf("%w: idempotency key was used with a different request", ErrConflict)
	}
	if !existing.ResourceID.Valid {
		return Result{}, false, fmt.Errorf("%w: idempotency record has no resource", ErrConflict)
	}
	if statementProvided {
		if err := requireStatementTransferConfirmation(ctx, executor, householdID, existing.ResourceID.V, command.SourceDocumentID, command.SourceReference); err != nil {
			return Result{}, false, err
		}
	}
	return Result{LedgerEntryID: existing.ResourceID.V.String()}, true, nil
}

func validateRepayment(ctx context.Context, executor bob.Executor, householdID, fromMemberID, toMemberID gofrsuuid.UUID, command Command) (gofrsuuid.UUID, error) {
	loanID, err := parseID(*command.LoanID, "loan")
	if err != nil {
		return gofrsuuid.Nil, err
	}
	loan, err := models.Transfers.Query(sm.Where(psql.And(
		models.Transfers.Columns.ID.EQ(psql.Arg(loanID)),
		models.Transfers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Transfers.Columns.Kind.EQ(psql.Arg(loanKind)),
	)), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return gofrsuuid.Nil, ErrNotFound
	}
	if fromMemberID != loan.ToMemberID || toMemberID != loan.FromMemberID {
		return gofrsuuid.Nil, fmt.Errorf("%w: repayment direction does not match the loan", ErrBadInput)
	}
	repayments, err := models.Transfers.Query(sm.Where(psql.And(
		models.Transfers.Columns.LoanID.EQ(psql.Arg(loanID)),
		models.Transfers.Columns.Kind.EQ(psql.Arg(repaymentKind)),
	))).All(ctx, executor)
	if err != nil {
		return gofrsuuid.Nil, err
	}
	var repaid int64
	for _, repayment := range repayments {
		var ok bool
		repaid, ok = accounting.Add(repaid, repayment.AmountYen)
		if !ok {
			return gofrsuuid.Nil, fmt.Errorf("%w: repayment total is too large", ErrBadInput)
		}
	}
	if err := validateRepaymentAmount(loan.AmountYen, repaid, command.AmountYen); err != nil {
		return gofrsuuid.Nil, err
	}
	return loanID, nil
}

func validateRepaymentAmount(loanAmount, repaid, repaymentAmount int64) error {
	if repaymentAmount > loanAmount-repaid {
		return fmt.Errorf("%w: repayment exceeds the outstanding loan", ErrBadInput)
	}
	return nil
}

type transferPosting struct {
	command              Command
	householdID          gofrsuuid.UUID
	actorID              gofrsuuid.UUID
	fromMemberID         gofrsuuid.UUID
	toMemberID           gofrsuuid.UUID
	paymentPoolID        *gofrsuuid.UUID
	paymentAccountID     *gofrsuuid.UUID
	destinationAccountID *gofrsuuid.UUID
	loanID               *gofrsuuid.UUID
	transferID           gofrsuuid.UUID
	journalID            gofrsuuid.UUID
	ledgerID             gofrsuuid.UUID
	description          string
}

func recordTransferAccounting(ctx context.Context, executor bob.Executor, posting transferPosting) error {
	command := posting.command
	occurredAt := command.OccurredAt.UTC()
	sourceKind, ledgerKind := "TRANSFER", "TRANSFER"
	journalLines, err := buildJournalLines(command.Kind, command.AmountYen, posting.fromMemberID, posting.toMemberID, posting.paymentPoolID, posting.paymentAccountID, posting.destinationAccountID)
	if err != nil {
		return err
	}
	if _, err := models.Journals.Insert(&models.JournalSetter{
		ID: &posting.journalID, HouseholdID: &posting.householdID, SourceKind: &sourceKind, SourceID: &posting.transferID,
		IdempotencyKey: &command.IdempotencyKey, OccurredAt: &occurredAt, CreatedBy: &posting.actorID,
	}).Exec(ctx, executor); err != nil {
		return err
	}
	for _, entry := range journalLines {
		if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
			JournalID: &posting.journalID, LineNo: &entry.LineNo,
			AccountID: nullableID(entry.AccountID), PoolID: nullableID(entry.PoolID), MemberID: nullableID(entry.MemberID),
			DebitYen: &entry.DebitYen, CreditYen: &entry.CreditYen, Memo: &entry.Memo,
		}).Exec(ctx, executor); err != nil {
			return err
		}
	}
	amount := command.AmountYen
	fromMember := sql.Null[gofrsuuid.UUID]{V: posting.fromMemberID, Valid: true}
	toMember := sql.Null[gofrsuuid.UUID]{V: posting.toMemberID, Valid: true}
	if _, err := models.LedgerEntries.Insert(&models.LedgerEntrySetter{
		ID: &posting.ledgerID, HouseholdID: &posting.householdID, JournalID: &posting.journalID, Kind: &ledgerKind,
		OccurredAt: &occurredAt, Description: &posting.description, AmountYen: &amount, FromMemberID: &fromMember, ToMemberID: &toMember,
	}).Exec(ctx, executor); err != nil {
		return err
	}
	loanValue, paymentAccountValue := sql.Null[gofrsuuid.UUID]{}, sql.Null[gofrsuuid.UUID]{}
	if posting.loanID != nil {
		loanValue = sql.Null[gofrsuuid.UUID]{V: *posting.loanID, Valid: true}
	}
	if posting.paymentAccountID != nil {
		paymentAccountValue = sql.Null[gofrsuuid.UUID]{V: *posting.paymentAccountID, Valid: true}
	}
	_, err = models.Transfers.Insert(&models.TransferSetter{
		ID: &posting.transferID, HouseholdID: &posting.householdID, LedgerEntryID: &posting.ledgerID,
		FromMemberID: &posting.fromMemberID, ToMemberID: &posting.toMemberID, Kind: &command.Kind,
		AmountYen: &amount, Description: &posting.description, LoanID: &loanValue, PaymentAccountID: &paymentAccountValue,
	}).Exec(ctx, executor)
	return err
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
