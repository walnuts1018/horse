package ingestion

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

type Worker struct {
	DB            *pgxpool.Pool
	Store         *ObjectStore
	Gemini        Gemini
	FallbackModel string
	OCR           OCR
}

type job struct {
	ID          string
	HouseholdID string
	SourceID    string
	ObjectKey   string
	MIMEType    string
	SourceKind  string
	Request     []byte
	Attempt     int
	Version     int64
}

// RunOneは期限切れの実行も引き継ぎ、attemptをフェンスとして遅延したWorkerの結果上書きを防ぐ。
func (w *Worker) RunOne(ctx context.Context, jobID string) (bool, error) {
	claimCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var j job
	err := w.DB.QueryRow(claimCtx, `WITH next AS (
	 SELECT id FROM extraction_jobs WHERE ($1='' OR id::text=$1)
	 AND attempt<5 AND ((status='queued' AND available_at<=now()) OR (status='running' AND updated_at<now()-interval '5 minutes'))
	 ORDER BY available_at,id FOR UPDATE SKIP LOCKED LIMIT 1
	), claimed AS (
	 UPDATE extraction_jobs SET status='running',attempt=attempt+1,updated_at=now(),error_code=NULL
	 WHERE id IN (SELECT id FROM next) RETURNING *
	)
	SELECT j.id::text,j.household_id::text,j.source_document_id::text,d.object_key,d.mime_type,d.source_kind,j.request,j.attempt,j.version
	FROM claimed j JOIN source_documents d ON d.id=j.source_document_id AND d.household_id=j.household_id`, jobID).Scan(&j.ID, &j.HouseholdID, &j.SourceID, &j.ObjectKey, &j.MIMEType, &j.SourceKind, &j.Request, &j.Attempt, &j.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim extraction job: %w", err)
	}
	ctx, span := otel.Tracer("horse/ingestion").Start(ctx, "extract.document")
	defer span.End()
	span.SetAttributes(attribute.Int("extraction.attempt", j.Attempt), attribute.String("document.mime_type", j.MIMEType), attribute.String("document.source_kind", j.SourceKind))
	workCtx, workCancel := context.WithTimeout(ctx, 4*time.Minute)
	defer workCancel()
	result, extractErr := w.extract(workCtx, j)
	finishCtx, finishCancel := context.WithTimeout(ctx, 10*time.Second)
	defer finishCancel()
	if extractErr != nil {
		span.SetStatus(codes.Error, "extraction_failed")
		slog.Warn("Document extraction failed", "job_id", j.ID, "attempt", j.Attempt)
		status := "queued"
		if j.Attempt >= 5 {
			status = "failed"
		}
		_, err := w.DB.Exec(finishCtx, `UPDATE extraction_jobs SET status=$3,error_code='extraction_failed',available_at=now()+($4 * interval '1 second'),updated_at=now() WHERE id=$1 AND status='running' AND attempt=$2`, j.ID, j.Attempt, status, 5*(1<<j.Attempt))
		if err != nil {
			return true, fmt.Errorf("record extraction failure: %w", err)
		}
		return true, nil
	}
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
	receipt, err := w.Gemini.Extract(ctx, document, request.Hints)
	if err != nil {
		return Verification{}, err
	}
	verified, err := Verify(receipt)
	if err != nil {
		return Verification{}, err
	}
	runs := []ExtractionRun{{Receipt: receipt, Issues: verified.Issues}}
	if !verified.Balanced && w.FallbackModel != "" && w.FallbackModel != w.Gemini.Model {
		fallback := w.Gemini
		fallback.Model = w.FallbackModel
		alternative, err := fallback.Extract(ctx, document, request.Hints)
		if err == nil {
			second, err := Verify(alternative)
			if err == nil {
				runs = append(runs, ExtractionRun{Receipt: alternative, Issues: second.Issues})
				if len(second.Issues) < len(verified.Issues) {
					verified = second
				}
			}
		}
	}
	verified.Runs = runs
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
	return verified, nil
}

func (w *Worker) RecoverExhausted(ctx context.Context) error {
	_, err := w.DB.Exec(ctx, `UPDATE extraction_jobs SET status='failed',error_code='retry_exhausted',updated_at=now() WHERE status='running' AND attempt>=5 AND updated_at<now()-interval '5 minutes'`)
	return err
}
