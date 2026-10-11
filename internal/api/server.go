package api

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph"
	"github.com/walnuts1018/horse/internal/ingestion"
	"uuid"
)

type Config struct {
	PublicOrigin   string
	FrontendOrigin string
	Store          *ingestion.ObjectStore
	Queue          *ingestion.Queue
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
	mux.HandleFunc("POST /documents/{id}/retry", s.retryDocument)
	mux.HandleFunc("POST /documents/{id}/confirm", s.confirmEvidence)
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
	var member bool
	if err := s.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM household_members WHERE household_id = $1 AND member_id = $2)`, householdID, principal.MemberID).Scan(&member); err != nil || !member {
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
		rows, err := s.db.Query(r.Context(), `SELECT cursor, type, entity_id::text, revision, changed_fields, created_at FROM household_events WHERE household_id = $1 AND cursor > $2 ORDER BY cursor LIMIT 100`, householdID, cursor)
		if err != nil {
			return
		}
		for rows.Next() {
			var event struct {
				Cursor        int64     `json:"cursor"`
				Type          string    `json:"type"`
				EntityID      string    `json:"entityId"`
				Revision      int64     `json:"revision"`
				ChangedFields []string  `json:"changedFields"`
				OccurredAt    time.Time `json:"occurredAt"`
			}
			if err := rows.Scan(&event.Cursor, &event.Type, &event.EntityID, &event.Revision, &event.ChangedFields, &event.OccurredAt); err != nil {
				rows.Close()
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.Cursor, event.Type, data); err != nil {
				rows.Close()
				return
			}
			cursor = event.Cursor
		}
		rows.Close()
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

type documentResponse struct {
	ID          string    `json:"id"`
	HouseholdID string    `json:"householdId"`
	SHA256      string    `json:"sha256"`
	MIMEType    string    `json:"mimeType"`
	SourceKind  string    `json:"sourceKind"`
	Status      string    `json:"status"`
	JobID       string    `json:"jobId,omitempty"`
	Duplicate   bool      `json:"duplicate"`
	Result      any       `json:"result,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (s *Server) uploadDocument(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(w, r) {
		return
	}
	principal, err := s.authenticate(r)
	if err != nil {
		writeGraphError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	if s.cfg.Store == nil {
		http.Error(w, "document storage is unavailable", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, ingestion.MaxDocumentBytes+(1<<20))
	// #nosec G120 -- MaxBytesReaderでmultipart全体を上限付きで読み取る。
	if err := r.ParseMultipartForm(ingestion.MaxDocumentBytes); err != nil {
		http.Error(w, "invalid document upload", http.StatusBadRequest)
		return
	}
	householdID := r.FormValue("householdId")
	sourceKind := r.FormValue("sourceKind")
	if !validSourceKind(sourceKind) {
		http.Error(w, "invalid source kind", http.StatusBadRequest)
		return
	}
	var member bool
	if err := s.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM household_members WHERE household_id = $1 AND member_id = $2)`, householdID, principal.MemberID).Scan(&member); err != nil || !member {
		http.Error(w, "household not found", http.StatusNotFound)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "document file is required", http.StatusBadRequest)
		return
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, ingestion.MaxDocumentBytes+1))
	if err != nil || len(data) == 0 || len(data) > ingestion.MaxDocumentBytes {
		http.Error(w, "invalid document size", http.StatusBadRequest)
		return
	}
	mimeType := documentMIME(header.Filename, data)
	if !validDocumentMIME(mimeType) {
		http.Error(w, "unsupported document type", http.StatusUnsupportedMediaType)
		return
	}
	var hints []ingestion.HistoryHint
	if encoded := r.FormValue("hints"); encoded != "" {
		if err := json.Unmarshal([]byte(encoded), &hints); err != nil || len(hints) > 30 {
			http.Error(w, "invalid extraction hints", http.StatusBadRequest)
			return
		}
	}
	key, hash, err := s.cfg.Store.Put(r.Context(), householdID, mimeType, data)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "document storage failed", "error", err)
		http.Error(w, "document storage failed", http.StatusBadGateway)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "document registration failed", http.StatusInternalServerError)
		return
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(r.Context())) }()
	id := uuid.NewV7().String()
	var insertedID string
	err = tx.QueryRow(r.Context(), `INSERT INTO source_documents(id, household_id, sha256, object_key, mime_type, source_kind, created_by) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (household_id, sha256) DO NOTHING RETURNING id::text`, id, householdID, hash, key, mimeType, sourceKind, principal.MemberID).Scan(&insertedID)
	if errors.Is(err, pgx.ErrNoRows) {
		var existing documentResponse
		var encoded []byte
		err = tx.QueryRow(r.Context(), `SELECT d.id::text,d.household_id::text,d.sha256,d.mime_type,d.source_kind,COALESCE(j.status,'queued'),COALESCE(j.id::text,''),COALESCE(j.result,'null'::jsonb),d.created_at FROM source_documents d LEFT JOIN LATERAL (SELECT id,status,result FROM extraction_jobs WHERE source_document_id=d.id ORDER BY version DESC LIMIT 1) j ON true WHERE d.household_id=$1 AND d.sha256=$2`, householdID, hash).Scan(&existing.ID, &existing.HouseholdID, &existing.SHA256, &existing.MIMEType, &existing.SourceKind, &existing.Status, &existing.JobID, &encoded, &existing.CreatedAt)
		if err != nil {
			http.Error(w, "document lookup failed", http.StatusInternalServerError)
			return
		}
		if err := json.Unmarshal(encoded, &existing.Result); err != nil {
			existing.Result = nil
		}
		existing.Duplicate = true
		_ = tx.Rollback(context.WithoutCancel(r.Context()))
		writeJSON(w, http.StatusOK, existing)
		return
	}
	if err != nil {
		http.Error(w, "document registration failed", http.StatusInternalServerError)
		return
	}
	jobID := uuid.NewV7().String()
	request, err := json.Marshal(map[string]any{"hints": hints})
	if err != nil {
		http.Error(w, "document registration failed", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO extraction_jobs(id, household_id, source_document_id, version, status, request) VALUES ($1,$2,$3,1,'queued',$4::jsonb)`, jobID, householdID, insertedID, string(request)); err != nil {
		http.Error(w, "document registration failed", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "document registration failed", http.StatusInternalServerError)
		return
	}
	if s.cfg.Queue != nil {
		if err := s.cfg.Queue.Publish(r.Context(), jobID); err != nil {
			s.logger.WarnContext(r.Context(), "extraction job notification failed", "job_id", jobID, "error", err)
		}
	}
	writeJSON(w, http.StatusAccepted, documentResponse{ID: insertedID, HouseholdID: householdID, SHA256: hash, MIMEType: mimeType, SourceKind: sourceKind, Status: "queued", JobID: jobID, CreatedAt: time.Now().UTC()})
}

func (s *Server) document(w http.ResponseWriter, r *http.Request) {
	principal, err := s.authenticate(r)
	if err != nil {
		writeGraphError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	var result documentResponse
	err = s.db.QueryRow(r.Context(), `SELECT d.id::text,d.household_id::text,d.sha256,d.mime_type,d.source_kind,COALESCE(j.status,'queued'),COALESCE(j.id::text,''),COALESCE(j.result,'null'::jsonb),d.created_at FROM source_documents d JOIN household_members hm ON hm.household_id=d.household_id AND hm.member_id=$2 LEFT JOIN LATERAL (SELECT id,status,result FROM extraction_jobs WHERE source_document_id=d.id ORDER BY version DESC LIMIT 1) j ON true WHERE d.id=$1`, r.PathValue("id"), principal.MemberID).Scan(&result.ID, &result.HouseholdID, &result.SHA256, &result.MIMEType, &result.SourceKind, &result.Status, &result.JobID, &result.Result, &result.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "document lookup failed", http.StatusInternalServerError)
		return
	}
	if encoded, ok := result.Result.([]byte); ok && len(encoded) > 0 {
		var decoded any
		if err := json.Unmarshal(encoded, &decoded); err == nil {
			result.Result = decoded
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) documents(w http.ResponseWriter, r *http.Request) {
	principal, err := s.authenticate(r)
	if err != nil {
		writeGraphError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	householdID := r.URL.Query().Get("householdId")
	var member bool
	if err := s.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM household_members WHERE household_id=$1 AND member_id=$2)`, householdID, principal.MemberID).Scan(&member); err != nil || !member {
		http.NotFound(w, r)
		return
	}
	limit, err := strconv.Atoi(r.URL.Query().Get("first"))
	if err != nil || limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		http.Error(w, "first must be between 1 and 100", http.StatusBadRequest)
		return
	}
	after := r.URL.Query().Get("after")
	if after != "" {
		if _, err := uuid.Parse(after); err != nil {
			http.Error(w, "invalid cursor", http.StatusBadRequest)
			return
		}
	}
	var cursor any
	if after != "" {
		cursor = after
	}
	rows, err := s.db.Query(r.Context(), `SELECT d.id::text,d.household_id::text,d.sha256,d.mime_type,d.source_kind,COALESCE(j.status,'queued'),COALESCE(j.id::text,''),COALESCE(j.result,'null'::jsonb),d.created_at FROM source_documents d LEFT JOIN LATERAL (SELECT id,status,result FROM extraction_jobs WHERE source_document_id=d.id ORDER BY version DESC LIMIT 1) j ON true WHERE d.household_id=$1 AND ($2::uuid IS NULL OR (d.created_at,d.id)<(SELECT cursor.created_at,cursor.id FROM source_documents cursor WHERE cursor.id=$2 AND cursor.household_id=$1)) ORDER BY d.created_at DESC,d.id DESC LIMIT $3`, householdID, cursor, limit+1)
	if err != nil {
		http.Error(w, "document lookup failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	result := make([]documentResponse, 0, limit+1)
	for rows.Next() {
		var item documentResponse
		var encoded []byte
		if err := rows.Scan(&item.ID, &item.HouseholdID, &item.SHA256, &item.MIMEType, &item.SourceKind, &item.Status, &item.JobID, &encoded, &item.CreatedAt); err != nil {
			http.Error(w, "document lookup failed", http.StatusInternalServerError)
			return
		}
		if err := json.Unmarshal(encoded, &item.Result); err != nil {
			item.Result = nil
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "document lookup failed", http.StatusInternalServerError)
		return
	}
	hasMore := len(result) > limit
	if hasMore {
		result = result[:limit]
	}
	var nextCursor *string
	if hasMore && len(result) > 0 {
		cursor := result[len(result)-1].ID
		nextCursor = &cursor
	}
	writeJSON(w, http.StatusOK, map[string]any{"documents": result, "nextCursor": nextCursor})
}

func (s *Server) retryDocument(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(w, r) {
		return
	}
	principal, err := s.authenticate(r)
	if err != nil {
		writeGraphError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	if s.cfg.Store == nil {
		http.Error(w, "document processing is unavailable", http.StatusServiceUnavailable)
		return
	}
	var input struct {
		Hints []ingestion.HistoryHint `json:"hints"`
	}
	if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, 64<<10), &input); err != nil || len(input.Hints) > 30 {
		http.Error(w, "invalid extraction hints", http.StatusBadRequest)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "extraction retry failed", http.StatusInternalServerError)
		return
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(r.Context())) }()
	var householdID string
	err = tx.QueryRow(r.Context(), `SELECT d.household_id::text FROM source_documents d JOIN household_members hm ON hm.household_id=d.household_id AND hm.member_id=$2 WHERE d.id=$1 FOR UPDATE OF d`, r.PathValue("id"), principal.MemberID).Scan(&householdID)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "document lookup failed", http.StatusInternalServerError)
		return
	}
	var latestStatus string
	var latestVersion int64
	err = tx.QueryRow(r.Context(), `SELECT status,version FROM extraction_jobs WHERE source_document_id=$1 ORDER BY version DESC LIMIT 1 FOR UPDATE`, r.PathValue("id")).Scan(&latestStatus, &latestVersion)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "extraction status lookup failed", http.StatusInternalServerError)
		return
	}
	if latestStatus == "queued" || latestStatus == "running" {
		http.Error(w, "extraction is already running", http.StatusConflict)
		return
	}
	request, err := json.Marshal(map[string]any{"hints": input.Hints})
	if err != nil {
		http.Error(w, "extraction retry failed", http.StatusInternalServerError)
		return
	}
	jobID := uuid.NewV7().String()
	if _, err := tx.Exec(r.Context(), `INSERT INTO extraction_jobs(id,household_id,source_document_id,version,status,request) VALUES ($1,$2,$3,$4,'queued',$5::jsonb)`, jobID, householdID, r.PathValue("id"), latestVersion+1, string(request)); err != nil {
		http.Error(w, "extraction retry failed", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "extraction retry failed", http.StatusInternalServerError)
		return
	}
	if s.cfg.Queue != nil {
		if err := s.cfg.Queue.Publish(r.Context(), jobID); err != nil {
			s.logger.WarnContext(r.Context(), "extraction job notification failed", "job_id", jobID, "error", err)
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"jobId": jobID, "status": "queued"})
}

func (s *Server) documentContent(w http.ResponseWriter, r *http.Request) {
	principal, err := s.authenticate(r)
	if err != nil {
		writeGraphError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	if s.cfg.Store == nil {
		http.Error(w, "document storage is unavailable", http.StatusServiceUnavailable)
		return
	}
	var householdID, key, mimeType string
	err = s.db.QueryRow(r.Context(), `SELECT d.household_id::text,d.object_key,d.mime_type FROM source_documents d JOIN household_members hm ON hm.household_id=d.household_id AND hm.member_id=$2 WHERE d.id=$1`, r.PathValue("id"), principal.MemberID).Scan(&householdID, &key, &mimeType)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "document lookup failed", http.StatusInternalServerError)
		return
	}
	doc, err := s.cfg.Store.Get(r.Context(), key, mimeType)
	if err != nil {
		http.Error(w, "document unavailable", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", doc.MIMEType)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Disposition", "inline")
	_, _ = w.Write(doc.Data)
}

func (s *Server) confirmEvidence(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(w, r) {
		return
	}
	principal, err := s.authenticate(r)
	if err != nil {
		writeGraphError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	var input struct {
		PurchaseID      string  `json:"purchaseId"`
		SourceReference string  `json:"sourceReference"`
		ItemID          *string `json:"itemId"`
	}
	if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, 64<<10), &input); err != nil || input.PurchaseID == "" || len(input.SourceReference) > 256 {
		http.Error(w, "invalid evidence confirmation", http.StatusBadRequest)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "evidence confirmation failed", http.StatusInternalServerError)
		return
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(r.Context())) }()
	var householdID string
	err = tx.QueryRow(r.Context(), `SELECT d.household_id::text FROM source_documents d JOIN household_members hm ON hm.household_id=d.household_id AND hm.member_id=$2 WHERE d.id=$1 FOR UPDATE OF d`, r.PathValue("id"), principal.MemberID).Scan(&householdID)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "evidence lookup failed", http.StatusInternalServerError)
		return
	}
	var purchaseRevision int64
	if err := tx.QueryRow(r.Context(), `SELECT revision FROM purchases WHERE id=$1 AND household_id=$2`, input.PurchaseID, householdID).Scan(&purchaseRevision); errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		http.Error(w, "purchase lookup failed", http.StatusInternalServerError)
		return
	}
	if input.ItemID != nil {
		var exists bool
		if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM purchase_items WHERE id=$1 AND purchase_id=$2)`, *input.ItemID, input.PurchaseID).Scan(&exists); err != nil {
			http.Error(w, "purchase item lookup failed", http.StatusInternalServerError)
			return
		}
		if !exists {
			http.NotFound(w, r)
			return
		}
	}
	var confirmedPurchaseID string
	err = tx.QueryRow(r.Context(), `SELECT purchase_id::text FROM document_confirmations WHERE source_document_id=$1 AND source_reference=$2`, r.PathValue("id"), input.SourceReference).Scan(&confirmedPurchaseID)
	if err == nil && confirmedPurchaseID != input.PurchaseID {
		http.Error(w, "document is already confirmed for another purchase", http.StatusConflict)
		return
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "evidence lookup failed", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO document_confirmations(source_document_id,source_reference,household_id,purchase_id,created_by) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (source_document_id,source_reference) DO NOTHING`, r.PathValue("id"), input.SourceReference, householdID, input.PurchaseID, principal.MemberID); err != nil {
		http.Error(w, "document confirmation failed", http.StatusConflict)
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO evidence_links(household_id,source_document_id,purchase_id,item_id) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`, householdID, r.PathValue("id"), input.PurchaseID, input.ItemID); err != nil {
		http.Error(w, "evidence link failed", http.StatusBadRequest)
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO household_events(household_id,type,entity_id,revision,changed_fields,actor_id) VALUES ($1,'evidence.linked',$2,$3,'["evidence"]'::jsonb,$4)`, householdID, r.PathValue("id"), purchaseRevision, principal.MemberID); err != nil {
		http.Error(w, "evidence event failed", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "evidence link failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"linked": true})
}

func validSourceKind(value string) bool {
	switch value {
	case "receipt", "order", "email", "card", "bank":
		return true
	default:
		return false
	}
}

func validDocumentMIME(value string) bool {
	switch value {
	case "image/jpeg", "image/png", "image/webp", "application/pdf", "text/plain", "text/csv", "message/rfc822":
		return true
	default:
		return false
	}
}

func documentMIME(filename string, data []byte) string {
	if mime := http.DetectContentType(data); strings.HasPrefix(mime, "image/") || mime == "application/pdf" {
		return mime
	}
	switch strings.ToLower(filename[strings.LastIndex(filename, ".")+1:]) {
	case "csv":
		return "text/csv"
	case "eml":
		return "message/rfc822"
	case "txt":
		return "text/plain"
	default:
		return ""
	}
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
