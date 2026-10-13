package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/walnuts1018/horse/internal/config"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/observability"
	"github.com/walnuts1018/horse/internal/postgres"
)

type Runtime struct {
	DB        *pgxpool.Pool
	Telemetry *observability.Telemetry
	Store     *ingestion.ObjectStore
	Worker    ingestion.Worker
}

type telemetryResult struct {
	telemetry *observability.Telemetry
	err       error
}

type databaseResult struct {
	database *pgxpool.Pool
	err      error
}

type objectStoreResult struct {
	store *ingestion.ObjectStore
	err   error
}

func provideTelemetry(ctx context.Context) telemetryResult {
	telemetry, err := observability.New(ctx, "horse-worker")
	return telemetryResult{telemetry: telemetry, err: err}
}

func provideDatabase(ctx context.Context, settings config.Worker) databaseResult {
	database, err := postgres.NewPool(ctx, settings.DatabaseURL)
	return databaseResult{database: database, err: err}
}

func provideObjectStore(ctx context.Context, settings config.Worker) objectStoreResult {
	if err := ctx.Err(); err != nil {
		return objectStoreResult{err: err}
	}
	store, err := ingestion.NewObjectStore(ctx, settings.S3Endpoint, settings.S3Bucket, settings.S3AccessKeyID, settings.S3SecretAccessKey)
	return objectStoreResult{store: store, err: err}
}

func provideRuntime(settings config.Worker, telemetry telemetryResult, database databaseResult, store objectStoreResult) (*Runtime, error) {
	if err := errors.Join(telemetry.err, database.err, store.err); err != nil {
		closeRuntimeResources(telemetry.telemetry, database.database)
		return nil, err
	}
	worker := ingestion.Worker{
		DB:            database.database,
		Store:         store.store,
		GeminiEnabled: settings.GeminiEnabled && (settings.GeminiFreeAPIKey != "" || settings.GeminiPaidAPIKey != ""),
		Router: ingestion.GeminiRouter{
			Free:             ingestion.Gemini{APIKey: settings.GeminiFreeAPIKey, Project: settings.GeminiFreeProject},
			Paid:             ingestion.Gemini{APIKey: settings.GeminiPaidAPIKey, Project: settings.GeminiPaidProject},
			FlashModel:       settings.GeminiFlashModel,
			LiteModel:        settings.GeminiLiteModel,
			ProModel:         settings.GeminiProModel,
			USDJPYRate:       settings.EffectiveUSDJPYRate(),
			USDJPYRateSource: settings.EffectiveUSDJPYRateSource(),
			USDJPYRateAsOf:   settings.USDJPYRateAsOf,
		},
		OCR: ingestion.OCR{Endpoint: settings.OCRURL},
	}
	return &Runtime{DB: database.database, Telemetry: telemetry.telemetry, Store: store.store, Worker: worker}, nil
}

func (r *Runtime) Close() {
	closeRuntimeResources(r.Telemetry, r.DB)
}

func closeRuntimeResources(telemetry *observability.Telemetry, database *pgxpool.Pool) {
	if database != nil {
		database.Close()
	}
	if telemetry != nil {
		shutdownCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := telemetry.Shutdown(shutdownCtx); err != nil {
			slog.Warn("Shutdown worker telemetry failed")
		}
	}
}
