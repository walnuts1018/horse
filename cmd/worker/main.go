package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/walnuts1018/horse/internal/bootstrap/worker"
	"github.com/walnuts1018/horse/internal/config"
	"github.com/walnuts1018/horse/internal/ingestion"
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
	runtime, err := worker.InitializeWorker(ctx, settings)
	if err != nil {
		return err
	}
	defer runtime.Close()
	if !settings.GeminiEnabled {
		slog.Info("Gemini extraction is disabled; source documents remain queued")
	}
	health, err := startHealthServer(runtime.DB)
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
						if _, err := runtime.Worker.RunOne(ctx, string(message.Body)); err != nil {
							slog.Error("Run notified extraction failed", "error", err)
							if err := message.Nack(false, true); err != nil {
								slog.Warn("Requeue extraction notification failed", "error", err)
							}
						} else if err := message.Ack(false); err != nil {
							slog.Warn("Acknowledge extraction notification failed", "error", err)
						}
					case <-poll.C:
						drain(ctx, &runtime.Worker)
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
			drain(ctx, &runtime.Worker)
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
