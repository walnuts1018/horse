package ingestion

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

// RunOneは期限切れの実行も引き継ぎ、attemptをフェンスとして遅延したWorkerの結果上書きを防ぐ。
func (w *Worker) RunOne(ctx context.Context, jobID string) (bool, error) {
	claimCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var j job
	err := w.DB.QueryRow(claimCtx, `WITH next AS (
	 SELECT id FROM extraction_jobs WHERE ($1='' OR id::text=$1)
	 AND ($2 OR EXISTS (SELECT 1 FROM source_documents enabled_doc WHERE enabled_doc.id=extraction_jobs.source_document_id AND enabled_doc.household_id=extraction_jobs.household_id AND enabled_doc.mime_type='text/csv' AND enabled_doc.source_kind IN ('card','bank')))
	 AND failure_count<5 AND status='queued' AND available_at<=now()
	 ORDER BY available_at,id FOR UPDATE SKIP LOCKED LIMIT 1
	), claimed AS (
	 UPDATE extraction_jobs SET status='running',attempt=attempt+1,updated_at=now(),error_code=NULL
	 WHERE id IN (SELECT id FROM next) RETURNING *
	)
	SELECT j.id::text,j.household_id::text,j.source_document_id::text,d.object_key,d.mime_type,d.source_kind,j.request,j.result,j.attempt,j.failure_count,j.version
	FROM claimed j JOIN source_documents d ON d.id=j.source_document_id AND d.household_id=j.household_id`, jobID, w.GeminiEnabled).Scan(&j.ID, &j.HouseholdID, &j.SourceID, &j.ObjectKey, &j.MIMEType, &j.SourceKind, &j.Request, &j.Previous, &j.Attempt, &j.FailureCount, &j.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim extraction job: %w", err)
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
		if len(result.Runs) > 0 {
			_, err = w.DB.Exec(finishCtx, `UPDATE extraction_jobs SET status=$3,error_code=$4,result=jsonb_set(COALESCE(result,'{}'::jsonb),'{runs}',COALESCE(result->'runs','[]'::jsonb) || $5::jsonb,true),failure_count=$6,available_at=now()+($7 * interval '1 second'),updated_at=now() WHERE id=$1 AND status='running' AND attempt=$2`, j.ID, j.Attempt, status, workerErrorCode(extractErr), string(payload), failureCount, retryDelay(extractErr, j.Attempt).Seconds())
		} else {
			_, err = w.DB.Exec(finishCtx, `UPDATE extraction_jobs SET status=$3,error_code=$4,failure_count=$5,available_at=now()+($6 * interval '1 second'),updated_at=now() WHERE id=$1 AND status='running' AND attempt=$2`, j.ID, j.Attempt, status, workerErrorCode(extractErr), failureCount, retryDelay(extractErr, j.Attempt).Seconds())
		}
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
	tx, err := w.DB.Begin(finishCtx)
	if err != nil {
		return true, fmt.Errorf("begin extraction completion: %w", err)
	}
	defer func() {
		if err := tx.Rollback(finishCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.Error("Rollback extraction completion failed")
		}
	}()
	updated, err := tx.Exec(finishCtx, `UPDATE extraction_jobs SET status='succeeded',result=$3::jsonb,error_code=NULL,updated_at=now() WHERE id=$1 AND status='running' AND attempt=$2`, j.ID, j.Attempt, string(payload))
	if err != nil {
		return true, fmt.Errorf("save extraction result: %w", err)
	}
	if updated.RowsAffected() == 0 {
		return true, nil
	}
	event, err := json.Marshal(map[string]any{"jobId": j.ID, "sourceDocumentId": j.SourceID, "status": "succeeded", "balanced": result.Balanced})
	if err != nil {
		return true, fmt.Errorf("encode extraction event: %w", err)
	}
	if _, err := tx.Exec(finishCtx, `INSERT INTO household_events(household_id,type,entity_id,revision,changed_fields,payload) VALUES($1,'receipt.extraction.updated',$2,$3,'["status","result"]'::jsonb,$4::jsonb)`, j.HouseholdID, j.SourceID, j.Version, string(event)); err != nil {
		return true, fmt.Errorf("record extraction event: %w", err)
	}
	if err := tx.Commit(finishCtx); err != nil {
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
	}
	if err := json.Unmarshal(j.Request, &request); err != nil {
		return Verification{}, fmt.Errorf("decode extraction request: %w", err)
	}
	receipt, runs, err := w.Router.Generate(ctx, j.SourceKind, document, request.Hints, GeminiRouteOptions{Reason: "initial_extraction"})
	if err != nil {
		return Verification{Runs: runs}, err
	}
	verified, err := Verify(receipt)
	if err != nil {
		return Verification{Receipt: receipt, Runs: runs}, &GeminiAPIError{Kind: GeminiFailureBadResponse, Code: "receipt_validation_failed"}
	}
	verified.Runs = annotateRuns(runs, receipt, verified.Issues)
	if len(document.MIMEType) >= 6 && document.MIMEType[:6] == "image/" {
		lines, ocrErr := w.OCR.Extract(ctx, document)
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
	_, err := w.DB.Exec(ctx, `UPDATE extraction_jobs SET failure_count=failure_count+1,status=CASE WHEN failure_count+1>=5 THEN 'failed' ELSE 'queued' END,error_code=CASE WHEN failure_count+1>=5 THEN 'retry_exhausted' ELSE 'worker_interrupted' END,available_at=CASE WHEN failure_count+1>=5 THEN available_at ELSE now() END,updated_at=now() WHERE status='running' AND updated_at<now()-interval '35 minutes'`)
	return err
}

func supportsProReview(mimeType string) bool {
	return strings.HasPrefix(mimeType, "image/") || mimeType == "application/pdf"
}
