package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	stdjson "encoding/json"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/gabriel-vasile/mimetype"
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

const (
	documentStatusQueued      = "queued"
	documentMIMEPDF           = "application/pdf"
	documentMIMEJPEG          = "image/jpeg"
	documentMIMEPNG           = "image/png"
	documentMIMEWebP          = "image/webp"
	documentMIMEEmail         = "message/rfc822"
	documentSourceKindReceipt = "receipt"
	jobIDJSONKey              = "jobId"
)

type documentResponse struct {
	ID               string                    `json:"id"`
	HouseholdID      string                    `json:"householdId"`
	Name             string                    `json:"name,omitempty"`
	SHA256           string                    `json:"sha256"`
	MIMEType         string                    `json:"mimeType"`
	SourceKind       string                    `json:"sourceKind"`
	Status           string                    `json:"status"`
	JobID            string                    `json:"jobId,omitempty"`
	Duplicate        bool                      `json:"duplicate"`
	Result           any                       `json:"result,omitempty"`
	CreatedAt        time.Time                 `json:"createdAt"`
	Confirmations    []documentConfirmation    `json:"confirmations"`
	HasEvidence      bool                      `json:"hasEvidence"`
	Attachments      []documentAttachment      `json:"attachments,omitempty"`
	AttachmentParent *documentAttachmentParent `json:"attachmentParent,omitempty"`
}

type documentAttachment struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Filename string `json:"filename"`
	MIMEType string `json:"mimeType"`
	Status   string `json:"status,omitempty"`
	JobID    string `json:"jobId,omitempty"`
}

type documentAttachmentParent struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Filename string `json:"filename"`
	Index    int32  `json:"index"`
}

type documentConfirmation struct {
	SourceReference   string `json:"sourceReference"`
	PurchaseID        string `json:"purchaseId"`
	AccountMovementID string `json:"accountMovementId,omitempty"`
}

func (s *Server) uploadDocument(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(w, r) {
		return
	}
	principal, err := s.authenticate(r)
	if err != nil {
		writeUnauthenticatedGraphError(w)
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
	householdUUID, err := parseNodeIDOrUUID(householdID, "Household")
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
	filename := sql.Null[string]{V: header.Filename, Valid: header.Filename != ""}
	if !validDocumentMIME(mimeType) {
		http.Error(w, "unsupported document type", http.StatusUnsupportedMediaType)
		return
	}
	var emailAttachments []ingestion.EmailAttachment
	if mimeType == documentMIMEEmail {
		emailAttachments, err = ingestion.ParseEmailAttachments(data)
		if err != nil {
			http.Error(w, "invalid email message", http.StatusBadRequest)
			return
		}
	}
	householdID = householdUUID.String()
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
	defer func(ctx context.Context) { _ = tx.Rollback(ctx) }(context.WithoutCancel(r.Context()))
	id, err := gofrsuuid.FromString(uuid.NewV7().String())
	if err != nil {
		http.Error(w, "document registration failed", http.StatusInternalServerError)
		return
	}
	executor := bobpgx.NewTx(tx, nil)
	document, err := models.SourceDocuments.Insert(
		&models.SourceDocumentSetter{ID: &id, HouseholdID: &householdUUID, Sha256: &hash, ObjectKey: &key, MimeType: &mimeType, SourceKind: &sourceKind, SourceFilename: &filename, CreatedBy: &memberID},
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
		if len(emailAttachments) > 0 {
			queuedJobIDs, attachmentErr := s.registerEmailAttachments(r.Context(), executor, householdUUID, memberID, existingDocument.ID, householdID, emailAttachments)
			if attachmentErr != nil {
				http.Error(w, "document registration failed", http.StatusInternalServerError)
				return
			}
			if err := tx.Commit(r.Context()); err != nil {
				http.Error(w, "document registration failed", http.StatusInternalServerError)
				return
			}
			s.publishDocumentJobs(r.Context(), queuedJobIDs)
			existing, err = s.documentResponse(r.Context(), bobpgx.NewPool(s.db), existingDocument)
			if err == nil {
				err = s.loadDocumentAttachments(r.Context(), bobpgx.NewPool(s.db), &existing)
			}
			if err != nil {
				http.Error(w, "document lookup failed", http.StatusInternalServerError)
				return
			}
			existing.Duplicate = true
			writeJSON(w, http.StatusOK, existing)
			return
		}
		if err := s.loadDocumentAttachments(r.Context(), executor, &existing); err != nil {
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
	jobID, err := insertDocumentJob(r.Context(), executor, householdUUID, document.ID)
	if err != nil {
		http.Error(w, "document registration failed", http.StatusInternalServerError)
		return
	}
	attachmentJobIDs, err := s.registerEmailAttachments(r.Context(), executor, householdUUID, memberID, document.ID, householdID, emailAttachments)
	if err != nil {
		http.Error(w, "document registration failed", http.StatusInternalServerError)
		return
	}
	queuedJobIDs := append([]gofrsuuid.UUID{jobID}, attachmentJobIDs...)
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "document registration failed", http.StatusInternalServerError)
		return
	}
	s.publishDocumentJobs(r.Context(), queuedJobIDs)
	result, err := s.documentResponse(r.Context(), bobpgx.NewPool(s.db), document)
	if err != nil {
		http.Error(w, "document lookup failed", http.StatusInternalServerError)
		return
	}
	if err := s.loadDocumentAttachments(r.Context(), bobpgx.NewPool(s.db), &result); err != nil {
		http.Error(w, "document lookup failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (s *Server) registerEmailAttachments(ctx context.Context, exec bob.Executor, householdID, memberID, parentID gofrsuuid.UUID, householdKey string, attachments []ingestion.EmailAttachment) ([]gofrsuuid.UUID, error) {
	queuedJobIDs := make([]gofrsuuid.UUID, 0, len(attachments))
	seenChildren := make(map[gofrsuuid.UUID]struct{}, len(attachments))
	for index, attachment := range attachments {
		childKey, childHash, err := s.cfg.Store.Put(ctx, householdKey, attachment.MIMEType, attachment.Data)
		if err != nil {
			s.logger.ErrorContext(ctx, "email attachment storage failed", "error", err)
			return nil, err
		}
		childID, childJobID, err := insertAttachmentDocument(ctx, exec, householdID, memberID, attachment, childKey, childHash)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seenChildren[childID]; duplicate {
			continue
		}
		seenChildren[childID] = struct{}{}
		attachmentIndex := int32(index)
		if _, err := models.SourceDocumentAttachments.Insert(&models.SourceDocumentAttachmentSetter{
			HouseholdID: &householdID, ParentDocumentID: &parentID, ChildDocumentID: &childID,
			AttachmentIndex: &attachmentIndex, Filename: &attachment.Filename, MimeType: &attachment.MIMEType,
		}, im.OnConflict("parent_document_id", "attachment_index").DoNothing()).Exec(ctx, exec); err != nil {
			return nil, err
		}
		if childJobID != nil {
			queuedJobIDs = append(queuedJobIDs, *childJobID)
		}
	}
	return queuedJobIDs, nil
}

func (s *Server) publishDocumentJobs(ctx context.Context, jobIDs []gofrsuuid.UUID) {
	if s.cfg.Queue == nil {
		return
	}
	for _, jobID := range jobIDs {
		if err := s.cfg.Queue.Publish(ctx, jobID.String()); err != nil {
			s.logger.WarnContext(ctx, "extraction job notification failed", "job_id", jobID.String(), "error", err)
		}
	}
}

func insertDocumentJob(ctx context.Context, exec bob.Executor, householdID, documentID gofrsuuid.UUID) (gofrsuuid.UUID, error) {
	jobID, err := gofrsuuid.FromString(uuid.NewV7().String())
	if err != nil {
		return gofrsuuid.Nil, err
	}
	request, err := json.Marshal(map[string]any{"purpose": ingestion.JobPurposeInteractive})
	if err != nil {
		return gofrsuuid.Nil, err
	}
	requestJSON := types.JSON[stdjson.RawMessage]{Val: request}
	version := int32(1)
	status := documentStatusQueued
	job, err := models.ExtractionJobs.Insert(&models.ExtractionJobSetter{
		ID: &jobID, HouseholdID: &householdID, SourceDocumentID: &documentID, Version: &version, Status: &status, Request: &requestJSON,
	}, im.OnConflict("source_document_id", "version").DoNothing()).One(ctx, exec)
	if errors.Is(err, sql.ErrNoRows) {
		job, err = models.ExtractionJobs.Query(sm.Where(psql.And(
			models.ExtractionJobs.Columns.SourceDocumentID.EQ(psql.Arg(documentID)),
			models.ExtractionJobs.Columns.Version.EQ(psql.Arg(version)),
		))).One(ctx, exec)
	}
	if err != nil {
		return gofrsuuid.Nil, err
	}
	return job.ID, nil
}

func insertAttachmentDocument(ctx context.Context, exec bob.Executor, householdID, memberID gofrsuuid.UUID, attachment ingestion.EmailAttachment, key, hash string) (gofrsuuid.UUID, *gofrsuuid.UUID, error) {
	id, err := gofrsuuid.FromString(uuid.NewV7().String())
	if err != nil {
		return gofrsuuid.Nil, nil, err
	}
	sourceKind := documentSourceKindReceipt
	filename := sql.Null[string]{V: attachment.Filename, Valid: true}
	document, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{
		ID: &id, HouseholdID: &householdID, Sha256: &hash, ObjectKey: &key, MimeType: &attachment.MIMEType,
		SourceKind: &sourceKind, SourceFilename: &filename, CreatedBy: &memberID,
	}, im.OnConflict("household_id", "sha256").DoNothing()).One(ctx, exec)
	if errors.Is(err, sql.ErrNoRows) {
		document, err = models.SourceDocuments.Query(sm.Where(psql.And(
			models.SourceDocuments.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.SourceDocuments.Columns.Sha256.EQ(psql.Arg(hash)),
		))).One(ctx, exec)
	}
	if err != nil {
		return gofrsuuid.Nil, nil, err
	}
	_, err = models.ExtractionJobs.Query(
		sm.Where(models.ExtractionJobs.Columns.SourceDocumentID.EQ(psql.Arg(document.ID))),
		sm.OrderBy(models.ExtractionJobs.Columns.Version).Desc(), sm.Limit(1),
	).One(ctx, exec)
	if err == nil {
		return document.ID, nil, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return gofrsuuid.Nil, nil, err
	}
	jobID, err := insertDocumentJob(ctx, exec, householdID, document.ID)
	if err != nil {
		return gofrsuuid.Nil, nil, err
	}
	return document.ID, &jobID, nil
}

func (s *Server) documentResponse(ctx context.Context, exec bob.Executor, document *models.SourceDocument) (documentResponse, error) {
	result := documentResponse{
		ID: document.ID.String(), HouseholdID: document.HouseholdID.String(),
		Name:   document.SourceFilename.V,
		SHA256: document.Sha256, MIMEType: document.MimeType, SourceKind: document.SourceKind,
		Status: documentStatusQueued, CreatedAt: document.CreatedAt,
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

func (s *Server) loadDocumentAttachments(ctx context.Context, exec bob.Executor, document *documentResponse) error {
	documentID, err := gofrsuuid.FromString(document.ID)
	if err != nil {
		return err
	}
	householdID, err := gofrsuuid.FromString(document.HouseholdID)
	if err != nil {
		return err
	}
	children, err := models.SourceDocumentAttachments.Query(
		sm.Where(psql.And(
			models.SourceDocumentAttachments.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.SourceDocumentAttachments.Columns.ParentDocumentID.EQ(psql.Arg(documentID)),
		)),
		sm.OrderBy(models.SourceDocumentAttachments.Columns.AttachmentIndex),
	).All(ctx, exec)
	if err != nil {
		return err
	}
	if len(children) > 0 {
		document.Attachments = make([]documentAttachment, 0, len(children))
	}
	for _, relation := range children {
		child, lookupErr := models.SourceDocuments.Query(sm.Where(psql.And(
			models.SourceDocuments.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.SourceDocuments.Columns.ID.EQ(psql.Arg(relation.ChildDocumentID)),
		))).One(ctx, exec)
		if lookupErr != nil {
			return lookupErr
		}
		attachment := documentAttachment{
			ID: child.ID.String(), Name: child.SourceFilename.V, Filename: relation.Filename, MIMEType: relation.MimeType,
		}
		job, jobErr := models.ExtractionJobs.Query(
			sm.Where(models.ExtractionJobs.Columns.SourceDocumentID.EQ(psql.Arg(child.ID))),
			sm.OrderBy(models.ExtractionJobs.Columns.Version).Desc(), sm.Limit(1),
		).One(ctx, exec)
		if jobErr == nil {
			attachment.Status = job.Status
			attachment.JobID = job.ID.String()
		} else if !errors.Is(jobErr, sql.ErrNoRows) {
			return jobErr
		}
		document.Attachments = append(document.Attachments, attachment)
	}
	parents, err := models.SourceDocumentAttachments.Query(
		sm.Where(psql.And(
			models.SourceDocumentAttachments.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.SourceDocumentAttachments.Columns.ChildDocumentID.EQ(psql.Arg(documentID)),
		)),
		sm.OrderBy(models.SourceDocumentAttachments.Columns.ParentDocumentID),
		sm.OrderBy(models.SourceDocumentAttachments.Columns.AttachmentIndex),
		sm.Limit(1),
	).One(ctx, exec)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	parent, err := models.SourceDocuments.Query(sm.Where(psql.And(
		models.SourceDocuments.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.SourceDocuments.Columns.ID.EQ(psql.Arg(parents.ParentDocumentID)),
	))).One(ctx, exec)
	if err != nil {
		return err
	}
	document.AttachmentParent = &documentAttachmentParent{
		ID: parent.ID.String(), Name: parent.SourceFilename.V, Filename: parents.Filename, Index: parents.AttachmentIndex,
	}
	return nil
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
		writeUnauthenticatedGraphError(w)
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
	if err := s.loadDocumentAttachments(r.Context(), bobpgx.NewPool(s.db), &result); err != nil {
		http.Error(w, "document lookup failed", http.StatusInternalServerError)
		return
	}
	if err := s.loadDocumentConfirmations(r.Context(), &result); err != nil {
		http.Error(w, "document confirmation lookup failed", http.StatusInternalServerError)
		return
	}
	if err := loadDocumentEvidence(r.Context(), bobpgx.NewPool(s.db), &result); err != nil {
		http.Error(w, "document evidence lookup failed", http.StatusInternalServerError)
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
		purchaseID := ""
		accountMovementID := ""
		if confirmation.PurchaseID.Valid {
			purchaseID = confirmation.PurchaseID.V.String()
		} else if confirmation.CorrectionID.Valid {
			correction, err := models.Corrections.Query(sm.Where(models.Corrections.Columns.ID.EQ(psql.Arg(confirmation.CorrectionID.V)))).One(ctx, bobpgx.NewPool(s.db))
			if err != nil {
				return err
			}
			purchaseID = correction.PurchaseID.String()
		} else if confirmation.AccountMovementID.Valid {
			accountMovementID = confirmation.AccountMovementID.V.String()
		}
		document.Confirmations = append(document.Confirmations, documentConfirmation{
			SourceReference: confirmation.SourceReference, PurchaseID: purchaseID, AccountMovementID: accountMovementID,
		})
	}
	return nil
}

func loadDocumentEvidence(ctx context.Context, exec bob.Executor, documents ...*documentResponse) error {
	ids := make([]bob.Expression, 0, len(documents))
	for _, document := range documents {
		id, err := gofrsuuid.FromString(document.ID)
		if err != nil {
			return err
		}
		ids = append(ids, psql.Arg(id))
		document.HasEvidence = false
	}
	if len(ids) == 0 {
		return nil
	}
	links, err := models.EvidenceLinks.Query(sm.Where(models.EvidenceLinks.Columns.SourceDocumentID.In(ids...))).All(ctx, exec)
	if err != nil {
		return err
	}
	correctionLinks, err := models.CorrectionEvidenceLinks.Query(sm.Where(models.CorrectionEvidenceLinks.Columns.SourceDocumentID.In(ids...))).All(ctx, exec)
	if err != nil {
		return err
	}
	evidenced := make(map[string]struct{}, len(links))
	for _, link := range links {
		evidenced[link.SourceDocumentID.String()] = struct{}{}
	}
	for _, link := range correctionLinks {
		evidenced[link.SourceDocumentID.String()] = struct{}{}
	}
	for _, document := range documents {
		if _, ok := evidenced[document.ID]; ok {
			document.HasEvidence = true
		}
	}
	return nil
}

func (s *Server) documents(w http.ResponseWriter, r *http.Request) {
	principal, err := s.authenticate(r)
	if err != nil {
		writeUnauthenticatedGraphError(w)
		return
	}
	householdID := r.URL.Query().Get("householdId")
	householdUUID, err := parseNodeIDOrUUID(householdID, "Household")
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
	filters, err := parseDocumentFilters(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	filters.householdID = householdUUID.String()
	var cursor *documentCursor
	if after := r.URL.Query().Get("after"); after != "" {
		cursor, err = decodeDocumentCursor(after, filters)
		if err != nil {
			http.Error(w, "invalid cursor", http.StatusBadRequest)
			return
		}
	}
	documentMods := []bob.Mod[*psqldialect.SelectQuery]{
		sm.Where(models.SourceDocuments.Columns.HouseholdID.EQ(psql.Arg(householdUUID))),
		sm.OrderBy(models.SourceDocuments.Columns.CreatedAt).Desc(),
		sm.OrderBy(models.SourceDocuments.Columns.ID).Desc(),
		sm.Limit(limit + 1),
	}
	if filters.sourceKind != "" {
		documentMods = append(documentMods, sm.Where(models.SourceDocuments.Columns.SourceKind.EQ(psql.Arg(filters.sourceKind))))
	}
	if filters.from != nil {
		documentMods = append(documentMods, sm.Where(models.SourceDocuments.Columns.CreatedAt.GTE(psql.Arg(*filters.from))))
	}
	if filters.to != nil {
		documentMods = append(documentMods, sm.Where(models.SourceDocuments.Columns.CreatedAt.LT(psql.Arg(*filters.to))))
	}
	if filters.confirmed != nil {
		confirmationFilter := documentConfirmationExists()
		if !*filters.confirmed {
			confirmationFilter = psql.Not(confirmationFilter)
		}
		documentMods = append(documentMods, sm.Where(confirmationFilter))
	}
	if filters.store != "" {
		documentMods = append(documentMods, documentMerchantExists(householdUUID, filters.store))
	}
	if cursor != nil {
		documentMods = append(documentMods, sm.Where(psql.Or(
			models.SourceDocuments.Columns.CreatedAt.LT(psql.Arg(cursor.createdAt)),
			psql.And(
				models.SourceDocuments.Columns.CreatedAt.EQ(psql.Arg(cursor.createdAt)),
				models.SourceDocuments.Columns.ID.LT(psql.Arg(cursor.id)),
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
		if err := s.loadDocumentAttachments(r.Context(), bobpgx.NewPool(s.db), &item); err != nil {
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
	documentResults := make([]*documentResponse, len(result))
	for i := range result {
		documentResults[i] = &result[i]
	}
	if err := loadDocumentEvidence(r.Context(), bobpgx.NewPool(s.db), documentResults...); err != nil {
		http.Error(w, "document evidence lookup failed", http.StatusInternalServerError)
		return
	}
	hasMore := len(result) > limit
	if hasMore {
		result = result[:limit]
	}
	var nextCursor *string
	if hasMore && len(result) > 0 {
		last := storedDocuments[limit-1]
		encoded := encodeDocumentCursor(documentCursor{id: last.ID, createdAt: last.CreatedAt}, filters)
		nextCursor = &encoded
	}
	writeJSON(w, http.StatusOK, map[string]any{"documents": result, "nextCursor": nextCursor})
}

func documentConfirmationExists() psql.Expression {
	query := psql.Select(
		sm.Columns(psql.Arg(1)),
		sm.From(models.DocumentConfirmations.NameExpr()),
		sm.Where(models.DocumentConfirmations.Columns.SourceDocumentID.EQ(models.SourceDocuments.Columns.ID)),
	)
	return psql.Exists(query)
}

func documentMerchantExists(householdID gofrsuuid.UUID, store string) bob.Mod[*psqldialect.SelectQuery] {
	pattern := psql.Arg("%" + escapeLike(store) + "%")
	receiptMerchant := psql.F("jsonb_extract_path_text", models.ExtractionJobs.Columns.Result, psql.Arg("receipt"), psql.Arg("merchant"))()
	statements := psql.Cast(psql.F("jsonb_extract_path", models.ExtractionJobs.Columns.Result, psql.Arg("statements"))(), "text")
	merchantFilter := psql.Or(
		psql.Expression{}.New(receiptMerchant).ILike(pattern),
		statements.ILike(pattern),
	)
	query := psql.Select(
		sm.Columns(psql.Arg(1)),
		sm.From(models.ExtractionJobs.NameExpr()),
		sm.Where(psql.And(
			models.ExtractionJobs.Columns.SourceDocumentID.EQ(models.SourceDocuments.Columns.ID),
			models.ExtractionJobs.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			merchantFilter,
		)),
	)
	return sm.Where(psql.Exists(query))
}

type documentFilters struct {
	householdID string
	store       string
	sourceKind  string
	from        *time.Time
	to          *time.Time
	confirmed   *bool
}

type documentCursor struct {
	id        gofrsuuid.UUID
	createdAt time.Time
}

func parseDocumentFilters(r *http.Request) (documentFilters, error) {
	query := r.URL.Query()
	filters := documentFilters{
		store:      strings.TrimSpace(query.Get("store")),
		sourceKind: query.Get("sourceKind"),
	}
	if filters.sourceKind != "" && !validSourceKind(filters.sourceKind) {
		return documentFilters{}, errors.New("invalid source kind")
	}
	for key, target := range map[string]**time.Time{"from": &filters.from, "to": &filters.to} {
		value := query.Get(key)
		if value == "" {
			continue
		}
		day, err := time.ParseInLocation("2006-01-02", value, time.FixedZone("JST", 9*60*60))
		if err != nil {
			return documentFilters{}, fmt.Errorf("invalid %s date", key)
		}
		if key == "to" {
			day = day.AddDate(0, 0, 1)
		}
		*target = &day
	}
	if filters.from != nil && filters.to != nil && !filters.from.Before(*filters.to) {
		return documentFilters{}, errors.New("invalid date range")
	}
	if value := query.Get("confirmed"); value != "" {
		confirmed := value == "true"
		if value != "true" && value != "false" {
			return documentFilters{}, errors.New("invalid confirmation filter")
		}
		filters.confirmed = &confirmed
	}
	return filters, nil
}

func encodeDocumentCursor(cursor documentCursor, filters documentFilters) string {
	return base64.RawURLEncoding.EncodeToString(fmt.Appendf(nil, "%s|%s|%x", cursor.id, cursor.createdAt.Format(time.RFC3339Nano), documentFilterHash(filters)))
}

func decodeDocumentCursor(value string, filters documentFilters) (*documentCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(string(decoded), "|")
	if len(parts) != 3 {
		return nil, errors.New("invalid cursor")
	}
	id, err := gofrsuuid.FromString(parts[0])
	if err != nil {
		return nil, err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[1])
	if err != nil {
		return nil, err
	}
	if parts[2] != fmt.Sprintf("%x", documentFilterHash(filters)) {
		return nil, errors.New("cursor filter mismatch")
	}
	return &documentCursor{id: id, createdAt: createdAt}, nil
}

func documentFilterHash(filters documentFilters) [32]byte {
	from, to, confirmed := "", "", ""
	if filters.from != nil {
		from = filters.from.Format(time.RFC3339Nano)
	}
	if filters.to != nil {
		to = filters.to.Format(time.RFC3339Nano)
	}
	if filters.confirmed != nil {
		confirmed = fmt.Sprint(*filters.confirmed)
	}
	return sha256.Sum256([]byte(strings.Join([]string{filters.householdID, filters.store, filters.sourceKind, from, to, confirmed}, "\x00")))
}

func escapeLike(value string) string {
	return strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(value)
}

func (s *Server) retryDocument(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(w, r) {
		return
	}
	principal, err := s.authenticate(r)
	if err != nil {
		writeUnauthenticatedGraphError(w)
		return
	}
	if s.cfg.Store == nil {
		http.Error(w, "document processing is unavailable", http.StatusServiceUnavailable)
		return
	}
	var input struct {
		Crop       *ingestion.Region `json:"crop,omitempty"`
		SourceKind *string           `json:"sourceKind,omitempty"`
	}
	if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, 64<<10), &input); err != nil || input.Crop != nil && !ingestion.ValidRegion(*input.Crop) || input.SourceKind != nil && !validReprocessableSourceKind(*input.SourceKind) {
		http.Error(w, "invalid extraction retry input", http.StatusBadRequest)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "extraction retry failed", http.StatusInternalServerError)
		return
	}
	defer func(ctx context.Context) { _ = tx.Rollback(ctx) }(context.WithoutCancel(r.Context()))
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
	if input.Crop != nil && document.MimeType != documentMIMEJPEG && document.MimeType != documentMIMEPNG && document.MimeType != documentMIMEWebP && document.MimeType != documentMIMEPDF {
		http.Error(w, "image crop requires an image or PDF document", http.StatusBadRequest)
		return
	}
	if input.Crop != nil && document.MimeType != documentMIMEPDF && input.Crop.Page != 0 {
		http.Error(w, "image page is out of range", http.StatusBadRequest)
		return
	}
	if input.SourceKind != nil {
		if document.MimeType == documentMIMEEmail {
			http.Error(w, "email documents cannot be reclassified", http.StatusBadRequest)
			return
		}
		attachment, err := models.SourceDocumentAttachments.Query(sm.Where(psql.And(
			models.SourceDocumentAttachments.Columns.HouseholdID.EQ(psql.Arg(document.HouseholdID)),
			models.SourceDocumentAttachments.Columns.ChildDocumentID.EQ(psql.Arg(document.ID)),
		))).Exists(r.Context(), executor)
		if err != nil {
			http.Error(w, "document attachment lookup failed", http.StatusInternalServerError)
			return
		}
		if !attachment {
			http.Error(w, "only email attachments can be reclassified", http.StatusBadRequest)
			return
		}
		if *input.SourceKind != document.SourceKind {
			confirmed, err := models.DocumentConfirmations.Query(sm.Where(models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(document.ID)))).Exists(r.Context(), executor)
			if err != nil {
				http.Error(w, "document confirmation lookup failed", http.StatusInternalServerError)
				return
			}
			evidenced, err := models.EvidenceLinks.Query(sm.Where(models.EvidenceLinks.Columns.SourceDocumentID.EQ(psql.Arg(document.ID)))).Exists(r.Context(), executor)
			if err != nil {
				http.Error(w, "document evidence lookup failed", http.StatusInternalServerError)
				return
			}
			if confirmed || evidenced {
				http.Error(w, "document with confirmed evidence cannot be reclassified", http.StatusConflict)
				return
			}
		}
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
	if latestJob != nil && latestJob.Status == "running" {
		http.Error(w, "extraction is already running", http.StatusConflict)
		return
	}
	if latestJob != nil && latestJob.Status == documentStatusQueued && (input.SourceKind == nil || input.Crop != nil) {
		http.Error(w, "extraction is already queued", http.StatusConflict)
		return
	}
	if input.SourceKind != nil && *input.SourceKind != document.SourceKind {
		if err := document.Update(r.Context(), executor, &models.SourceDocumentSetter{SourceKind: input.SourceKind}); err != nil {
			http.Error(w, "document update failed", http.StatusInternalServerError)
			return
		}
		document.SourceKind = *input.SourceKind
	}
	if latestJob != nil && latestJob.Status == documentStatusQueued {
		if err := tx.Commit(r.Context()); err != nil {
			http.Error(w, "extraction retry failed", http.StatusInternalServerError)
			return
		}
		if s.cfg.Queue != nil {
			if err := s.cfg.Queue.Publish(r.Context(), latestJob.ID.String()); err != nil {
				s.logger.WarnContext(r.Context(), "extraction job notification failed", "job_id", latestJob.ID.String(), "error", err)
			}
		}
		writeJSON(w, http.StatusAccepted, map[string]string{jobIDJSONKey: latestJob.ID.String(), "status": documentStatusQueued})
		return
	}
	request, err := json.Marshal(map[string]any{"crop": input.Crop, "purpose": ingestion.JobPurposeInteractive})
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
	status := documentStatusQueued
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
	writeJSON(w, http.StatusAccepted, map[string]string{jobIDJSONKey: jobID.String(), "status": documentStatusQueued})
}

func (s *Server) documentContent(w http.ResponseWriter, r *http.Request) {
	principal, err := s.authenticate(r)
	if err != nil {
		writeUnauthenticatedGraphError(w)
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
		writeUnauthenticatedGraphError(w)
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
	defer func(ctx context.Context) { _ = tx.Rollback(ctx) }(context.WithoutCancel(r.Context()))
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
	purchaseID, err := parseNodeIDOrUUID(input.PurchaseID, "Purchase")
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
		itemID, err := parseNodeIDOrUUID(*input.ItemID, "PurchaseItem")
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
	if err == nil && (!confirmation.PurchaseID.Valid || confirmation.PurchaseID.V != purchase.ID || confirmation.CorrectionID.Valid) {
		http.Error(w, "document is already confirmed for another purchase", http.StatusConflict)
		return
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "evidence lookup failed", http.StatusInternalServerError)
		return
	}
	purchaseIDValue := sql.Null[gofrsuuid.UUID]{V: purchase.ID, Valid: true}
	if _, err := models.DocumentConfirmations.Insert(&models.DocumentConfirmationSetter{
		SourceDocumentID: &document.ID, SourceReference: &input.SourceReference,
		HouseholdID: &document.HouseholdID, PurchaseID: &purchaseIDValue, CreatedBy: &memberID,
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
		transform := verified.Transform
		observation, err = json.Marshal(map[string]any{jobIDJSONKey: *input.ObservationJobID, "itemIndex": *input.SourceItemIndex, "item": item, "transform": transform, "transformPage": verified.TransformPage})
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
		itemIDUUID, err := parseNodeIDOrUUID(*input.ItemID, "PurchaseItem")
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
	case documentSourceKindReceipt, "order", "email", "card", "bank":
		return true
	default:
		return false
	}
}

func validReprocessableSourceKind(value string) bool {
	switch value {
	case documentSourceKindReceipt, "order", "card", "bank":
		return true
	default:
		return false
	}
}

func validDocumentMIME(value string) bool {
	switch value {
	case "image/jpeg", "image/png", "image/webp", "application/pdf", "text/plain", "text/csv", documentMIMEEmail:
		return true
	default:
		return false
	}
}

func documentMIME(filename string, data []byte) string {
	detected := mimetype.Detect(data).String()
	if !strings.HasPrefix(detected, "text/plain") {
		if validDocumentMIME(detected) {
			return detected
		}
		return ""
	}
	if !strings.HasPrefix(http.DetectContentType(data), "text/plain") {
		return ""
	}
	_, extension, _ := strings.CutLast(strings.ToLower(filename), ".")
	switch extension {
	case "csv":
		return "text/csv"
	case "eml":
		return documentMIMEEmail
	case "txt":
		return "text/plain"
	default:
		return ""
	}
}
