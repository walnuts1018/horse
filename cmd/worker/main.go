package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/observability"
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
	if os.Getenv("GEMINI_API_KEY") == "" || os.Getenv("GEMINI_MODEL") == "" {
		slog.Warn("Gemini is not configured; only deterministic statement imports are available")
	}
	db, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return errors.New("initialize worker database failed")
	}
	defer db.Close()
	store, err := ingestion.NewObjectStore(os.Getenv("S3_ENDPOINT"), os.Getenv("S3_BUCKET"), os.Getenv("S3_ACCESS_KEY_ID"), os.Getenv("S3_SECRET_ACCESS_KEY"))
	if err != nil {
		return err
	}
	worker := ingestion.Worker{DB: db, Store: store, Gemini: ingestion.Gemini{APIKey: os.Getenv("GEMINI_API_KEY"), Model: os.Getenv("GEMINI_MODEL")}, FallbackModel: os.Getenv("GEMINI_FALLBACK_MODEL"), OCR: ingestion.OCR{Endpoint: os.Getenv("OCR_URL")}}
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
