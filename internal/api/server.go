package api

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type Config struct {
	PublicOrigin   string
	FrontendOrigin string
	Store          DocumentStore
	OCR            ingestion.OCR
	Queue          *ingestion.Queue
}

type DocumentStore interface {
	Put(context.Context, string, string, []byte) (string, string, error)
	Get(context.Context, string, string) (ingestion.Document, error)
}

type Server struct {
	db     *pgxpool.Pool
	auth   *auth.Service
	logger *slog.Logger
	cfg    Config
}

func New(db *pgxpool.Pool, authService *auth.Service, logger *slog.Logger, cfg Config) *Server {
	return &Server{db: db, auth: authService, logger: logger, cfg: cfg}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("GET /auth/login", s.login)
	mux.HandleFunc("GET /auth/callback", s.callback)
	mux.HandleFunc("GET /auth/me", s.me)
	mux.HandleFunc("POST /auth/logout", s.logout)
	mux.HandleFunc("POST /graphql", s.graphql)
	mux.HandleFunc("POST /documents", s.uploadDocument)
	mux.HandleFunc("GET /documents", s.documents)
	mux.HandleFunc("GET /documents/{id}", s.document)
	mux.HandleFunc("GET /documents/{id}/content", s.documentContent)
	mux.HandleFunc("GET /documents/{id}/preview", s.documentPreview)
	mux.HandleFunc("GET /documents/{id}/matches", s.documentMatches)
	mux.HandleFunc("POST /documents/{id}/retry", s.retryDocument)
	mux.HandleFunc("POST /documents/{id}/confirm", s.confirmEvidence)
	mux.HandleFunc("POST /purchases/{id}/evidence", s.confirmPurchaseEvidence)
	mux.HandleFunc("GET /purchases/{id}/evidence", s.purchaseEvidence)
	mux.HandleFunc("GET /events", s.events)
	return s.recover(s.accessLog(mux))
}

func (s *Server) graphql(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(w, r) {
		return
	}
	principal, err := s.authenticate(r)
	if err != nil {
		writeGraphError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	resolver := &graph.Resolver{DB: s.db, Auth: s.auth}
	executor := handler.New(graph.NewExecutableSchema(graph.Config{Resolvers: resolver}))
	executor.AddTransport(transport.POST{})
	executor.Use(extension.Introspection{})
	executor.SetErrorPresenter(func(ctx context.Context, err error) *gqlerror.Error {
		presented := graphql.DefaultErrorPresenter(ctx, err)
		if errors.Is(err, graph.ErrConflict) {
			presented.Extensions = map[string]any{"code": "CONFLICT"}
			presented.Message = "the resource changed since it was loaded"
		} else if errors.Is(err, graph.ErrNotFound) {
			presented.Extensions = map[string]any{"code": "NOT_FOUND"}
			presented.Message = "resource not found"
		} else if errors.Is(err, auth.ErrUnauthenticated) {
			presented.Extensions = map[string]any{"code": "UNAUTHENTICATED"}
			presented.Message = "authentication required"
		} else if errors.Is(err, graph.ErrBadInput) {
			presented.Extensions = map[string]any{"code": "BAD_USER_INPUT"}
			presented.Message = "invalid input"
		} else {
			presented.Extensions = map[string]any{"code": "INTERNAL_SERVER_ERROR"}
			presented.Message = "internal server error"
		}
		return presented
	})
	executor.SetRecoverFunc(func(ctx context.Context, recovered any) error {
		s.logger.ErrorContext(ctx, "GraphQL resolver panic", "panic", fmt.Sprint(recovered))
		return errors.New("internal server error")
	})
	request := http.MaxBytesReader(w, r.Body, 2<<20)
	r.Body = request
	ctx := auth.WithPrincipal(r.Context(), principal)
	executor.ServeHTTP(w, r.WithContext(ctx))
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	principal, err := s.authenticate(r)
	if err != nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	householdID := r.URL.Query().Get("householdId")
	householdUUID, err := uuid.FromString(householdID)
	if err != nil {
		http.Error(w, "household not found", http.StatusNotFound)
		return
	}
	memberUUID, err := uuid.FromString(principal.MemberID)
	if err != nil {
		http.Error(w, "household not found", http.StatusNotFound)
		return
	}
	member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberUUID)),
	))).Exists(r.Context(), bobpgx.NewPool(s.db))
	if err != nil || !member {
		http.Error(w, "household not found", http.StatusNotFound)
		return
	}
	after := r.URL.Query().Get("after")
	if header := r.Header.Get("Last-Event-ID"); header != "" {
		after = header
	}
	cursor, err := strconv.ParseInt(after, 10, 64)
	if after == "" {
		cursor = 0
		err = nil
	}
	if err != nil || cursor < 0 {
		http.Error(w, "invalid event cursor", http.StatusBadRequest)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		events, err := models.HouseholdEvents.Query(
			sm.Where(psql.And(
				models.HouseholdEvents.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
				models.HouseholdEvents.Columns.Cursor.GT(psql.Arg(cursor)),
			)),
			sm.OrderBy(models.HouseholdEvents.Columns.Cursor),
			sm.Limit(100),
		).All(r.Context(), bobpgx.NewPool(s.db))
		if err != nil {
			return
		}
		for _, storedEvent := range events {
			var event struct {
				Cursor        int64     `json:"cursor"`
				Type          string    `json:"type"`
				EntityID      string    `json:"entityId"`
				Revision      int64     `json:"revision"`
				ChangedFields []string  `json:"changedFields"`
				OccurredAt    time.Time `json:"occurredAt"`
			}
			event.Cursor = storedEvent.Cursor
			event.Type = storedEvent.Type
			event.EntityID = storedEvent.EntityID.String()
			event.Revision = storedEvent.Revision
			event.OccurredAt = storedEvent.CreatedAt
			if err := json.Unmarshal(storedEvent.ChangedFields.Val, &event.ChangedFields); err != nil {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.Cursor, event.Type, data); err != nil {
				return
			}
			cursor = event.Cursor
		}
		flusher.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.Ping(ctx); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		http.Error(w, "OIDC is not configured", http.StatusServiceUnavailable)
		return
	}
	s.auth.Login(w, r)
}

func (s *Server) callback(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		http.Error(w, "OIDC is not configured", http.StatusServiceUnavailable)
		return
	}
	s.auth.Callback(w, r)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"authenticated":false,"loginURL":"/auth/login"}`))
		return
	}
	s.auth.Me(w, r)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		http.Error(w, "OIDC is not configured", http.StatusServiceUnavailable)
		return
	}
	s.auth.Logout(w, r)
}

func (s *Server) authenticate(r *http.Request) (auth.Principal, error) {
	if s.auth == nil {
		return auth.Principal{}, auth.ErrUnauthenticated
	}
	return s.auth.Resolve(r)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.MarshalWrite(w, value)
}

func (s *Server) sameOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" || (origin != s.cfg.PublicOrigin && origin != s.cfg.FrontendOrigin) {
		http.Error(w, "request origin rejected", http.StatusForbidden)
		return false
	}
	return true
}

func (s *Server) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.ErrorContext(r.Context(), "HTTP handler panic", "panic", fmt.Sprint(recovered))
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		s.logger.InfoContext(r.Context(), "request completed", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start).String())
	})
}

func writeGraphError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.MarshalWrite(w, map[string]any{"errors": []map[string]any{{"message": message, "extensions": map[string]string{"code": code}}}})
}
