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

	"github.com/walnuts1018/horse/internal/api"
	bootstrapapi "github.com/walnuts1018/horse/internal/bootstrap/api"
	"github.com/walnuts1018/horse/internal/ingestion"
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
	app, err := bootstrapapi.NewAPI(ctx, logger)
	if err != nil {
		return err
	}
	defer app.Close()
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           otelhttp.NewHandler(api.New(app.Pool, app.Auth, logger, api.Config{PublicOrigin: app.Config.PublicOrigin, FrontendOrigin: app.Config.FrontendOrigin, Store: app.ObjectStore, OCR: ingestion.OCR{Endpoint: app.Config.OCREndpoint}, Queue: app.Queue}).Handler(), "horse-api"),
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
