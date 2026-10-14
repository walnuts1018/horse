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
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type statementTransferEvidence struct {
	DocumentID gofrsuuid.UUID
	Entry      ingestion.StatementEntry
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
			if entry.Kind != transferKind || entry.Amount == 0 {
				return nil, fmt.Errorf("%w: statement row is not a matching loan transfer", ErrBadInput)
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
	transfer, err := statementTransferForLedger(ctx, executor, householdID, ledgerEntryID)
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
	))).One(ctx, executor)
}
