package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

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
	if os.Getenv("DATABASE_URL") == "" {
		return errors.New("DATABASE_URL is required")
	}
	freeAPIKey, paidAPIKey := os.Getenv("GEMINI_FREE_API_KEY"), os.Getenv("GEMINI_PAID_API_KEY")
	geminiEnabled, err := strconv.ParseBool(os.Getenv("GEMINI_ENABLED"))
	if err != nil && os.Getenv("GEMINI_ENABLED") != "" {
		return errors.New("GEMINI_ENABLED must be true or false")
	}
	if !geminiEnabled {
		slog.Info("Gemini extraction is disabled; source documents remain queued")
	} else if freeAPIKey == "" && paidAPIKey == "" {
		slog.Warn("Gemini is enabled but no API keys are configured; only deterministic statement imports are available")
	}
	usdJPY := 158.535
	rate := os.Getenv("USDJPY_RATE")
	if rate == "" {
		rate = os.Getenv("GEMINI_USDJPY_RATE")
	}
	if value := rate; value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil || parsed <= 0 {
			return errors.New("GEMINI_USDJPY_RATE must be a positive number")
		}
		usdJPY = parsed
	}
	rateSource := os.Getenv("USDJPY_RATE_SOURCE")
	if rateSource == "" {
		rateSource = "application_default"
		if rate != "" {
			rateSource = "operator_config"
		}
	}
	rateAsOf := os.Getenv("USDJPY_RATE_AS_OF")
	if rateAsOf != "" {
		if _, err := time.Parse("2006-01-02", rateAsOf); err != nil {
			return errors.New("USDJPY_RATE_AS_OF must use YYYY-MM-DD")
		}
	}
	db, err := postgres.NewPool(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return errors.New("initialize worker database failed")
	}
	defer db.Close()
	store, err := ingestion.NewObjectStore(os.Getenv("S3_ENDPOINT"), os.Getenv("S3_BUCKET"), os.Getenv("S3_ACCESS_KEY_ID"), os.Getenv("S3_SECRET_ACCESS_KEY"))
	if err != nil {
		return err
	}
	worker := ingestion.Worker{
		DB:            db,
		Store:         store,
		GeminiEnabled: geminiEnabled && (freeAPIKey != "" || paidAPIKey != ""),
		Router: ingestion.GeminiRouter{
			Free:             ingestion.Gemini{APIKey: freeAPIKey, Project: os.Getenv("GEMINI_FREE_PROJECT_ID")},
			Paid:             ingestion.Gemini{APIKey: paidAPIKey, Project: os.Getenv("GEMINI_PAID_PROJECT_ID")},
			FlashModel:       os.Getenv("GEMINI_FLASH_MODEL"),
			LiteModel:        os.Getenv("GEMINI_LITE_MODEL"),
			ProModel:         os.Getenv("GEMINI_PRO_MODEL"),
			USDJPYRate:       usdJPY,
			USDJPYRateSource: rateSource,
			USDJPYRateAsOf:   rateAsOf,
		},
		OCR: ingestion.OCR{Endpoint: os.Getenv("OCR_URL")},
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
		if queue == nil && os.Getenv("RABBITMQ_URL") != "" {
			queue, err = ingestion.NewQueue(os.Getenv("RABBITMQ_URL"))
			if err != nil {
				slog.Warn("Extraction queue unavailable; recovering jobs from database")
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
							slog.Error("Run notified extraction failed")
							if err := message.Nack(false, true); err != nil {
								slog.Warn("Requeue extraction notification failed")
							}
						} else if err := message.Ack(false); err != nil {
							slog.Warn("Acknowledge extraction notification failed")
						}
					case <-poll.C:
						drain(ctx, &worker)
					}
				}
				poll.Stop()
			} else {
				slog.Warn("Consume extraction queue failed")
			}
			if err := queue.Close(); err != nil {
				slog.Warn("Close unavailable extraction queue failed")
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
		slog.Warn("Recover exhausted extraction jobs failed")
	}
	for ctx.Err() == nil {
		processed, err := w.RunOne(ctx, "")
		if err != nil {
			slog.Error("Recover extraction job failed")
			return
		}
		if !processed {
			return
		}
	}
}
