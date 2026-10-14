package purchase

import (
	"context"
	"database/sql"
	stdjson "encoding/json"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/application/txcleanup"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type StatementRefundCorrection struct {
	CorrectionID   string
	AmountYen      int64
	OccurredAt     string
	Merchant       string
	SourceFilename string
}

type StatementRefundCorrectionsPage struct {
	Candidates  []StatementRefundCorrection
	HasNextPage bool
}

type LinkStatementRefundCorrectionCommand struct {
	ActorID          string
	HouseholdID      string
	CorrectionID     string
	SourceDocumentID string
	SourceReference  string
}

type statementRefundObservation struct {
	Statement ingestion.StatementEntry `json:"statement"`
}

func parseStatementRefundObservation(value types.JSON[stdjson.RawMessage]) (ingestion.StatementEntry, error) {
	var observation statementRefundObservation
	if err := json.Unmarshal(value.Val, &observation); err != nil {
		return ingestion.StatementEntry{}, err
	}
	if observation.Statement.Kind != "REFUND" || observation.Statement.Amount >= 0 || strings.TrimSpace(observation.Statement.Merchant) == "" {
		return ingestion.StatementEntry{}, fmt.Errorf("%w: correction evidence is not a refund statement", ErrBadInput)
	}
	if _, err := time.Parse(time.RFC3339, observation.Statement.OccurredAt); err != nil {
		return ingestion.StatementEntry{}, fmt.Errorf("%w: correction evidence has an invalid refund date", ErrBadInput)
	}
	return observation.Statement, nil
}

func sameRefundStatement(left, right ingestion.StatementEntry) bool {
	leftAt, leftErr := time.Parse(time.RFC3339, left.OccurredAt)
	rightAt, rightErr := time.Parse(time.RFC3339, right.OccurredAt)
	return leftErr == nil && rightErr == nil && left.Amount == right.Amount && leftAt.Equal(rightAt) && left.Merchant == right.Merchant
}

func statementRefundCorrection(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, link *models.CorrectionEvidenceLink, current ingestion.StatementEntry, currentDocumentID gofrsuuid.UUID) (StatementRefundCorrection, bool, error) {
	if link.SourceDocumentID == currentDocumentID {
		return StatementRefundCorrection{}, false, nil
	}
	original, err := parseStatementRefundObservation(link.Observation)
	if err != nil {
		return StatementRefundCorrection{}, false, nil
	}
	originalReference := original.SourceReference
	if originalReference == "" {
		originalReference = fmt.Sprintf("row:%d", original.Row)
	}
	if originalReference != link.SourceReference {
		return StatementRefundCorrection{}, false, nil
	}
	if !sameRefundStatement(original, current) {
		return StatementRefundCorrection{}, false, nil
	}
	sourceDocument, err := models.SourceDocuments.Query(sm.Where(psql.And(
		models.SourceDocuments.Columns.ID.EQ(psql.Arg(link.SourceDocumentID)),
		models.SourceDocuments.Columns.HouseholdID.EQ(psql.Arg(householdID)),
	))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return StatementRefundCorrection{}, false, nil
	}
	if err != nil {
		return StatementRefundCorrection{}, false, err
	}
	correction, err := models.Corrections.Query(sm.Where(psql.And(
		models.Corrections.Columns.ID.EQ(psql.Arg(link.CorrectionID)),
		models.Corrections.Columns.HouseholdID.EQ(psql.Arg(householdID)),
	))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return StatementRefundCorrection{}, false, ErrNotFound
	}
	if err != nil {
		return StatementRefundCorrection{}, false, err
	}
	if correction.AmountDeltaYen >= 0 || correction.AmountDeltaYen != original.Amount {
		return StatementRefundCorrection{}, false, nil
	}
	confirmation, err := models.DocumentConfirmations.Query(sm.Where(psql.And(
		models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(link.SourceDocumentID)),
		models.DocumentConfirmations.Columns.SourceReference.EQ(psql.Arg(link.SourceReference)),
		models.DocumentConfirmations.Columns.HouseholdID.EQ(psql.Arg(householdID)),
	))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (!confirmation.CorrectionID.Valid || confirmation.CorrectionID.V != correction.ID) {
		return StatementRefundCorrection{}, false, nil
	}
	if err != nil {
		return StatementRefundCorrection{}, false, err
	}
	return StatementRefundCorrection{
		CorrectionID: correction.ID.String(), AmountYen: correction.AmountDeltaYen,
		OccurredAt: original.OccurredAt, Merchant: original.Merchant, SourceFilename: sourceDocument.SourceFilename.V,
	}, true, nil
}

func (s *Service) StatementRefundCorrections(ctx context.Context, householdID, sourceDocumentID, sourceReference, after string, first int) (StatementRefundCorrectionsPage, error) {
	if first < 1 || first > 100 || sourceReference == "" || len(sourceReference) > 256 {
		return StatementRefundCorrectionsPage{}, fmt.Errorf("%w: page size must be between 1 and 100", ErrBadInput)
	}
	householdUUID, err := parseUUID(householdID, "household")
	if err != nil {
		return StatementRefundCorrectionsPage{}, err
	}
	sourceUUID, err := parseUUID(sourceDocumentID, "source document")
	if err != nil {
		return StatementRefundCorrectionsPage{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return StatementRefundCorrectionsPage{}, err
	}
	defer txcleanup.Rollback(ctx, tx)
	executor := bobpgx.NewTx(tx, nil)
	currentEvidence, err := loadStatementRefundEvidence(ctx, executor, householdUUID, sourceDocumentID, sourceReference)
	if err != nil {
		return StatementRefundCorrectionsPage{}, err
	}
	current, err := parseStatementRefundObservation(types.JSON[stdjson.RawMessage]{Val: currentEvidence.Observation})
	if err != nil {
		return StatementRefundCorrectionsPage{}, err
	}
	var afterUUID *gofrsuuid.UUID
	if after != "" {
		parsed, err := parseUUID(after, "refund correction cursor")
		if err != nil {
			return StatementRefundCorrectionsPage{}, ErrBadInput
		}
		afterUUID = &parsed
	}
	page := StatementRefundCorrectionsPage{Candidates: make([]StatementRefundCorrection, 0, first)}
	for {
		filters := []bob.Expression{models.CorrectionEvidenceLinks.Columns.HouseholdID.EQ(psql.Arg(householdUUID))}
		if afterUUID != nil {
			filters = append(filters, models.CorrectionEvidenceLinks.Columns.CorrectionID.LT(psql.Arg(*afterUUID)))
		}
		links, err := models.CorrectionEvidenceLinks.Query(
			sm.Where(psql.And(filters...)),
			sm.OrderBy(models.CorrectionEvidenceLinks.Columns.CorrectionID).Desc(),
			sm.Limit(101),
		).All(ctx, executor)
		if err != nil {
			return StatementRefundCorrectionsPage{}, err
		}
		if len(links) == 0 {
			break
		}
		for _, link := range links {
			afterUUID = &link.CorrectionID
			candidate, matches, err := statementRefundCorrection(ctx, executor, householdUUID, link, current, sourceUUID)
			if err != nil {
				return StatementRefundCorrectionsPage{}, err
			}
			if matches {
				page.Candidates = append(page.Candidates, candidate)
				if len(page.Candidates) > first {
					page.HasNextPage = true
					page.Candidates = page.Candidates[:first]
					return page, nil
				}
			}
		}
		if len(links) < 101 {
			break
		}
	}
	return page, nil
}

func (s *Service) LinkStatementRefundCorrection(ctx context.Context, input LinkStatementRefundCorrectionCommand) (string, error) {
	if input.SourceReference == "" || len(input.SourceReference) > 256 {
		return "", fmt.Errorf("%w: invalid statement reference", ErrBadInput)
	}
	householdUUID, err := parseUUID(input.HouseholdID, "household")
	if err != nil {
		return "", err
	}
	actorUUID, err := parseUUID(input.ActorID, "actor")
	if err != nil {
		return "", err
	}
	correctionUUID, err := parseUUID(input.CorrectionID, "correction")
	if err != nil {
		return "", err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer txcleanup.Rollback(ctx, tx)
	executor := bobpgx.NewTx(tx, nil)
	member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(actorUUID)),
	))).Exists(ctx, executor)
	if err != nil {
		return "", err
	}
	if !member {
		return "", ErrNotFound
	}
	currentEvidence, err := loadStatementRefundEvidence(ctx, executor, householdUUID, input.SourceDocumentID, input.SourceReference)
	if err != nil {
		return "", err
	}
	current, err := parseStatementRefundObservation(types.JSON[stdjson.RawMessage]{Val: currentEvidence.Observation})
	if err != nil {
		return "", err
	}
	correction, err := models.Corrections.Query(sm.Where(psql.And(
		models.Corrections.Columns.ID.EQ(psql.Arg(correctionUUID)),
		models.Corrections.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
	)), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	link, err := models.CorrectionEvidenceLinks.Query(sm.Where(psql.And(
		models.CorrectionEvidenceLinks.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.CorrectionEvidenceLinks.Columns.CorrectionID.EQ(psql.Arg(correctionUUID)),
	)), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if _, matches, err := statementRefundCorrection(ctx, executor, householdUUID, link, current, currentEvidence.DocumentID); err != nil {
		return "", err
	} else if !matches || correction.AmountDeltaYen >= 0 || correction.AmountDeltaYen != current.Amount {
		return "", fmt.Errorf("%w: refund does not match the existing correction", ErrBadInput)
	}
	correctionID := sql.Null[gofrsuuid.UUID]{V: correctionUUID, Valid: true}
	if _, err := models.DocumentConfirmations.Insert(&models.DocumentConfirmationSetter{
		SourceDocumentID: &currentEvidence.DocumentID, SourceReference: &input.SourceReference,
		HouseholdID: &householdUUID, CorrectionID: &correctionID, CreatedBy: &actorUUID,
	}).Exec(ctx, executor); err != nil {
		if isUniqueViolation(err) {
			return "", ErrConflict
		}
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return correction.PurchaseID.String(), nil
}
