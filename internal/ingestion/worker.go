package ingestion

import (
	"context"
	"log/slog"
	"time"

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
	ID                string
	HouseholdID       string
	SourceID          string
	SourceHash        string
	ObjectKey         string
	MIMEType          string
	AnalysisObjectKey string
	AnalysisMIMEType  string
	AnalysisTransform *[9]float64
	SourceKind        string
	Request           []byte
	Previous          []byte
	Attempt           int
	FailureCount      int
	Version           int64
}

// RunOneは期限切れの実行も引き継ぎ、attemptをフェンスとして遅延したWorkerの結果上書きを防ぐ。
func (w *Worker) RunOne(ctx context.Context, jobID string) (bool, error) {
	j, claimed, err := w.claim(ctx, jobID)
	if err != nil || !claimed {
		return claimed, err
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
		slog.Warn("Document extraction failed", "job_id", j.ID, "attempt", j.Attempt, "error", extractErr)
		return w.recordExtractionFailure(finishCtx, j, result, extractErr)
	}
	saved, err := w.recordExtractionSuccess(finishCtx, j, result)
	if err != nil {
		return true, err
	}
	if saved {
		span.SetAttributes(attribute.Bool("extraction.balanced", result.Balanced), attribute.Int("extraction.issue_count", len(result.Issues)))
	}
	return true, nil
}
