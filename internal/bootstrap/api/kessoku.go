package api

import (
	"cmp"
	"context"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mazrean/kessoku"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/observability"
	"github.com/walnuts1018/horse/internal/postgres"
)

//go:generate go tool kessoku $GOFILE

type Config struct {
	DatabaseURL       string
	PublicOrigin      string
	FrontendOrigin    string
	OIDCIssuerURL     string
	OIDCClientID      string
	OIDCClientSecret  string
	OIDCRedirectURL   string
	S3Endpoint        string
	S3Bucket          string
	S3AccessKeyID     string
	S3SecretAccessKey string
	RabbitMQURL       string
	OCREndpoint       string
}

type App struct {
	Config      Config
	Telemetry   *observability.Telemetry
	Pool        *pgxpool.Pool
	Auth        *auth.Service
	ObjectStore *ingestion.ObjectStore
	Queue       *ingestion.Queue
	cleanup     *cleanupRegistry
}

type cleanupRegistry struct {
	mu       sync.Mutex
	once     sync.Once
	closed   bool
	cleanups []registeredCleanup
}

type registeredCleanup struct {
	priority uint8
	close    func()
}

func (r *cleanupRegistry) Add(priority uint8, cleanup func()) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		cleanup()
		return
	}
	r.cleanups = append(r.cleanups, registeredCleanup{priority: priority, close: cleanup})
	r.mu.Unlock()
}

func (r *cleanupRegistry) Close() {
	r.once.Do(func() {
		r.mu.Lock()
		r.closed = true
		cleanups := slices.Clone(r.cleanups)
		r.cleanups = nil
		r.mu.Unlock()
		slices.SortFunc(cleanups, func(a, b registeredCleanup) int { return cmp.Compare(a.priority, b.priority) })
		for _, cleanup := range cleanups {
			cleanup.close()
		}
	})
}

func NewAPI(ctx context.Context, logger *slog.Logger) (*App, error) {
	cleanup := new(cleanupRegistry)
	app, err := InitializeAPI(ctx, cleanup, logger)
	if err != nil {
		cleanup.Close()
		return nil, err
	}
	return app, nil
}

func loadConfig() Config {
	return Config{
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		PublicOrigin:      os.Getenv("PUBLIC_ORIGIN"),
		FrontendOrigin:    os.Getenv("FRONTEND_ORIGIN"),
		OIDCIssuerURL:     os.Getenv("OIDC_ISSUER_URL"),
		OIDCClientID:      os.Getenv("OIDC_CLIENT_ID"),
		OIDCClientSecret:  os.Getenv("OIDC_CLIENT_SECRET"),
		OIDCRedirectURL:   os.Getenv("OIDC_REDIRECT_URL"),
		S3Endpoint:        os.Getenv("S3_ENDPOINT"),
		S3Bucket:          os.Getenv("S3_BUCKET"),
		S3AccessKeyID:     os.Getenv("S3_ACCESS_KEY_ID"),
		S3SecretAccessKey: os.Getenv("S3_SECRET_ACCESS_KEY"),
		RabbitMQURL:       os.Getenv("RABBITMQ_URL"),
		OCREndpoint:       os.Getenv("OCR_URL"),
	}
}

func newTelemetry(ctx context.Context, logger *slog.Logger, cleanup *cleanupRegistry) (*observability.Telemetry, error) {
	telemetry, err := observability.New(ctx, "horse-api")
	if err != nil {
		return nil, err
	}
	cleanup.Add(2, func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := telemetry.Shutdown(shutdownCtx); err != nil {
			logger.Warn("Shutting down telemetry failed", "error", err)
		}
	})
	return telemetry, nil
}

func newPool(ctx context.Context, cfg Config, logger *slog.Logger, cleanup *cleanupRegistry) (*pgxpool.Pool, error) {
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("Initializing database pool failed", "error", err)
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		logger.Error("Checking database connection failed", "error", err)
		return nil, err
	}
	cleanup.Add(1, pool.Close)
	return pool, nil
}

func newAuth(ctx context.Context, pool *pgxpool.Pool, cfg Config, logger *slog.Logger) (*auth.Service, error) {
	service, err := auth.New(ctx, pool, auth.Config{
		IssuerURL: cfg.OIDCIssuerURL, ClientID: cfg.OIDCClientID,
		ClientSecret: cfg.OIDCClientSecret, RedirectURL: cfg.OIDCRedirectURL,
		PublicOrigin: cfg.PublicOrigin, FrontendOrigin: cfg.FrontendOrigin,
	})
	if err != nil {
		logger.Error("Initializing authentication failed", "error", err)
	}
	return service, err
}

func newObjectStore(cfg Config, logger *slog.Logger) (*ingestion.ObjectStore, error) {
	if cfg.S3Endpoint == "" && cfg.S3Bucket == "" && cfg.S3AccessKeyID == "" && cfg.S3SecretAccessKey == "" {
		return nil, nil
	}
	store, err := ingestion.NewObjectStore(cfg.S3Endpoint, cfg.S3Bucket, cfg.S3AccessKeyID, cfg.S3SecretAccessKey)
	if err != nil {
		logger.Error("Initializing object store failed", "error", err)
	}
	return store, err
}

func newQueue(cfg Config, logger *slog.Logger, cleanup *cleanupRegistry) (*ingestion.Queue, error) {
	if cfg.RabbitMQURL == "" {
		return nil, nil
	}
	queue, err := ingestion.NewQueue(cfg.RabbitMQURL)
	if err != nil {
		logger.Warn("Extraction queue unavailable; database jobs will be recovered by the worker", "error", err)
		return nil, nil
	}
	cleanup.Add(0, func() {
		if err := queue.Close(); err != nil {
			logger.Warn("Closing extraction queue failed", "error", err)
		}
	})
	return queue, nil
}

func newApp(cfg Config, telemetry *observability.Telemetry, pool *pgxpool.Pool, authService *auth.Service, objectStore *ingestion.ObjectStore, queue *ingestion.Queue, cleanup *cleanupRegistry) *App {
	return &App{
		Config: cfg, Telemetry: telemetry, Pool: pool, Auth: authService,
		ObjectStore: objectStore, Queue: queue, cleanup: cleanup,
	}
}

func (app *App) Close() { app.cleanup.Close() }

var _ = kessoku.Inject[*App](
	"InitializeAPI",
	kessoku.Provide(loadConfig),
	kessoku.Async(kessoku.Provide(newTelemetry)),
	kessoku.Async(kessoku.Provide(newPool)),
	kessoku.Async(kessoku.Provide(newObjectStore)),
	kessoku.Async(kessoku.Provide(newQueue)),
	kessoku.Provide(newAuth),
	kessoku.Provide(newApp),
)
