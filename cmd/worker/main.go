package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/walnuts1018/horse/internal/config"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/observability"
	"github.com/walnuts1018/horse/internal/postgres"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("Worker stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	settings, err := config.LoadWorker()
	if err != nil {
		return err
	}
	telemetry, err := observability.New(ctx, "horse-worker")
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := telemetry.Shutdown(shutdownCtx); err != nil {
			slog.Warn("Shutdown worker telemetry failed")
		}
	}()
	if !settings.GeminiEnabled {
		slog.Info("Gemini extraction is disabled; source documents remain queued")
	}
	db, err := postgres.NewPool(ctx, settings.DatabaseURL)
	if err != nil {
		return errors.New("initialize worker database failed")
	}
	defer db.Close()
	store, err := ingestion.NewObjectStore(settings.S3Endpoint, settings.S3Bucket, settings.S3AccessKeyID, settings.S3SecretAccessKey)
	if err != nil {
		return err
	}
	worker := ingestion.Worker{
		DB:            db,
		Store:         store,
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
	health, err := startHealthServer(db)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := health.Shutdown(shutdownCtx); err != nil {
			slog.Warn("Shutdown worker health server failed")
		}
	}()
	var queue *ingestion.Queue
	defer func() {
		if queue != nil {
			if err := queue.Close(); err != nil {
				slog.Warn("Close extraction queue failed")
			}
		}
	}()
	for ctx.Err() == nil {
		if queue == nil && settings.RabbitMQURL != "" {
			queue, err = ingestion.NewQueue(settings.RabbitMQURL)
			if err != nil {
				slog.Warn("Extraction queue unavailable; recovering jobs from database", "error", err)
			}
		}
		if queue != nil {
			deliveries, consumeErr := queue.Consume()
			if consumeErr == nil {
				slog.Info("Extraction worker ready")
				poll := time.NewTicker(10 * time.Second)
				connected := true
				for connected && ctx.Err() == nil {
					select {
					case <-ctx.Done():
					case message, ok := <-deliveries:
						if !ok {
							connected = false
							break
						}
						if _, err := worker.RunOne(ctx, string(message.Body)); err != nil {
							slog.Error("Run notified extraction failed", "error", err)
							if err := message.Nack(false, true); err != nil {
								slog.Warn("Requeue extraction notification failed", "error", err)
							}
						} else if err := message.Ack(false); err != nil {
							slog.Warn("Acknowledge extraction notification failed", "error", err)
						}
					case <-poll.C:
						drain(ctx, &worker)
					}
				}
				poll.Stop()
			} else {
				slog.Warn("Consume extraction queue failed", "error", consumeErr)
			}
			if err := queue.Close(); err != nil {
				slog.Warn("Close unavailable extraction queue failed", "error", err)
			}
			queue = nil
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
			}
		} else {
			drain(ctx, &worker)
			select {
			case <-ctx.Done():
			case <-time.After(10 * time.Second):
			}
		}
	}
	return nil
}

func drain(ctx context.Context, w *ingestion.Worker) {
	if err := w.RecoverExhausted(ctx); err != nil {
		slog.Warn("Recover exhausted extraction jobs failed", "error", err)
	}
	for ctx.Err() == nil {
		processed, err := w.RunOne(ctx, "")
		if err != nil {
			slog.Error("Recover extraction job failed", "error", err)
			return
		}
		if !processed {
			return
		}
	}
}
