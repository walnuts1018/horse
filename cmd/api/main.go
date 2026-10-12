package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/walnuts1018/horse/internal/api"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/observability"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("API server stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	telemetry, err := observability.New(ctx, "horse-api")
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := telemetry.Shutdown(shutdownCtx); err != nil {
			logger.Warn("Shutting down telemetry failed", "error", err)
		}
	}()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return err
	}
	publicOrigin := os.Getenv("PUBLIC_ORIGIN")
	authService, err := auth.New(ctx, pool, auth.Config{
		IssuerURL: os.Getenv("OIDC_ISSUER_URL"), ClientID: os.Getenv("OIDC_CLIENT_ID"),
		ClientSecret: os.Getenv("OIDC_CLIENT_SECRET"), RedirectURL: os.Getenv("OIDC_REDIRECT_URL"),
		PublicOrigin: publicOrigin, FrontendOrigin: os.Getenv("FRONTEND_ORIGIN"),
	})
	if err != nil {
		return err
	}
	var objectStore *ingestion.ObjectStore
	if os.Getenv("S3_ENDPOINT") != "" || os.Getenv("S3_BUCKET") != "" || os.Getenv("S3_ACCESS_KEY_ID") != "" || os.Getenv("S3_SECRET_ACCESS_KEY") != "" {
		objectStore, err = ingestion.NewObjectStore(os.Getenv("S3_ENDPOINT"), os.Getenv("S3_BUCKET"), os.Getenv("S3_ACCESS_KEY_ID"), os.Getenv("S3_SECRET_ACCESS_KEY"))
		if err != nil {
			return err
		}
	}
	var queue *ingestion.Queue
	if endpoint := os.Getenv("RABBITMQ_URL"); endpoint != "" {
		queue, err = ingestion.NewQueue(endpoint)
		if err != nil {
			logger.Warn("Extraction queue unavailable; database jobs will be recovered by the worker", "error", err)
		} else {
			defer func() {
				if err := queue.Close(); err != nil {
					logger.Warn("Closing extraction queue failed", "error", err)
				}
			}()
		}
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           otelhttp.NewHandler(api.New(pool, authService, logger, api.Config{PublicOrigin: publicOrigin, FrontendOrigin: os.Getenv("FRONTEND_ORIGIN"), Store: objectStore, OCR: ingestion.OCR{Endpoint: os.Getenv("OCR_URL")}, Queue: queue}).Handler(), "horse-api"),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       60 * time.Second,
	}
	serverErr := make(chan error, 1)
	go func() { serverErr <- httpServer.ListenAndServe() }()
	logger.Info("API server listening", "address", httpServer.Addr)
	select {
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	}
}
