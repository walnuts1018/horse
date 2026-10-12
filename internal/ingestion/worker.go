package ingestion

import (
	"context"
	"database/sql"
	stdjson "encoding/json"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

type Worker struct {
	DB            *pgxpool.Pool
	Store         DocumentStore
	Router        GeminiRouter
	OCR           OCR
	GeminiEnabled bool
}

type DocumentStore interface {
	Get(context.Context, string, string) (Document, error)
}

type job struct {
	ID           string
	HouseholdID  string
	SourceID     string
	ObjectKey    string
	MIMEType     string
	SourceKind   string
	Request      []byte
	Previous     []byte
	Attempt      int
	FailureCount int
	Version      int64
}

func postgresInt32(value int, field string) (int32, error) {
	if value < math.MinInt32 || value > math.MaxInt32 {
		return 0, fmt.Errorf("%s exceeds PostgreSQL integer range", field)
	}
	return int32(value), nil
}

// RunOneは期限切れの実行も引き継ぎ、attemptをフェンスとして遅延したWorkerの結果上書きを防ぐ。
func (w *Worker) RunOne(ctx context.Context, jobID string) (bool, error) {
	claimCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := w.DB.Begin(claimCtx)
	if err != nil {
		return false, fmt.Errorf("begin extraction claim: %w", err)
	}
	executor := bobpgx.NewTx(tx, nil)
	defer func() {
		if err := tx.Rollback(claimCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.Error("Rollback extraction claim failed")
		}
	}()
	var candidate *models.ExtractionJob
	var document *models.SourceDocument
	skipped := make([]bob.Expression, 0)
	for {
		conditions := []bob.Expression{
			models.ExtractionJobs.Columns.FailureCount.LT(psql.Arg(5)),
			models.ExtractionJobs.Columns.Status.EQ(psql.Arg("queued")),
			models.ExtractionJobs.Columns.AvailableAt.LTE(psql.Arg(time.Now())),
		}
		if jobID != "" {
			id, parseErr := gofrsuuid.FromString(jobID)
			if parseErr != nil {
				return false, fmt.Errorf("claim extraction job: %w", parseErr)
			}
			conditions = append(conditions, models.ExtractionJobs.Columns.ID.EQ(psql.Arg(id)))
		}
		if len(skipped) > 0 {
			conditions = append(conditions, models.ExtractionJobs.Columns.ID.NotIn(skipped...))
		}
		candidate, err = models.ExtractionJobs.Query(
			sm.Where(psql.And(conditions...)),
			sm.OrderBy(models.ExtractionJobs.Columns.AvailableAt),
			sm.OrderBy(models.ExtractionJobs.Columns.ID),
			sm.ForUpdate().SkipLocked(), sm.Limit(1),
		).One(claimCtx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("claim extraction job: %w", err)
		}
		document, err = models.SourceDocuments.Query(sm.Where(psql.And(
			models.SourceDocuments.Columns.ID.EQ(psql.Arg(candidate.SourceDocumentID)),
			models.SourceDocuments.Columns.HouseholdID.EQ(psql.Arg(candidate.HouseholdID)),
		))).One(claimCtx, executor)
		if err != nil {
			return false, fmt.Errorf("load extraction document: %w", err)
		}
		if w.GeminiEnabled || document.MimeType == "text/csv" && (document.SourceKind == "card" || document.SourceKind == "bank") {
			break
		}
		skipped = append(skipped, psql.Arg(candidate.ID))
	}
	nextStatus := "running"
	nextAttempt := candidate.Attempt + 1
	updatedAt := time.Now().UTC()
	noErrorCode := sql.Null[string]{}
	if err := candidate.Update(claimCtx, executor, &models.ExtractionJobSetter{
		Status: &nextStatus, Attempt: &nextAttempt, UpdatedAt: &updatedAt, ErrorCode: &noErrorCode,
	}); err != nil {
		return false, fmt.Errorf("claim extraction job: %w", err)
	}
	if err := tx.Commit(claimCtx); err != nil {
		return false, fmt.Errorf("commit extraction claim: %w", err)
	}
	j := job{
		ID: candidate.ID.String(), HouseholdID: candidate.HouseholdID.String(), SourceID: candidate.SourceDocumentID.String(),
		ObjectKey: document.ObjectKey, MIMEType: document.MimeType, SourceKind: document.SourceKind,
		Request: candidate.Request.Val, Attempt: int(candidate.Attempt), FailureCount: int(candidate.FailureCount), Version: int64(candidate.Version),
	}
	if candidate.Result.Valid {
		j.Previous = candidate.Result.V.Val
	}
	ctx, span := otel.Tracer("horse/ingestion").Start(ctx, "extract.document")
	defer span.End()
	span.SetAttributes(attribute.Int("extraction.attempt", j.Attempt), attribute.String("document.mime_type", j.MIMEType), attribute.String("document.source_kind", j.SourceKind))
	workCtx, workCancel := context.WithTimeout(ctx, 32*time.Minute)
	defer workCancel()
	result, extractErr := w.extract(workCtx, j)
	finishCtx, finishCancel := context.WithTimeout(ctx, 10*time.Second)
	defer finishCancel()
	if extractErr != nil {
		span.SetStatus(codes.Error, "extraction_failed")
		slog.Warn("Document extraction failed", "job_id", j.ID, "attempt", j.Attempt)
		spendCap := errors.Is(extractErr, ErrGeminiSpendCap)
		failureCount := j.FailureCount
		if !spendCap {
			failureCount++
		}
		status := "failed"
		if spendCap || retryWorkerError(extractErr) && failureCount < 5 {
			status = "queued"
		}
		payload, marshalErr := json.Marshal(result.Runs)
		if marshalErr != nil {
			return true, fmt.Errorf("encode failed extraction runs: %w", marshalErr)
		}
		jobUUID, parseErr := gofrsuuid.FromString(j.ID)
		if parseErr != nil {
			return true, fmt.Errorf("record extraction failure: %w", parseErr)
		}
		attempt, err := postgresInt32(j.Attempt, "extraction attempt")
		if err != nil {
			return true, err
		}
		failureCountValue, err := postgresInt32(failureCount, "extraction failure count")
		if err != nil {
			return true, err
		}
		updatedStatus := status
		code := sql.Null[string]{V: workerErrorCode(extractErr), Valid: true}
		updatedAt := time.Now().UTC()
		availableAt := updatedAt.Add(retryDelay(extractErr, j.Attempt))
		setter := models.ExtractionJobSetter{
			Status: &updatedStatus, ErrorCode: &code, FailureCount: &failureCountValue,
			AvailableAt: &availableAt, UpdatedAt: &updatedAt,
		}
		if len(result.Runs) > 0 {
			previous := make(map[string]stdjson.RawMessage)
			if len(j.Previous) > 0 {
				if err := stdjson.Unmarshal(j.Previous, &previous); err != nil {
					previous = make(map[string]stdjson.RawMessage)
				}
			}
			var existingRuns []stdjson.RawMessage
			_ = stdjson.Unmarshal(previous["runs"], &existingRuns)
			var newRuns []stdjson.RawMessage
			if err := stdjson.Unmarshal(payload, &newRuns); err != nil {
				return true, fmt.Errorf("decode failed extraction runs: %w", err)
			}
			existingRuns = append(existingRuns, newRuns...)
			encodedRuns, err := stdjson.Marshal(existingRuns)
			if err != nil {
				return true, fmt.Errorf("encode failed extraction runs: %w", err)
			}
			previous["runs"] = encodedRuns
			encodedResult, err := stdjson.Marshal(previous)
			if err != nil {
				return true, fmt.Errorf("encode failed extraction result: %w", err)
			}
			resultValue := sql.Null[types.JSON[stdjson.RawMessage]]{
				V: types.JSON[stdjson.RawMessage]{Val: encodedResult}, Valid: true,
			}
			setter.Result = &resultValue
		}
		_, err = models.ExtractionJobs.Update(setter.UpdateMod(), um.Where(psql.And(
			models.ExtractionJobs.Columns.ID.EQ(psql.Arg(jobUUID)),
			models.ExtractionJobs.Columns.Status.EQ(psql.Arg("running")),
			models.ExtractionJobs.Columns.Attempt.EQ(psql.Arg(attempt)),
		))).Exec(finishCtx, bobpgx.NewPool(w.DB))
		if err != nil {
			return true, fmt.Errorf("record extraction failure: %w", err)
		}
		return true, nil
	}
	result.Runs = appendPreviousRuns(result.Runs, j.Previous)
	payload, err := json.Marshal(result)
	if err != nil {
		return true, fmt.Errorf("encode extraction result: %w", err)
	}
	finishTx, err := w.DB.Begin(finishCtx)
	if err != nil {
		return true, fmt.Errorf("begin extraction completion: %w", err)
	}
	defer func() {
		if err := finishTx.Rollback(finishCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.Error("Rollback extraction completion failed")
		}
	}()
	jobUUID, err := gofrsuuid.FromString(j.ID)
	if err != nil {
		return true, fmt.Errorf("save extraction result: %w", err)
	}
	attempt, err := postgresInt32(j.Attempt, "extraction attempt")
	if err != nil {
		return true, err
	}
	status := "succeeded"
	completionAt := time.Now().UTC()
	completionErrorCode := sql.Null[string]{}
	resultValue := sql.Null[types.JSON[stdjson.RawMessage]]{
		V: types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(payload)}, Valid: true,
	}
	updated, err := models.ExtractionJobs.Update((&models.ExtractionJobSetter{
		Status: &status, Result: &resultValue, ErrorCode: &completionErrorCode, UpdatedAt: &completionAt,
	}).UpdateMod(), um.Where(psql.And(
		models.ExtractionJobs.Columns.ID.EQ(psql.Arg(jobUUID)),
		models.ExtractionJobs.Columns.Status.EQ(psql.Arg("running")),
		models.ExtractionJobs.Columns.Attempt.EQ(psql.Arg(attempt)),
	))).Exec(finishCtx, bobpgx.NewTx(finishTx, nil))
	if err != nil {
		return true, fmt.Errorf("save extraction result: %w", err)
	}
	if updated == 0 {
		return true, nil
	}
	event, err := json.Marshal(map[string]any{"jobId": j.ID, "sourceDocumentId": j.SourceID, "status": "succeeded", "balanced": result.Balanced})
	if err != nil {
		return true, fmt.Errorf("encode extraction event: %w", err)
	}
	householdUUID, err := gofrsuuid.FromString(j.HouseholdID)
	if err != nil {
		return true, fmt.Errorf("record extraction event: %w", err)
	}
	sourceUUID, err := gofrsuuid.FromString(j.SourceID)
	if err != nil {
		return true, fmt.Errorf("record extraction event: %w", err)
	}
	eventType := "receipt.extraction.updated"
	jVersion := int64(j.Version)
	changedFields := types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(`["status","result"]`)}
	eventPayload := types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(event)}
	if _, err := models.HouseholdEvents.Insert(&models.HouseholdEventSetter{
		HouseholdID: &householdUUID, Type: &eventType, EntityID: &sourceUUID, Revision: &jVersion,
		ChangedFields: &changedFields, Payload: &eventPayload,
	}).Exec(finishCtx, bobpgx.NewTx(finishTx, nil)); err != nil {
		return true, fmt.Errorf("record extraction event: %w", err)
	}
	if err := finishTx.Commit(finishCtx); err != nil {
		return true, fmt.Errorf("commit extraction result: %w", err)
	}
	span.SetAttributes(attribute.Bool("extraction.balanced", result.Balanced), attribute.Int("extraction.issue_count", len(result.Issues)))
	return true, nil
}

func (w *Worker) extract(ctx context.Context, j job) (Verification, error) {
	document, err := w.Store.Get(ctx, j.ObjectKey, j.MIMEType)
	if err != nil {
		return Verification{}, err
	}
	if document.MIMEType == "text/csv" && (j.SourceKind == "card" || j.SourceKind == "bank") {
		entries, err := ParseStatementCSV(document.Data, j.SourceKind)
		if err != nil {
			return Verification{}, err
		}
		return Verification{Statements: entries, Balanced: true}, nil
	}
	var request struct {
		Hints []HistoryHint `json:"hints"`
		Crop  *Region       `json:"crop,omitempty"`
	}
	if err := json.Unmarshal(j.Request, &request); err != nil {
		return Verification{}, fmt.Errorf("decode extraction request: %w", err)
	}
	transform := identityTransform()
	if request.Crop != nil {
		document, transform, err = CropImage(document, *request.Crop)
		if err != nil {
			return Verification{}, fmt.Errorf("prepare cropped document: %w", err)
		}
	}
	receipt, runs, err := w.Router.Generate(ctx, j.SourceKind, document, request.Hints, GeminiRouteOptions{Reason: "initial_extraction"})
	if err != nil {
		return Verification{Runs: runs}, err
	}
	verified, err := Verify(receipt)
	if err != nil {
		return Verification{Receipt: receipt, Runs: runs}, &GeminiAPIError{Kind: GeminiFailureBadResponse, Code: "receipt_validation_failed"}
	}
	var lines []OCRLine
	var ocrErr error
	if len(document.MIMEType) >= 6 && document.MIMEType[:6] == "image/" {
		lines, ocrErr = w.OCR.Extract(ctx, document)
		if ocrErr == nil {
			if err := transformVerification(&verified, lines, transform); err != nil {
				return Verification{}, fmt.Errorf("map evidence regions to original image: %w", err)
			}
		} else if err := transformReceiptRegions(&verified, transform); err != nil {
			return Verification{}, fmt.Errorf("map evidence regions to original image: %w", err)
		}
	} else {
		verified.Transform = transform
	}
	verified.Runs = annotateRuns(runs, receipt, verified.Issues)
	if len(document.MIMEType) >= 6 && document.MIMEType[:6] == "image/" {
		if ocrErr != nil {
			verified.Issues = append(verified.Issues, "独立したOCR読取を取得できませんでした。原本を確認してください。")
		} else {
			verified.OCRLines = lines
			verified.Issues = append(verified.Issues, CompareOCR(verified.Receipt, lines)...)
		}
		verified.Balanced = len(verified.Issues) == 0
	}
	if !verified.Balanced && len(verified.Runs) > 0 {
		guidance := evidenceGuidance(verified.Issues, verified.OCRLines)
		lastRun := latestSuccessfulRun(verified.Runs)
		if lastRun != nil {
			alternative, qualityRuns, qualityErr := w.Router.Generate(ctx, j.SourceKind, document, request.Hints, w.Router.QualityOptions(j.SourceKind, *lastRun, guidance))
			verified.Runs = append(verified.Runs, qualityRuns...)
			if qualityErr == nil {
				second, verifyErr := Verify(alternative)
				if verifyErr == nil {
					if err := transformReceiptRegions(&second, transform); err != nil {
						return Verification{}, fmt.Errorf("map reviewed evidence regions to original image: %w", err)
					}
					second.Runs = annotateRuns(qualityRuns, alternative, second.Issues)
					second.OCRLines = verified.OCRLines
					if len(second.OCRLines) > 0 {
						second.Issues = append(second.Issues, CompareOCR(second.Receipt, second.OCRLines)...)
						second.Balanced = len(second.Issues) == 0
					}
					if betterVerification(second, verified) {
						verified.Receipt, verified.Issues, verified.ComputedTotal, verified.Balanced, verified.Candidates = second.Receipt, second.Issues, second.ComputedTotal, second.Balanced, second.Candidates
					}
				}
			}
		}
		if !verified.Balanced && supportsProReview(document.MIMEType) && needsProReview(verified.Issues) && w.Router.Paid.APIKey != "" {
			proReceipt, proRuns, proErr := w.Router.Generate(ctx, j.SourceKind, document, request.Hints, w.Router.ProOptions(evidenceGuidance(verified.Issues, verified.OCRLines)))
			verified.Runs = append(verified.Runs, proRuns...)
			if proErr == nil {
				proVerification, verifyErr := Verify(proReceipt)
				if verifyErr == nil {
					if err := transformReceiptRegions(&proVerification, transform); err != nil {
						return Verification{}, fmt.Errorf("map reviewed evidence regions to original image: %w", err)
					}
					proVerification.Runs = annotateRuns(proRuns, proReceipt, proVerification.Issues)
					proVerification.OCRLines = verified.OCRLines
					if len(proVerification.OCRLines) > 0 {
						proVerification.Issues = append(proVerification.Issues, CompareOCR(proVerification.Receipt, proVerification.OCRLines)...)
						proVerification.Balanced = len(proVerification.Issues) == 0
					}
					if betterVerification(proVerification, verified) {
						verified.Receipt, verified.Issues, verified.ComputedTotal, verified.Balanced, verified.Candidates = proVerification.Receipt, proVerification.Issues, proVerification.ComputedTotal, proVerification.Balanced, proVerification.Candidates
					}
				}
			}
		}
	}
	return verified, nil
}

func appendPreviousRuns(runs []ExtractionRun, previous []byte) []ExtractionRun {
	if len(previous) == 0 {
		return runs
	}
	var prior Verification
	if err := json.Unmarshal(previous, &prior); err != nil {
		return runs
	}
	return append(prior.Runs, runs...)
}

func annotateRuns(runs []ExtractionRun, receipt Receipt, issues []string) []ExtractionRun {
	for i := len(runs) - 1; i >= 0; i-- {
		if runs[i].Status == "succeeded" && runs[i].Model == receipt.Model && runs[i].Receipt != nil {
			runs[i].Issues = append([]string(nil), issues...)
			break
		}
	}
	return runs
}

func latestSuccessfulRun(runs []ExtractionRun) *ExtractionRun {
	for i := len(runs) - 1; i >= 0; i-- {
		if runs[i].Status == "succeeded" && runs[i].Receipt != nil {
			return &runs[i]
		}
	}
	return nil
}

func betterVerification(candidate, current Verification) bool {
	if candidate.Balanced != current.Balanced {
		return candidate.Balanced
	}
	return len(candidate.Issues) < len(current.Issues)
}

func (w *Worker) RecoverExhausted(ctx context.Context) error {
	cutoff := time.Now().Add(-35 * time.Minute)
	tx, err := w.DB.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin stale extraction recovery: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.Error("Rollback stale extraction recovery failed")
		}
	}()
	executor := bobpgx.NewTx(tx, nil)
	staleJobs, err := models.ExtractionJobs.Query(sm.Where(psql.And(
		models.ExtractionJobs.Columns.Status.EQ(psql.Arg("running")),
		models.ExtractionJobs.Columns.UpdatedAt.LT(psql.Arg(cutoff)),
	)), sm.ForUpdate()).All(ctx, executor)
	if err != nil {
		return fmt.Errorf("load stale extraction jobs: %w", err)
	}
	now := time.Now().UTC()
	for _, staleJob := range staleJobs {
		failureCount := staleJob.FailureCount + 1
		status, errorCode := "queued", "worker_interrupted"
		availableAt := now
		if failureCount >= 5 {
			status, errorCode, availableAt = "failed", "retry_exhausted", staleJob.AvailableAt
		}
		failureCode := sql.Null[string]{V: errorCode, Valid: true}
		if err := staleJob.Update(ctx, executor, &models.ExtractionJobSetter{
			FailureCount: &failureCount, Status: &status, ErrorCode: &failureCode,
			AvailableAt: &availableAt, UpdatedAt: &now,
		}); err != nil {
			return fmt.Errorf("recover stale extraction job: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit stale extraction recovery: %w", err)
	}
	return nil
}

func supportsProReview(mimeType string) bool {
	return strings.HasPrefix(mimeType, "image/") || mimeType == "application/pdf"
}
