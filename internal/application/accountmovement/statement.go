package accountmovement

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type statementTransferEvidence struct {
	DocumentID gofrsuuid.UUID
	Entry      ingestion.StatementEntry
}

func loadStatementTransferEvidence(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, documentID, reference string) (*statementTransferEvidence, error) {
	documentUUID, err := gofrsuuid.FromString(documentID)
	if err != nil {
		return nil, apperror.Wrap(apperror.KindNotFound, errors.New("invalid source document id"))
	}
	document, err := models.SourceDocuments.Query(sm.Where(models.SourceDocuments.Columns.ID.EQ(psql.Arg(documentUUID))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) || err == nil && document.HouseholdID != householdID {
		return nil, apperror.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if document.SourceKind != "bank" {
		return nil, apperror.Wrap(apperror.KindBadInput, errors.New("account transfers require a bank statement"))
	}
	_, err = models.DocumentConfirmations.Query(sm.Where(psql.And(
		models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(documentUUID)),
		models.DocumentConfirmations.Columns.SourceReference.EQ(psql.Arg(reference)),
	))).One(ctx, executor)
	if err == nil {
		return nil, apperror.Wrap(apperror.KindConflict, errors.New("statement row is already confirmed"))
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
		return nil, apperror.Wrap(apperror.KindBadInput, errors.New("statement extraction is unavailable"))
	}
	if err != nil {
		return nil, err
	}
	if !job.Result.Valid {
		return nil, apperror.Wrap(apperror.KindBadInput, errors.New("statement extraction is unavailable"))
	}
	var verification ingestion.Verification
	if err := json.Unmarshal(job.Result.V.Val, &verification); err != nil {
		return nil, err
	}
	index := slices.IndexFunc(verification.Statements, func(entry ingestion.StatementEntry) bool {
		return entry.SourceReference == reference || entry.SourceReference == "" && reference == fmt.Sprintf("row:%d", entry.Row)
	})
	if index < 0 {
		return nil, apperror.ErrNotFound
	}
	entry := verification.Statements[index]
	if entry.Kind != "ACCOUNT_TRANSFER" || entry.Amount == 0 || entry.Region == nil {
		return nil, apperror.Wrap(apperror.KindBadInput, errors.New("statement row is not an account transfer"))
	}
	return &statementTransferEvidence{DocumentID: documentUUID, Entry: entry}, nil
}

func validateStatementTransfer(entry ingestion.StatementEntry, statementAccountID, fromAccountID, toAccountID gofrsuuid.UUID, amount int64, occurredAt time.Time) error {
	if entry.Amount > maxMoney || entry.Amount < -maxMoney || entry.Amount == 0 {
		return apperror.Wrap(apperror.KindBadInput, errors.New("statement transfer amount is invalid"))
	}
	entryTime, err := time.Parse(time.RFC3339, entry.OccurredAt)
	if err != nil || !entryTime.Equal(occurredAt) || amount != absTransferAmount(entry.Amount) {
		return apperror.Wrap(apperror.KindBadInput, errors.New("statement transfer amount or date does not match"))
	}
	if entry.Amount < 0 && fromAccountID != statementAccountID || entry.Amount > 0 && toAccountID != statementAccountID {
		return apperror.Wrap(apperror.KindBadInput, errors.New("statement account does not match transfer direction"))
	}
	if fromAccountID == toAccountID || fromAccountID != statementAccountID && toAccountID != statementAccountID {
		return apperror.Wrap(apperror.KindBadInput, errors.New("statement account must be part of the transfer"))
	}
	return nil
}

func absTransferAmount(amount int64) int64 {
	if amount < 0 {
		return -amount
	}
	return amount
}

func requireStatementMovementConfirmation(ctx context.Context, executor bob.Executor, householdID, movementID gofrsuuid.UUID, documentID, reference string) error {
	documentUUID, err := gofrsuuid.FromString(documentID)
	if err != nil {
		return apperror.Wrap(apperror.KindNotFound, errors.New("invalid source document id"))
	}
	confirmation, err := models.DocumentConfirmations.Query(sm.Where(psql.And(
		models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(documentUUID)),
		models.DocumentConfirmations.Columns.SourceReference.EQ(psql.Arg(reference)),
		models.DocumentConfirmations.Columns.HouseholdID.EQ(psql.Arg(householdID)),
	))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (!confirmation.AccountMovementID.Valid || confirmation.AccountMovementID.V != movementID) {
		return apperror.Wrap(apperror.KindConflict, errors.New("statement movement confirmation does not match"))
	}
	return err
}
