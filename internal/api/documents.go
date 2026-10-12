package api

import (
	"context"
	"database/sql"
	stdjson "encoding/json"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"uuid"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	psqldialect "github.com/stephenafamo/bob/dialect/psql/dialect"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
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
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		http.Error(w, "household not found", http.StatusNotFound)
		return
	}
	memberID, err := gofrsuuid.FromString(principal.MemberID)
	if err != nil {
		http.Error(w, "household not found", http.StatusNotFound)
		return
	}
	member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberID)),
	))).Exists(r.Context(), bobpgx.NewPool(s.db))
	if err != nil || !member {
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
	id, err := gofrsuuid.FromString(uuid.NewV7().String())
	if err != nil {
		http.Error(w, "document registration failed", http.StatusInternalServerError)
		return
	}
	executor := bobpgx.NewTx(tx, nil)
	document, err := models.SourceDocuments.Insert(
		&models.SourceDocumentSetter{ID: &id, HouseholdID: &householdUUID, Sha256: &hash, ObjectKey: &key, MimeType: &mimeType, SourceKind: &sourceKind, CreatedBy: &memberID},
		im.OnConflict("household_id", "sha256").DoNothing(),
	).One(r.Context(), executor)
	if errors.Is(err, sql.ErrNoRows) {
		existingDocument, lookupErr := models.SourceDocuments.Query(sm.Where(psql.And(
			models.SourceDocuments.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			models.SourceDocuments.Columns.Sha256.EQ(psql.Arg(hash)),
		))).One(r.Context(), executor)
		var existing documentResponse
		if lookupErr == nil {
			existing, lookupErr = s.documentResponse(r.Context(), executor, existingDocument)
		}
		err = lookupErr
		if err != nil {
			http.Error(w, "document lookup failed", http.StatusInternalServerError)
			return
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
	jobID, err := gofrsuuid.FromString(uuid.NewV7().String())
	if err != nil {
		http.Error(w, "document registration failed", http.StatusInternalServerError)
		return
	}
	request, err := json.Marshal(map[string]any{"hints": hints})
	if err != nil {
		http.Error(w, "document registration failed", http.StatusInternalServerError)
		return
	}
	requestJSON := types.JSON[stdjson.RawMessage]{Val: request}
	version := int32(1)
	status := "queued"
	if _, err := models.ExtractionJobs.Insert(&models.ExtractionJobSetter{
		ID: &jobID, HouseholdID: &householdUUID, SourceDocumentID: &document.ID, Version: &version, Status: &status, Request: &requestJSON,
	}).Exec(r.Context(), executor); err != nil {
		http.Error(w, "document registration failed", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "document registration failed", http.StatusInternalServerError)
		return
	}
	if s.cfg.Queue != nil {
		if err := s.cfg.Queue.Publish(r.Context(), jobID.String()); err != nil {
			s.logger.WarnContext(r.Context(), "extraction job notification failed", "job_id", jobID.String(), "error", err)
		}
	}
	writeJSON(w, http.StatusAccepted, documentResponse{ID: document.ID.String(), HouseholdID: householdID, SHA256: hash, MIMEType: mimeType, SourceKind: sourceKind, Status: "queued", JobID: jobID.String(), CreatedAt: document.CreatedAt})
}

func (s *Server) documentResponse(ctx context.Context, exec bob.Executor, document *models.SourceDocument) (documentResponse, error) {
	result := documentResponse{
		ID: document.ID.String(), HouseholdID: document.HouseholdID.String(),
		SHA256: document.Sha256, MIMEType: document.MimeType, SourceKind: document.SourceKind,
		Status: "queued", CreatedAt: document.CreatedAt,
	}
	job, err := models.ExtractionJobs.Query(
		sm.Where(models.ExtractionJobs.Columns.SourceDocumentID.EQ(psql.Arg(document.ID))),
		sm.OrderBy(models.ExtractionJobs.Columns.Version).Desc(),
		sm.Limit(1),
	).One(ctx, exec)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return documentResponse{}, err
	}
	result.Status = job.Status
	result.JobID = job.ID.String()
	if job.Result.Valid {
		var decoded any
		if err := json.Unmarshal(job.Result.V.Val, &decoded); err == nil {
			result.Result = decoded
		}
	}
	return result, nil
}

func documentForMember(ctx context.Context, exec bob.Executor, documentID, memberID gofrsuuid.UUID, lock bool) (*models.SourceDocument, error) {
	mods := []bob.Mod[*psqldialect.SelectQuery]{
		sm.Where(models.SourceDocuments.Columns.ID.EQ(psql.Arg(documentID))),
	}
	if lock {
		mods = append(mods, sm.ForUpdate())
	}
	document, err := models.SourceDocuments.Query(mods...).One(ctx, exec)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, pgx.ErrNoRows
		}
		return nil, err
	}
	member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(document.HouseholdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberID)),
	))).Exists(ctx, exec)
	if err != nil {
		return nil, err
	}
	if !member {
		return nil, pgx.ErrNoRows
	}
	return document, nil
}

func (s *Server) document(w http.ResponseWriter, r *http.Request) {
	principal, err := s.authenticate(r)
	if err != nil {
		writeGraphError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	documentID, err := gofrsuuid.FromString(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	memberID, err := gofrsuuid.FromString(principal.MemberID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	storedDocument, err := documentForMember(r.Context(), bobpgx.NewPool(s.db), documentID, memberID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "document lookup failed", http.StatusInternalServerError)
		return
	}
	result, err := s.documentResponse(r.Context(), bobpgx.NewPool(s.db), storedDocument)
	if err != nil {
		http.Error(w, "document lookup failed", http.StatusInternalServerError)
		return
	}
	if err := s.loadDocumentConfirmations(r.Context(), &result); err != nil {
		http.Error(w, "document confirmation lookup failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) loadDocumentConfirmations(ctx context.Context, document *documentResponse) error {
	documentID, err := gofrsuuid.FromString(document.ID)
	if err != nil {
		return err
	}
	householdID, err := gofrsuuid.FromString(document.HouseholdID)
	if err != nil {
		return err
	}
	confirmations, err := models.DocumentConfirmations.Query(
		sm.Where(psql.And(
			models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(documentID)),
			models.DocumentConfirmations.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		)),
		sm.OrderBy(models.DocumentConfirmations.Columns.SourceReference),
	).All(ctx, bobpgx.NewPool(s.db))
	if err != nil {
		return err
	}
	document.Confirmations = make([]documentConfirmation, 0)
	for _, confirmation := range confirmations {
		document.Confirmations = append(document.Confirmations, documentConfirmation{
			SourceReference: confirmation.SourceReference, PurchaseID: confirmation.PurchaseID.String(),
		})
	}
	return nil
}

func (s *Server) documents(w http.ResponseWriter, r *http.Request) {
	principal, err := s.authenticate(r)
	if err != nil {
		writeGraphError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	householdID := r.URL.Query().Get("householdId")
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	memberID, err := gofrsuuid.FromString(principal.MemberID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberID)),
	))).Exists(r.Context(), bobpgx.NewPool(s.db))
	if err != nil || !member {
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
	var cursor *models.SourceDocument
	if after != "" {
		cursorID, err := gofrsuuid.FromString(after)
		if err != nil {
			http.Error(w, "invalid cursor", http.StatusBadRequest)
			return
		}
		cursor, err = models.SourceDocuments.Query(sm.Where(psql.And(
			models.SourceDocuments.Columns.ID.EQ(psql.Arg(cursorID)),
			models.SourceDocuments.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		))).One(r.Context(), bobpgx.NewPool(s.db))
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusOK, map[string]any{"documents": []documentResponse{}, "nextCursor": nil})
			return
		}
		if err != nil {
			http.Error(w, "document lookup failed", http.StatusInternalServerError)
			return
		}
	}
	documentMods := []bob.Mod[*psqldialect.SelectQuery]{
		sm.Where(models.SourceDocuments.Columns.HouseholdID.EQ(psql.Arg(householdUUID))),
		sm.OrderBy(models.SourceDocuments.Columns.CreatedAt).Desc(),
		sm.OrderBy(models.SourceDocuments.Columns.ID).Desc(),
		sm.Limit(limit + 1),
	}
	if cursor != nil {
		documentMods = append(documentMods, sm.Where(psql.Or(
			models.SourceDocuments.Columns.CreatedAt.LT(psql.Arg(cursor.CreatedAt)),
			psql.And(
				models.SourceDocuments.Columns.CreatedAt.EQ(psql.Arg(cursor.CreatedAt)),
				models.SourceDocuments.Columns.ID.LT(psql.Arg(cursor.ID)),
			),
		)))
	}
	storedDocuments, err := models.SourceDocuments.Query(documentMods...).All(r.Context(), bobpgx.NewPool(s.db))
	if err != nil {
		http.Error(w, "document lookup failed", http.StatusInternalServerError)
		return
	}
	result := make([]documentResponse, 0, limit+1)
	for _, storedDocument := range storedDocuments {
		item, err := s.documentResponse(r.Context(), bobpgx.NewPool(s.db), storedDocument)
		if err != nil {
			http.Error(w, "document lookup failed", http.StatusInternalServerError)
			return
		}
		result = append(result, item)
	}
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
	documentID, err := gofrsuuid.FromString(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	memberID, err := gofrsuuid.FromString(principal.MemberID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	executor := bobpgx.NewTx(tx, nil)
	document, err := documentForMember(r.Context(), executor, documentID, memberID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "document lookup failed", http.StatusInternalServerError)
		return
	}
	latestJob, err := models.ExtractionJobs.Query(
		sm.Where(models.ExtractionJobs.Columns.SourceDocumentID.EQ(psql.Arg(document.ID))),
		sm.OrderBy(models.ExtractionJobs.Columns.Version).Desc(),
		sm.Limit(1), sm.ForUpdate(),
	).One(r.Context(), executor)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "extraction status lookup failed", http.StatusInternalServerError)
		return
	}
	if latestJob != nil && (latestJob.Status == "queued" || latestJob.Status == "running") {
		http.Error(w, "extraction is already running", http.StatusConflict)
		return
	}
	request, err := json.Marshal(map[string]any{"hints": input.Hints})
	if err != nil {
		http.Error(w, "extraction retry failed", http.StatusInternalServerError)
		return
	}
	jobID, err := gofrsuuid.FromString(uuid.NewV7().String())
	if err != nil {
		http.Error(w, "extraction retry failed", http.StatusInternalServerError)
		return
	}
	version := int32(1)
	if latestJob != nil {
		version = latestJob.Version + 1
	}
	status := "queued"
	requestJSON := types.JSON[stdjson.RawMessage]{Val: request}
	if _, err := models.ExtractionJobs.Insert(&models.ExtractionJobSetter{
		ID: &jobID, HouseholdID: &document.HouseholdID, SourceDocumentID: &document.ID, Version: &version, Status: &status, Request: &requestJSON,
	}).Exec(r.Context(), executor); err != nil {
		http.Error(w, "extraction retry failed", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "extraction retry failed", http.StatusInternalServerError)
		return
	}
	if s.cfg.Queue != nil {
		if err := s.cfg.Queue.Publish(r.Context(), jobID.String()); err != nil {
			s.logger.WarnContext(r.Context(), "extraction job notification failed", "job_id", jobID.String(), "error", err)
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"jobId": jobID.String(), "status": "queued"})
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
	documentID, err := gofrsuuid.FromString(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	memberID, err := gofrsuuid.FromString(principal.MemberID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	document, err := documentForMember(r.Context(), bobpgx.NewPool(s.db), documentID, memberID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "document lookup failed", http.StatusInternalServerError)
		return
	}
	doc, err := s.cfg.Store.Get(r.Context(), document.ObjectKey, document.MimeType)
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
	documentID, err := gofrsuuid.FromString(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	memberID, err := gofrsuuid.FromString(principal.MemberID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	executor := bobpgx.NewTx(tx, nil)
	document, err := documentForMember(r.Context(), executor, documentID, memberID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "evidence lookup failed", http.StatusInternalServerError)
		return
	}
	purchaseID, err := gofrsuuid.FromString(input.PurchaseID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	purchase, err := models.Purchases.Query(sm.Where(psql.And(
		models.Purchases.Columns.ID.EQ(psql.Arg(purchaseID)),
		models.Purchases.Columns.HouseholdID.EQ(psql.Arg(document.HouseholdID)),
	))).One(r.Context(), executor)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		http.Error(w, "purchase lookup failed", http.StatusInternalServerError)
		return
	}
	if input.ItemID != nil {
		itemID, err := gofrsuuid.FromString(*input.ItemID)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		exists, err := models.PurchaseItems.Query(sm.Where(psql.And(
			models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemID)),
			models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseID)),
		))).Exists(r.Context(), executor)
		if err != nil {
			http.Error(w, "purchase item lookup failed", http.StatusInternalServerError)
			return
		}
		if !exists {
			http.NotFound(w, r)
			return
		}
	}
	confirmation, err := models.DocumentConfirmations.Query(sm.Where(psql.And(
		models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(document.ID)),
		models.DocumentConfirmations.Columns.SourceReference.EQ(psql.Arg(input.SourceReference)),
	))).One(r.Context(), executor)
	if err == nil && confirmation.PurchaseID != purchase.ID {
		http.Error(w, "document is already confirmed for another purchase", http.StatusConflict)
		return
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "evidence lookup failed", http.StatusInternalServerError)
		return
	}
	if _, err := models.DocumentConfirmations.Insert(&models.DocumentConfirmationSetter{
		SourceDocumentID: &document.ID, SourceReference: &input.SourceReference,
		HouseholdID: &document.HouseholdID, PurchaseID: &purchase.ID, CreatedBy: &memberID,
	}, im.OnConflict("source_document_id", "source_reference").DoNothing()).Exec(r.Context(), executor); err != nil {
		http.Error(w, "document confirmation failed", http.StatusConflict)
		return
	}
	var region any
	observation := []byte(`{}`)
	if input.SourceItemIndex != nil {
		observationJobID, err := gofrsuuid.FromString(*input.ObservationJobID)
		if err != nil {
			http.Error(w, "observation not found", http.StatusNotFound)
			return
		}
		job, err := models.ExtractionJobs.Query(sm.Where(psql.And(
			models.ExtractionJobs.Columns.ID.EQ(psql.Arg(observationJobID)),
			models.ExtractionJobs.Columns.SourceDocumentID.EQ(psql.Arg(document.ID)),
			models.ExtractionJobs.Columns.HouseholdID.EQ(psql.Arg(document.HouseholdID)),
			models.ExtractionJobs.Columns.Status.EQ(psql.Arg("succeeded")),
		))).One(r.Context(), executor)
		if err != nil || !job.Result.Valid {
			http.Error(w, "observation not found", http.StatusNotFound)
			return
		}
		var verified ingestion.Verification
		if err := json.Unmarshal(job.Result.V.Val, &verified); err != nil || *input.SourceItemIndex >= len(verified.Receipt.Items) {
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
	var itemID sql.Null[gofrsuuid.UUID]
	if input.ItemID != nil {
		itemIDUUID, err := gofrsuuid.FromString(*input.ItemID)
		if err != nil {
			http.Error(w, "evidence link failed", http.StatusBadRequest)
			return
		}
		itemID = sql.Null[gofrsuuid.UUID]{V: itemIDUUID, Valid: true}
	}
	var evidenceRegion sql.Null[types.JSON[stdjson.RawMessage]]
	if region != nil {
		encoded, ok := region.(string)
		if !ok {
			http.Error(w, "evidence encoding failed", http.StatusInternalServerError)
			return
		}
		evidenceRegion = sql.Null[types.JSON[stdjson.RawMessage]]{
			V: types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(encoded)}, Valid: true,
		}
	}
	observationJSON := types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(observation)}
	if _, err := psql.Insert(
		im.Into(psql.Quote("evidence_links"), "household_id", "source_document_id", "purchase_id", "item_id", "evidence_region", "observation"),
		im.Values(psql.Arg(document.HouseholdID, document.ID, purchase.ID, itemID, evidenceRegion, observationJSON)),
		im.OnConflict().DoNothing(),
	).Exec(r.Context(), executor); err != nil {
		http.Error(w, "evidence link failed", http.StatusBadRequest)
		return
	}
	eventType := "evidence.linked"
	changedFields := types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(`["evidence"]`)}
	actorID := sql.Null[gofrsuuid.UUID]{V: memberID, Valid: true}
	if _, err := models.HouseholdEvents.Insert(&models.HouseholdEventSetter{
		HouseholdID: &document.HouseholdID, Type: &eventType, EntityID: &document.ID,
		Revision: &purchase.Revision, ChangedFields: &changedFields, ActorID: &actorID,
	}).Exec(r.Context(), executor); err != nil {
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
