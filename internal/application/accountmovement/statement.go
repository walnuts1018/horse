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
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/application/txcleanup"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type statementMovementEvidence struct {
	DocumentID gofrsuuid.UUID
	Entry      ingestion.StatementEntry
}

type statementIncomeEvidence struct {
	DocumentID gofrsuuid.UUID
	Entry      ingestion.StatementEntry
}

type LinkMovementCommand struct {
	ActorID           string
	HouseholdID       string
	AccountMovementID string
	SourceDocumentID  string
	SourceReference   string
	SourceAccountID   string
}

const externalIncomeStatementKind = "EXTERNAL_INCOME"

func loadStatementIncomeEvidence(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, documentID, reference string) (*statementIncomeEvidence, error) {
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
		return nil, apperror.Wrap(apperror.KindBadInput, errors.New("external income requires a bank statement"))
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
	if entry.Kind != externalIncomeStatementKind || entry.Amount <= 0 || entry.Region == nil {
		return nil, apperror.Wrap(apperror.KindBadInput, errors.New("statement row is not an external income"))
	}
	return &statementIncomeEvidence{DocumentID: documentUUID, Entry: entry}, nil
}

func validateStatementIncome(entry ingestion.StatementEntry, accountID gofrsuuid.UUID, amount int64, occurredAt time.Time) error {
	entryTime, err := time.Parse(time.RFC3339, entry.OccurredAt)
	if err != nil || amount <= 0 || amount != entry.Amount || !entryTime.Equal(occurredAt) {
		return apperror.Wrap(apperror.KindBadInput, errors.New("statement income amount or date does not match"))
	}
	if accountID == gofrsuuid.Nil {
		return apperror.Wrap(apperror.KindBadInput, errors.New("statement income account is invalid"))
	}
	return nil
}

func loadStatementMovementEvidence(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, documentID, reference, movementKind string) (*statementMovementEvidence, error) {
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
		return nil, apperror.Wrap(apperror.KindBadInput, errors.New("account movements require a bank statement"))
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
	if entry.Kind != "ACCOUNT_TRANSFER" && entry.Kind != cardBillPaymentKind || movementKind != "" && entry.Kind != statementMovementKind(movementKind) || entry.Amount == 0 || entry.Region == nil {
		return nil, apperror.Wrap(apperror.KindBadInput, errors.New("statement row does not match the account movement"))
	}
	return &statementMovementEvidence{DocumentID: documentUUID, Entry: entry}, nil
}

func validateStatementTransfer(entry ingestion.StatementEntry, statementAccountID, fromAccountID, toAccountID gofrsuuid.UUID, amount int64, occurredAt time.Time) error {
	return validateStatementMovement(entry, transferKind, statementAccountID, fromAccountID, toAccountID, amount, occurredAt)
}

func validateStatementMovement(entry ingestion.StatementEntry, movementKind string, statementAccountID, fromAccountID, toAccountID gofrsuuid.UUID, amount int64, occurredAt time.Time) error {
	if entry.Amount > maxMoney || entry.Amount < -maxMoney || entry.Amount == 0 {
		return apperror.Wrap(apperror.KindBadInput, errors.New("statement movement amount is invalid"))
	}
	entryTime, err := time.Parse(time.RFC3339, entry.OccurredAt)
	if err != nil || !entryTime.Equal(occurredAt) || amount != absTransferAmount(entry.Amount) {
		return apperror.Wrap(apperror.KindBadInput, errors.New("statement movement amount or date does not match"))
	}
	switch movementKind {
	case transferKind:
		if entry.Amount < 0 && fromAccountID != statementAccountID || entry.Amount > 0 && toAccountID != statementAccountID {
			return apperror.Wrap(apperror.KindBadInput, errors.New("statement account does not match transfer direction"))
		}
		if fromAccountID != statementAccountID && toAccountID != statementAccountID {
			return apperror.Wrap(apperror.KindBadInput, errors.New("statement account must be part of the transfer"))
		}
	case cardBillPaymentKind:
		if entry.Kind != cardBillPaymentKind || entry.Amount >= 0 || statementAccountID != fromAccountID {
			return apperror.Wrap(apperror.KindBadInput, errors.New("statement account does not match card bill payment"))
		}
	default:
		return apperror.Wrap(apperror.KindBadInput, errors.New("unsupported statement account movement"))
	}
	if fromAccountID == toAccountID {
		return apperror.Wrap(apperror.KindBadInput, errors.New("statement movement accounts must differ"))
	}
	return nil
}

func statementMovementKind(movementKind string) string {
	switch movementKind {
	case transferKind:
		return "ACCOUNT_TRANSFER"
	case cardBillPaymentKind:
		return "CARD_BILL_PAYMENT"
	default:
		return ""
	}
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

func requireStatementIncomeConfirmation(ctx context.Context, executor bob.Executor, householdID, incomeID gofrsuuid.UUID, documentID, reference string) error {
	documentUUID, err := gofrsuuid.FromString(documentID)
	if err != nil {
		return apperror.Wrap(apperror.KindNotFound, errors.New("invalid source document id"))
	}
	confirmation, err := models.DocumentConfirmations.Query(sm.Where(psql.And(
		models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(documentUUID)),
		models.DocumentConfirmations.Columns.SourceReference.EQ(psql.Arg(reference)),
		models.DocumentConfirmations.Columns.HouseholdID.EQ(psql.Arg(householdID)),
	))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (!confirmation.AccountIncomeID.Valid || confirmation.AccountIncomeID.V != incomeID) {
		return apperror.Wrap(apperror.KindConflict, errors.New("statement income confirmation does not match"))
	}
	return err
}

func (s *Service) LinkStatementMovement(ctx context.Context, command LinkMovementCommand) (MovementResult, error) {
	if command.SourceDocumentID == "" || command.SourceReference == "" || command.SourceAccountID == "" || len(command.SourceReference) > 256 {
		return MovementResult{}, apperror.Wrap(apperror.KindBadInput, errors.New("invalid statement movement source"))
	}
	householdID, err := parseID(command.HouseholdID, "household")
	if err != nil {
		return MovementResult{}, err
	}
	actorID, err := parseID(command.ActorID, "actor")
	if err != nil {
		return MovementResult{}, err
	}
	movementID, err := parseID(command.AccountMovementID, "account movement")
	if err != nil {
		return MovementResult{}, err
	}
	statementAccountID, err := parseID(command.SourceAccountID, "statement account")
	if err != nil {
		return MovementResult{}, err
	}
	if _, err := gofrsuuid.FromString(command.SourceDocumentID); err != nil {
		return MovementResult{}, apperror.Wrap(apperror.KindNotFound, errors.New("invalid source document id"))
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return MovementResult{}, err
	}
	defer txcleanup.Rollback(ctx, tx)
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return MovementResult{}, apperror.ErrNotFound
	} else if err != nil {
		return MovementResult{}, err
	}
	evidence, err := loadStatementMovementEvidence(ctx, executor, householdID, command.SourceDocumentID, command.SourceReference, "")
	if err != nil {
		return MovementResult{}, err
	}
	account, err := loadAccount(ctx, executor, householdID, statementAccountID)
	if err != nil {
		return MovementResult{}, err
	}
	if account.Kind != bankAccountKind {
		return MovementResult{}, apperror.Wrap(apperror.KindBadInput, errors.New("statement account must be a bank account"))
	}
	movement, err := models.AccountMovements.Query(sm.Where(psql.And(
		models.AccountMovements.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.AccountMovements.Columns.ID.EQ(psql.Arg(movementID)),
	)), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return MovementResult{}, apperror.ErrNotFound
	}
	if err != nil {
		return MovementResult{}, err
	}
	if statementMovementKind(movement.Kind) != evidence.Entry.Kind || movement.AmountYen != absTransferAmount(evidence.Entry.Amount) {
		return MovementResult{}, apperror.Wrap(apperror.KindBadInput, errors.New("selected movement does not match statement kind or amount"))
	}
	if err := validateStatementMovement(evidence.Entry, movement.Kind, statementAccountID, movement.FromAccountID, movement.ToAccountID, movement.AmountYen, movement.OccurredAt); err != nil {
		return MovementResult{}, err
	}
	if _, err := models.DocumentConfirmations.Insert(&models.DocumentConfirmationSetter{
		SourceDocumentID: &evidence.DocumentID, SourceReference: &command.SourceReference, HouseholdID: &householdID,
		AccountMovementID: &sql.Null[gofrsuuid.UUID]{V: movement.ID, Valid: true}, CreatedBy: &actorID,
	}).Exec(ctx, executor); err != nil {
		return MovementResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MovementResult{}, err
	}
	return movementResult(movement), nil
}
