package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"
)

func startHealthServer(db *pgxpool.Pool) (*http.Server, error) {
	e := echo.New()
	e.GET("/healthz", echo.WrapHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })))
	e.GET("/readyz", echo.WrapHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})))
	listener, err := new(net.ListenConfig).Listen(context.Background(), "tcp", ":8081") // #nosec G102 -- kubeletからPodのヘルスチェックを受け付けるため、全インターフェースで待ち受ける。
	if err != nil {
		return nil, errors.New("listen worker health server failed")
	}
	server := &http.Server{Handler: e, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Worker health server failed")
		}
	}()
	return server, nil
}
