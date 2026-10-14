package ingestion

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (w *Worker) claim(ctx context.Context, jobID string) (job, bool, error) {
	claimCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := w.DB.Begin(claimCtx)
	if err != nil {
		return job{}, false, fmt.Errorf("begin extraction claim: %w", err)
	}
	executor := bobpgx.NewTx(tx, nil)
	defer func() {
		if err := tx.Rollback(claimCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.Error("Rollback extraction claim failed", "error", err)
		}
	}()
	var candidate *models.ExtractionJob
	var document *models.SourceDocument
	conditions := []bob.Expression{
		models.ExtractionJobs.Columns.FailureCount.LT(psql.Arg(5)),
		models.ExtractionJobs.Columns.Status.EQ(psql.Arg(extractionStatusQueued)),
		models.ExtractionJobs.Columns.AvailableAt.LTE(psql.Arg(time.Now())),
	}
	if jobID != "" {
		id, parseErr := gofrsuuid.FromString(jobID)
		if parseErr != nil {
			return job{}, false, fmt.Errorf("claim extraction job: %w", parseErr)
		}
		conditions = append(conditions, models.ExtractionJobs.Columns.ID.EQ(psql.Arg(id)))
	}
	if !w.GeminiEnabled {
		eligibleDocuments := psql.Select(
			sm.Columns(models.SourceDocuments.Columns.ID),
			sm.From(models.SourceDocuments.NameExpr()),
			sm.Where(psql.And(
				models.SourceDocuments.Columns.MimeType.EQ(psql.Arg(mimeTypeCSV)),
				models.SourceDocuments.Columns.SourceKind.In(psql.Arg(statementSourceCard), psql.Arg(statementSourceBank)),
			)),
		)
		conditions = append(conditions, models.ExtractionJobs.Columns.SourceDocumentID.In(eligibleDocuments))
	}
	candidate, err = models.ExtractionJobs.Query(
		sm.Where(psql.And(conditions...)),
		sm.OrderBy(models.ExtractionJobs.Columns.AvailableAt),
		sm.OrderBy(models.ExtractionJobs.Columns.ID),
		sm.ForUpdate().SkipLocked(), sm.Limit(1),
	).One(claimCtx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return job{}, false, nil
	}
	if err != nil {
		return job{}, false, fmt.Errorf("claim extraction job: %w", err)
	}
	document, err = models.SourceDocuments.Query(sm.Where(psql.And(
		models.SourceDocuments.Columns.ID.EQ(psql.Arg(candidate.SourceDocumentID)),
		models.SourceDocuments.Columns.HouseholdID.EQ(psql.Arg(candidate.HouseholdID)),
	))).One(claimCtx, executor)
	if err != nil {
		return job{}, false, fmt.Errorf("load extraction document: %w", err)
	}
	nextStatus := "running"
	nextAttempt := candidate.Attempt + 1
	updatedAt := time.Now().UTC()
	noErrorCode := sql.Null[string]{}
	if err := candidate.Update(claimCtx, executor, &models.ExtractionJobSetter{
		Status: &nextStatus, Attempt: &nextAttempt, UpdatedAt: &updatedAt, ErrorCode: &noErrorCode,
	}); err != nil {
		return job{}, false, fmt.Errorf("claim extraction job: %w", err)
	}
	if err := tx.Commit(claimCtx); err != nil {
		return job{}, false, fmt.Errorf("commit extraction claim: %w", err)
	}
	j := job{
		ID: candidate.ID.String(), HouseholdID: candidate.HouseholdID.String(), SourceID: candidate.SourceDocumentID.String(),
		ObjectKey: document.ObjectKey, MIMEType: document.MimeType, SourceKind: document.SourceKind,
		Request: candidate.Request.Val, Attempt: int(candidate.Attempt), FailureCount: int(candidate.FailureCount), Version: int64(candidate.Version),
	}
	if document.AnalysisObjectKey.Valid {
		j.AnalysisObjectKey = document.AnalysisObjectKey.V
	}
	if document.AnalysisMimeType.Valid {
		j.AnalysisMIMEType = document.AnalysisMimeType.V
	}
	if document.AnalysisTransform.Valid {
		var transform [9]float64
		if err := json.Unmarshal(document.AnalysisTransform.V.Val, &transform); err != nil {
			return job{}, false, fmt.Errorf("decode analysis coordinate transform: %w", err)
		}
		j.AnalysisTransform = &transform
	}
	if candidate.Result.Valid {
		j.Previous = candidate.Result.V.Val
	}
	return j, true, nil
}
