package api

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/walnuts1018/horse/internal/ingestion"
)

type documentResponse struct {
	ID            string                 `json:"id"`
	HouseholdID   string                 `json:"householdId"`
	SHA256        string                 `json:"sha256"`
	MIMEType      string                 `json:"mimeType"`
	SourceKind    string                 `json:"sourceKind"`
	Status        string                 `json:"status"`
	JobID         string                 `json:"jobId,omitempty"`
	Duplicate     bool                   `json:"duplicate"`
	Result        any                    `json:"result,omitempty"`
	CreatedAt     time.Time              `json:"createdAt"`
	Confirmations []documentConfirmation `json:"confirmations"`
}

type documentConfirmation struct {
	SourceReference string `json:"sourceReference"`
	PurchaseID      string `json:"purchaseId"`
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
	if err := s.loadDocumentConfirmations(r.Context(), &result); err != nil {
		http.Error(w, "document confirmation lookup failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) loadDocumentConfirmations(ctx context.Context, document *documentResponse) error {
	rows, err := s.db.Query(ctx, `SELECT source_reference,purchase_id::text FROM document_confirmations WHERE source_document_id=$1 AND household_id=$2 ORDER BY source_reference`, document.ID, document.HouseholdID)
	if err != nil {
		return err
	}
	defer rows.Close()
	document.Confirmations = make([]documentConfirmation, 0)
	for rows.Next() {
		var confirmation documentConfirmation
		if err := rows.Scan(&confirmation.SourceReference, &confirmation.PurchaseID); err != nil {
			return err
		}
		document.Confirmations = append(document.Confirmations, confirmation)
	}
	return rows.Err()
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
	rows.Close()
	for i := range result {
		if err := s.loadDocumentConfirmations(r.Context(), &result[i]); err != nil {
			http.Error(w, "document confirmation lookup failed", http.StatusInternalServerError)
			return
		}
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
		PurchaseID       string  `json:"purchaseId"`
		SourceReference  string  `json:"sourceReference"`
		ItemID           *string `json:"itemId"`
		ObservationJobID *string `json:"observationJobId"`
		SourceItemIndex  *int    `json:"sourceItemIndex"`
	}
	if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, 64<<10), &input); err != nil || input.PurchaseID == "" || len(input.SourceReference) > 256 || (input.SourceItemIndex != nil && (input.ItemID == nil || input.ObservationJobID == nil || *input.SourceItemIndex < 0)) || (input.ObservationJobID != nil && input.SourceItemIndex == nil) {
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
	var region any
	observation := []byte(`{}`)
	if input.SourceItemIndex != nil {
		var payload []byte
		if err := tx.QueryRow(r.Context(), `SELECT result FROM extraction_jobs WHERE id=$1 AND source_document_id=$2 AND household_id=$3 AND status='succeeded'`, *input.ObservationJobID, r.PathValue("id"), householdID).Scan(&payload); err != nil {
			http.Error(w, "observation not found", http.StatusNotFound)
			return
		}
		var verified ingestion.Verification
		if err := json.Unmarshal(payload, &verified); err != nil || *input.SourceItemIndex >= len(verified.Receipt.Items) {
			http.Error(w, "invalid source item index", http.StatusBadRequest)
			return
		}
		item := verified.Receipt.Items[*input.SourceItemIndex]
		observation, err = json.Marshal(map[string]any{"jobId": *input.ObservationJobID, "itemIndex": *input.SourceItemIndex, "item": item, "transform": []int{1, 0, 0, 0, 1, 0, 0, 0, 1}})
		if err != nil {
			http.Error(w, "evidence encoding failed", http.StatusInternalServerError)
			return
		}
		if item.Region != nil {
			encoded, err := json.Marshal(item.Region)
			if err != nil {
				http.Error(w, "evidence encoding failed", http.StatusInternalServerError)
				return
			}
			region = string(encoded)
		}
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO evidence_links(household_id,source_document_id,purchase_id,item_id,evidence_region,observation) VALUES ($1,$2,$3,$4,$5::jsonb,$6::jsonb) ON CONFLICT DO NOTHING`, householdID, r.PathValue("id"), input.PurchaseID, input.ItemID, region, string(observation)); err != nil {
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
