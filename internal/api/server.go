package api

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/errcode"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/application/mailforwarding"
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
	MailForwarding mailforwarding.Config
}

const (
	graphErrorCodeJSONKey         = "code"
	graphErrorCodeUnauthenticated = "UNAUTHENTICATED"
	graphUnauthenticatedMessage   = "authentication required"
	maxGraphQLOperationNameLength = 128
)

type DocumentStore interface {
	Put(context.Context, string, string, []byte) (string, string, error)
	PutAnalysis(context.Context, string, string, string, []byte) (string, string, error)
	Get(context.Context, string, string) (ingestion.Document, error)
}

type Server struct {
	db             *pgxpool.Pool
	auth           *auth.Service
	logger         *slog.Logger
	cfg            Config
	graphqlHandler *handler.Server
}

func New(db *pgxpool.Pool, authService *auth.Service, logger *slog.Logger, cfg Config) *Server {
	server := &Server{db: db, auth: authService, logger: logger, cfg: cfg}
	server.graphqlHandler = server.newGraphQLHandler()
	return server
}

func (s *Server) Handler() http.Handler {
	e := echo.New()
	add := func(method, path string, handler http.HandlerFunc) {
		wrapped := echo.WrapHandler(handler)
		e.Add(method, path, wrapped)
		if method == http.MethodGet {
			e.HEAD(path, wrapped)
		}
	}
	add(http.MethodGet, "/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	add(http.MethodGet, "/readyz", s.ready)
	add(http.MethodGet, "/auth/login", s.login)
	add(http.MethodGet, "/auth/callback", s.callback)
	add(http.MethodGet, "/auth/me", s.me)
	add(http.MethodPost, "/auth/logout", s.logout)
	add(http.MethodPost, "/graphql", s.graphql)
	add(http.MethodPost, "/documents", s.uploadDocument)
	add(http.MethodGet, "/documents", s.documents)
	add(http.MethodGet, "/documents/:id", s.document)
	add(http.MethodGet, "/documents/:id/content", s.documentContent)
	add(http.MethodGet, "/documents/:id/preview", s.documentPreview)
	add(http.MethodGet, "/documents/:id/matches", s.documentMatches)
	add(http.MethodDelete, "/documents/:id", s.deleteDocument)
	add(http.MethodPost, "/documents/:id/retry", s.retryDocument)
	add(http.MethodPost, "/documents/:id/interaction", s.interactWithDocument)
	add(http.MethodPost, "/documents/:id/confirm", s.confirmEvidence)
	add(http.MethodPost, "/purchases/:id/evidence", s.confirmPurchaseEvidence)
	add(http.MethodGet, "/purchases/:id/evidence", s.purchaseEvidence)
	add(http.MethodGet, "/events", s.events)
	return s.recover(s.accessLog(e))
}

func (s *Server) graphql(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(w, r) {
		return
	}
	principal, err := s.authenticate(r)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthenticated) {
			writeUnauthenticatedGraphError(w)
			return
		}
		s.writeAuthenticationUnavailable(w, r, err)
		writeGraphQLUnavailableBody(w)
		return
	}
	request := http.MaxBytesReader(w, r.Body, 2<<20)
	r.Body = request
	ctx := auth.WithPrincipal(r.Context(), principal)
	s.graphqlHandler.ServeHTTP(w, r.WithContext(ctx))
}

func (s *Server) newGraphQLHandler() *handler.Server {
	resolver := &graph.Resolver{
		DB: s.db, Auth: s.auth, Logger: s.logger,
		MailForwarding:        mailforwarding.NewService(s.db, s.cfg.MailForwarding),
		MailForwardingEnabled: s.cfg.MailForwarding.MailboxDomain != "" && s.cfg.MailForwarding.AddressLocalPartPrefix != "",
	}
	executor := handler.New(graph.NewExecutableSchema(graph.Config{
		Resolvers: resolver,
		Directives: graph.DirectiveRoot{
			NodeID: graph.NodeIDDirective,
		},
	}))
	executor.AddTransport(transport.SSE{KeepAlivePingInterval: 15 * time.Second})
	executor.AddTransport(transport.POST{})
	executor.Use(extension.Introspection{})
	executor.SetErrorPresenter(func(ctx context.Context, err error) *gqlerror.Error {
		presented := graphql.DefaultErrorPresenter(ctx, err)
		kind, isApplicationError := graph.ApplicationErrorKind(err)
		if isGraphQLProtocolError(presented) {
			s.logGraphQLProtocolRejection(ctx, presented)
			return presented
		} else if isApplicationError && kind == apperror.KindConflict {
			presented.Extensions = map[string]any{graphErrorCodeJSONKey: "CONFLICT"}
			presented.Message = "the resource changed since it was loaded"
		} else if isApplicationError && kind == apperror.KindNotFound {
			presented.Extensions = map[string]any{graphErrorCodeJSONKey: "NOT_FOUND"}
			presented.Message = "resource not found"
		} else if errors.Is(err, auth.ErrUnauthenticated) {
			presented.Extensions = map[string]any{graphErrorCodeJSONKey: "UNAUTHENTICATED"}
			presented.Message = "authentication required"
		} else if isApplicationError && kind == apperror.KindBadInput {
			s.logGraphQLInputRejection(ctx, err)
			presented.Extensions = map[string]any{graphErrorCodeJSONKey: "BAD_USER_INPUT"}
			presented.Message = "invalid input"
		} else {
			s.logGraphQLResolverFailure(ctx, err)
			presented.Extensions = map[string]any{graphErrorCodeJSONKey: "INTERNAL_SERVER_ERROR"}
			presented.Message = "internal server error"
		}
		return presented
	})
	executor.SetRecoverFunc(func(ctx context.Context, recovered any) error {
		s.logger.ErrorContext(ctx, "GraphQL resolver panic", "graphql_path", graphQLFieldPath(ctx), "panic_type", fmt.Sprintf("%T", recovered))
		return errors.New("internal server error")
	})
	return executor
}

func (s *Server) logGraphQLProtocolRejection(ctx context.Context, err *gqlerror.Error) {
	attrs := []any{"graphql_error_code", err.Extensions[graphErrorCodeJSONKey]}
	if operationName := graphQLOperationName(ctx); operationName != "" {
		attrs = append(attrs, "operation", operationName)
	}
	s.logger.WarnContext(ctx, "GraphQL protocol request rejected", attrs...)
}

func (s *Server) logGraphQLInputRejection(ctx context.Context, err error) {
	attrs := []any{"error_type", fmt.Sprintf("%T", err)}
	if path := graphQLFieldPath(ctx); path != "" {
		attrs = append(attrs, "graphql_path", path)
	}
	if operationName := graphQLOperationName(ctx); operationName != "" {
		attrs = append(attrs, "operation", operationName)
	}
	s.logger.InfoContext(ctx, "GraphQL input rejected", attrs...)
}

func graphQLOperationName(ctx context.Context) string {
	if !graphql.HasOperationContext(ctx) {
		return ""
	}
	operation := graphql.GetOperationContext(ctx).Operation
	if operation == nil || operation.Name == "" || len(operation.Name) > maxGraphQLOperationNameLength {
		return ""
	}

	return operation.Name
}

func (s *Server) logGraphQLResolverFailure(ctx context.Context, err error) {
	path := graphQLFieldPath(ctx)
	if path == "" {
		return
	}
	attrs := []any{"graphql_path", path, "error_type", fmt.Sprintf("%T", err)}
	if postgresError, ok := errors.AsType[*pgconn.PgError](err); ok {
		attrs = append(attrs,
			"postgres_code", postgresError.Code,
			"postgres_schema", postgresError.SchemaName,
			"postgres_table", postgresError.TableName,
			"postgres_column", postgresError.ColumnName,
			"postgres_constraint", postgresError.ConstraintName,
		)
	}
	s.logger.ErrorContext(ctx, "GraphQL resolver failed", attrs...)
}

func graphQLFieldPath(ctx context.Context) string {
	var fields []string
	for field := graphql.GetFieldContext(ctx); field != nil; field = field.Parent {
		if field.Index != nil {
			fields = append(fields, "["+strconv.Itoa(*field.Index)+"]")
		} else if field.Field.Field != nil && field.Field.Name != "" {
			fields = append(fields, field.Field.Name)
		}
	}
	slices.Reverse(fields)
	var path strings.Builder
	for _, field := range fields {
		if strings.HasPrefix(field, "[") {
			path.WriteString(field)
			continue
		}
		if path.Len() > 0 {
			path.WriteByte('.')
		}
		path.WriteString(field)
	}
	return path.String()
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	principal, err := s.authenticate(r)
	if err != nil {
		s.writeAuthenticationFailure(w, r, err)
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
			if _, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", event.Cursor, data); err != nil {
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
		defer func(ctx context.Context) {
			if recovered := recover(); recovered != nil {
				s.logger.ErrorContext(ctx, "HTTP handler panic", "panic", fmt.Sprint(recovered))
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}(r.Context())
		next.ServeHTTP(w, r)
	})
}

func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if IsHealthProbe(r) {
			return
		}
		s.logger.InfoContext(r.Context(), "request completed", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start).String())
	})
}

// IsHealthProbeはリクエストがAPIのヘルスプローブ宛てかを返す。
func IsHealthProbe(r *http.Request) bool {
	return r.URL.Path == "/healthz" || r.URL.Path == "/readyz"
}

func writeUnauthenticatedGraphError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.MarshalWrite(w, map[string]any{"errors": []map[string]any{{"message": graphUnauthenticatedMessage, "extensions": map[string]string{graphErrorCodeJSONKey: graphErrorCodeUnauthenticated}}}})
}

func (s *Server) writeAuthenticationFailure(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, auth.ErrUnauthenticated) {
		writeUnauthenticatedGraphError(w)
		return
	}
	s.writeAuthenticationUnavailable(w, r, err)
	http.Error(w, "service unavailable", http.StatusServiceUnavailable)
}

func (s *Server) writeAuthenticationUnavailable(w http.ResponseWriter, r *http.Request, err error) {
	cause := rootError(err)
	attrs := []any{"error_type", fmt.Sprintf("%T", cause)}
	databaseError, ok := errors.AsType[*pgconn.PgError](cause)
	if ok {
		attrs = append(attrs, "database_code", databaseError.Code)
	}
	s.logger.ErrorContext(r.Context(), "authentication resolution failed", attrs...)
}

func rootError(err error) error {
	for {
		cause := errors.Unwrap(err)
		if cause == nil {
			return err
		}
		err = cause
	}
}

func writeGraphQLUnavailableBody(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.MarshalWrite(w, map[string]any{"errors": []map[string]any{{"message": "service unavailable", "extensions": map[string]string{graphErrorCodeJSONKey: "SERVICE_UNAVAILABLE"}}}})
}

func isGraphQLProtocolError(err *gqlerror.Error) bool {
	if err == nil {
		return false
	}
	code, ok := err.Extensions[graphErrorCodeJSONKey].(string)
	return ok && (code == errcode.ParseFailed || code == errcode.ValidationFailed)
}
