package transfer

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/txcleanup"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type statementTransferEvidence struct {
	DocumentID gofrsuuid.UUID
	Entry      ingestion.StatementEntry
}

type LinkStatementCommand struct {
	ActorID          string
	HouseholdID      string
	LedgerEntryID    string
	SourceDocumentID string
	SourceReference  string
	SourceAccountID  string
}

func loadStatementTransferEvidence(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, documentID, reference, transferKind string) (*statementTransferEvidence, error) {
	documentUUID, err := gofrsuuid.FromString(documentID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid source document id", ErrNotFound)
	}
	document, err := models.SourceDocuments.Query(sm.Where(models.SourceDocuments.Columns.ID.EQ(psql.Arg(documentUUID))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) || err == nil && document.HouseholdID != householdID {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if document.SourceKind != "bank" {
		return nil, fmt.Errorf("%w: transfers require a bank statement", ErrBadInput)
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
	if errors.Is(err, sql.ErrNoRows) || err == nil && !job.Result.Valid {
		return nil, fmt.Errorf("%w: statement extraction is unavailable", ErrBadInput)
	}
	if err != nil {
		return nil, err
	}
	var verification ingestion.Verification
	if err := json.Unmarshal(job.Result.V.Val, &verification); err != nil {
		return nil, err
	}
	for _, entry := range verification.Statements {
		if entry.SourceReference == reference || entry.SourceReference == "" && reference == fmt.Sprintf("row:%d", entry.Row) {
			if (transferKind == "" && entry.Kind != loanKind && entry.Kind != repaymentKind) || transferKind != "" && entry.Kind != transferKind || entry.Amount == 0 {
				return nil, fmt.Errorf("%w: statement row is not a matching transfer", ErrBadInput)
			}
			return &statementTransferEvidence{DocumentID: documentUUID, Entry: entry}, nil
		}
	}
	return nil, ErrNotFound
}

func validateStatementTransfer(entry ingestion.StatementEntry, sourceAccountID gofrsuuid.UUID, paymentAccountID, destinationAccountID *gofrsuuid.UUID, amount int64, occurredAt time.Time) error {
	if entry.Amount > maxMoney || entry.Amount < -maxMoney || entry.Amount == 0 || amount != absStatementAmount(entry.Amount) {
		return fmt.Errorf("%w: statement amount does not match transfer", ErrBadInput)
	}
	entryTime, err := time.Parse(time.RFC3339, entry.OccurredAt)
	if err != nil || !entryTime.Equal(occurredAt) {
		return fmt.Errorf("%w: statement date does not match transfer", ErrBadInput)
	}
	var selectedAccountID *gofrsuuid.UUID
	if entry.Amount < 0 {
		selectedAccountID = paymentAccountID
	} else {
		selectedAccountID = destinationAccountID
	}
	if selectedAccountID == nil || *selectedAccountID != sourceAccountID {
		return fmt.Errorf("%w: statement account does not match transfer direction", ErrBadInput)
	}
	return nil
}

func requireStatementTransferConfirmation(ctx context.Context, executor bob.Executor, householdID, ledgerEntryID gofrsuuid.UUID, documentID, reference string) error {
	documentUUID, err := gofrsuuid.FromString(documentID)
	if err != nil {
		return fmt.Errorf("%w: invalid source document id", ErrNotFound)
	}
	transfer, err := models.Transfers.Query(sm.Where(psql.And(
		models.Transfers.Columns.LedgerEntryID.EQ(psql.Arg(ledgerEntryID)),
		models.Transfers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
	))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: idempotent transfer is unavailable", ErrConflict)
	}
	if err != nil {
		return err
	}
	confirmation, err := models.DocumentConfirmations.Query(sm.Where(psql.And(
		models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(documentUUID)),
		models.DocumentConfirmations.Columns.SourceReference.EQ(psql.Arg(reference)),
		models.DocumentConfirmations.Columns.HouseholdID.EQ(psql.Arg(householdID)),
	))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (!confirmation.TransferID.Valid || confirmation.TransferID.V != transfer.ID) {
		return fmt.Errorf("%w: statement transfer confirmation does not match", ErrConflict)
	}
	return err
}

func absStatementAmount(amount int64) int64 {
	if amount < 0 {
		return -amount
	}
	return amount
}

func recordStatementTransferConfirmation(ctx context.Context, executor bob.Executor, householdID, actorID, transferID, documentID gofrsuuid.UUID, reference string) error {
	transferValue := sql.Null[gofrsuuid.UUID]{V: transferID, Valid: true}
	_, err := models.DocumentConfirmations.Insert(&models.DocumentConfirmationSetter{
		SourceDocumentID: &documentID, SourceReference: &reference, HouseholdID: &householdID,
		TransferID: &transferValue, CreatedBy: &actorID,
	}).Exec(ctx, executor)
	return err
}

func statementTransferForLedger(ctx context.Context, executor bob.Executor, householdID, ledgerEntryID gofrsuuid.UUID) (*models.Transfer, error) {
	return models.Transfers.Query(sm.Where(psql.And(
		models.Transfers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Transfers.Columns.LedgerEntryID.EQ(psql.Arg(ledgerEntryID)),
	)), sm.ForUpdate()).One(ctx, executor)
}

func (w *Workflow) LinkStatement(ctx context.Context, command LinkStatementCommand) (Result, error) {
	if command.SourceDocumentID == "" || command.SourceReference == "" || command.SourceAccountID == "" || len(command.SourceReference) > 256 {
		return Result{}, fmt.Errorf("%w: invalid statement transfer source", ErrBadInput)
	}
	householdID, err := parseID(command.HouseholdID, "household")
	if err != nil {
		return Result{}, err
	}
	actorID, err := parseID(command.ActorID, "actor")
	if err != nil {
		return Result{}, err
	}
	ledgerEntryID, err := parseID(command.LedgerEntryID, "ledger entry")
	if err != nil {
		return Result{}, err
	}
	statementAccountID, err := parseID(command.SourceAccountID, "statement account")
	if err != nil {
		return Result{}, err
	}
	documentID, err := gofrsuuid.FromString(command.SourceDocumentID)
	if err != nil {
		return Result{}, fmt.Errorf("%w: invalid source document id", ErrNotFound)
	}

	tx, err := w.db.Begin(ctx)
	if err != nil {
		return Result{}, err
	}
	defer txcleanup.Rollback(ctx, tx)
	executor := bobpgx.NewTx(tx, nil)
	if err := requireHouseholdMember(ctx, executor, householdID, actorID); err != nil {
		return Result{}, err
	}
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return Result{}, ErrNotFound
	} else if err != nil {
		return Result{}, err
	}
	evidence, err := loadStatementTransferEvidence(ctx, executor, householdID, command.SourceDocumentID, command.SourceReference, "")
	if err != nil {
		return Result{}, err
	}
	if evidence.Entry.Amount == 0 || evidence.Entry.Amount > maxMoney || evidence.Entry.Amount < -maxMoney {
		return Result{}, fmt.Errorf("%w: statement amount is invalid", ErrBadInput)
	}
	account, err := models.Accounts.Query(sm.Where(psql.And(
		models.Accounts.Columns.ID.EQ(psql.Arg(statementAccountID)),
		models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID)),
	))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return Result{}, ErrNotFound
	}
	if err != nil {
		return Result{}, err
	}
	if account.Kind != "BANK" {
		return Result{}, fmt.Errorf("%w: statement account must be a bank account", ErrBadInput)
	}
	transfer, err := statementTransferForLedger(ctx, executor, householdID, ledgerEntryID)
	if errors.Is(err, sql.ErrNoRows) {
		return Result{}, ErrNotFound
	}
	if err != nil {
		return Result{}, err
	}
	if transfer.Kind != evidence.Entry.Kind || transfer.AmountYen != absStatementAmount(evidence.Entry.Amount) {
		return Result{}, fmt.Errorf("%w: selected transfer does not match statement kind or amount", ErrBadInput)
	}
	ledger, err := models.LedgerEntries.Query(sm.Where(psql.And(
		models.LedgerEntries.Columns.ID.EQ(psql.Arg(transfer.LedgerEntryID)),
		models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(householdID)),
	))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return Result{}, ErrNotFound
	}
	if err != nil {
		return Result{}, err
	}
	entryTime, err := time.Parse(time.RFC3339, evidence.Entry.OccurredAt)
	if err != nil || !entryTime.Equal(ledger.OccurredAt) {
		return Result{}, fmt.Errorf("%w: selected transfer does not match statement date", ErrBadInput)
	}
	journal, err := models.Journals.Query(sm.Where(psql.And(
		models.Journals.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Journals.Columns.SourceKind.EQ(psql.Arg("TRANSFER")),
		models.Journals.Columns.SourceID.EQ(psql.Arg(transfer.ID)),
	))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return Result{}, fmt.Errorf("%w: selected transfer has no journal", ErrConflict)
	}
	if err != nil {
		return Result{}, err
	}
	accountEntries, err := models.JournalEntries.Query(sm.Where(psql.And(
		models.JournalEntries.Columns.JournalID.EQ(psql.Arg(journal.ID)),
		models.JournalEntries.Columns.AccountID.EQ(psql.Arg(sql.Null[gofrsuuid.UUID]{V: statementAccountID, Valid: true})),
	))).All(ctx, executor)
	if err != nil {
		return Result{}, err
	}
	amount := transfer.AmountYen
	if len(accountEntries) != 1 || evidence.Entry.Amount < 0 && (accountEntries[0].CreditYen != amount || accountEntries[0].DebitYen != 0) || evidence.Entry.Amount > 0 && (accountEntries[0].DebitYen != amount || accountEntries[0].CreditYen != 0) {
		return Result{}, fmt.Errorf("%w: selected bank account or signed amount does not match transfer", ErrBadInput)
	}
	_, err = models.DocumentConfirmations.Query(sm.Where(psql.And(
		models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(documentID)),
		models.DocumentConfirmations.Columns.SourceReference.EQ(psql.Arg(command.SourceReference)),
	)), sm.ForUpdate()).One(ctx, executor)
	if err == nil {
		return Result{}, fmt.Errorf("%w: statement row is already confirmed", ErrConflict)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Result{}, err
	}
	if err := recordStatementTransferConfirmation(ctx, executor, householdID, actorID, transfer.ID, evidence.DocumentID, command.SourceReference); err != nil {
		return Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	return Result{LedgerEntryID: ledger.ID.String()}, nil
}
