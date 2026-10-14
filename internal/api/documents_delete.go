package api

import (
	"context"
	"database/sql"
	stdjson "encoding/json"
	"errors"
	"net/http"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type documentDeleter interface {
	Delete(context.Context, string) error
}

const documentDeletionPendingKey = "deletionPending"

func (s *Server) deleteDocument(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(w, r) {

		return
	}
	principal, err := s.authenticate(r)
	if err != nil {
		writeUnauthenticatedGraphError(w)

		return
	}
	store, ok := s.cfg.Store.(documentDeleter)
	if !ok {
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
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "document deletion failed", http.StatusInternalServerError)

		return
	}
	defer func(ctx context.Context) { _ = tx.Rollback(ctx) }(context.WithoutCancel(r.Context()))
	executor := bobpgx.NewTx(tx, nil)
	document, err := documentForDeletionMember(r.Context(), executor, documentID, memberID)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)

		return
	}
	if err != nil {
		http.Error(w, "document lookup failed", http.StatusInternalServerError)

		return
	}
	pendingDeletion := documentDeletionPending(document)
	if err := validateDocumentDeletion(r.Context(), executor, document); err != nil {
		if statusError, ok := errors.AsType[documentDeletionStatusError](err); ok {
			http.Error(w, statusError.Error(), http.StatusConflict)

			return
		}
		http.Error(w, "document deletion validation failed", http.StatusInternalServerError)

		return
	}
	if !pendingDeletion {
		metadata := make(map[string]any)
		if err := stdjson.Unmarshal(document.Metadata.Val, &metadata); err != nil {
			http.Error(w, "document metadata lookup failed", http.StatusInternalServerError)

			return
		}
		metadata[documentDeletionPendingKey] = true
		metadataJSON, err := stdjson.Marshal(metadata)
		if err != nil {
			http.Error(w, "document metadata update failed", http.StatusInternalServerError)

			return
		}
		metadataValue := types.JSON[stdjson.RawMessage]{Val: metadataJSON}
		if _, err := models.SourceDocuments.Update(
			(models.SourceDocumentSetter{Metadata: &metadataValue}).UpdateMod(),
			um.Where(models.SourceDocuments.Columns.ID.EQ(psql.Arg(document.ID))),
		).Exec(r.Context(), executor); err != nil {
			http.Error(w, "document metadata update failed", http.StatusInternalServerError)

			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			http.Error(w, "document metadata update failed", http.StatusInternalServerError)

			return
		}
	} else if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "document lookup failed", http.StatusInternalServerError)

		return
	}
	if document.AnalysisObjectKey.Valid {
		if err := store.Delete(r.Context(), document.AnalysisObjectKey.V); err != nil {
			s.logger.ErrorContext(r.Context(), "analysis image deletion failed", "error", err)
			http.Error(w, "document deletion failed", http.StatusBadGateway)

			return
		}
	}
	if err := store.Delete(r.Context(), document.ObjectKey); err != nil {
		s.logger.ErrorContext(r.Context(), "document deletion failed", "error", err)
		http.Error(w, "document deletion failed", http.StatusBadGateway)

		return
	}
	finalTx, err := s.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "document registration deletion failed", http.StatusInternalServerError)

		return
	}
	defer func(ctx context.Context) { _ = finalTx.Rollback(ctx) }(context.WithoutCancel(r.Context()))
	finalExecutor := bobpgx.NewTx(finalTx, nil)
	finalDocument, err := models.SourceDocuments.Query(
		sm.Where(models.SourceDocuments.Columns.ID.EQ(psql.Arg(document.ID))),
		sm.ForUpdate(),
	).One(r.Context(), finalExecutor)
	if errors.Is(err, sql.ErrNoRows) {
		w.WriteHeader(http.StatusNoContent)

		return
	}
	if err != nil {
		http.Error(w, "document lookup failed", http.StatusInternalServerError)

		return
	}
	if !documentDeletionPending(finalDocument) {
		http.Error(w, "document deletion is not pending", http.StatusConflict)

		return
	}
	if _, err := models.SourceDocuments.Delete(dm.Where(models.SourceDocuments.Columns.ID.EQ(psql.Arg(document.ID)))).Exec(r.Context(), finalExecutor); err != nil {
		http.Error(w, "document registration deletion failed", http.StatusInternalServerError)

		return
	}
	if err := finalTx.Commit(r.Context()); err != nil {
		http.Error(w, "document registration deletion failed", http.StatusInternalServerError)

		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type documentDeletionStatusError struct {
	message string
}

func (e documentDeletionStatusError) Error() string {
	return e.message
}

func validateDocumentDeletion(ctx context.Context, executor bob.Executor, document *models.SourceDocument) error {
	job, err := models.ExtractionJobs.Query(
		sm.Where(models.ExtractionJobs.Columns.SourceDocumentID.EQ(psql.Arg(document.ID))),
		sm.OrderBy(models.ExtractionJobs.Columns.Version).Desc(),
		sm.Limit(1),
		sm.ForUpdate(),
	).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) || err == nil && job.Status != "succeeded" && job.Status != "failed" {
		return documentDeletionStatusError{message: "only completed, unconfirmed documents can be deleted"}
	}
	if err != nil {
		return err
	}
	confirmed, err := models.DocumentConfirmations.Query(
		sm.Where(models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(document.ID))),
	).Exists(ctx, executor)
	if err != nil {
		return err
	}
	evidenced, err := models.EvidenceLinks.Query(
		sm.Where(models.EvidenceLinks.Columns.SourceDocumentID.EQ(psql.Arg(document.ID))),
	).Exists(ctx, executor)
	if err != nil {
		return err
	}
	correctionEvidence, err := models.CorrectionEvidenceLinks.Query(
		sm.Where(models.CorrectionEvidenceLinks.Columns.SourceDocumentID.EQ(psql.Arg(document.ID))),
	).Exists(ctx, executor)
	if err != nil {
		return err
	}
	attachments, err := models.SourceDocumentAttachments.Query(sm.Where(psql.Or(
		models.SourceDocumentAttachments.Columns.ParentDocumentID.EQ(psql.Arg(document.ID)),
		models.SourceDocumentAttachments.Columns.ChildDocumentID.EQ(psql.Arg(document.ID)),
	))).Exists(ctx, executor)
	if err != nil {
		return err
	}
	if confirmed || evidenced || correctionEvidence || attachments {
		return documentDeletionStatusError{message: "documents with confirmed evidence or attachments cannot be deleted"}
	}

	return nil
}

func documentDeletionPending(document *models.SourceDocument) bool {
	var metadata map[string]stdjson.RawMessage
	if err := stdjson.Unmarshal(document.Metadata.Val, &metadata); err != nil {
		return false
	}
	var pending bool

	return stdjson.Unmarshal(metadata[documentDeletionPendingKey], &pending) == nil && pending
}

func documentForDeletionMember(ctx context.Context, executor bob.Executor, documentID, memberID gofrsuuid.UUID) (*models.SourceDocument, error) {
	document, err := models.SourceDocuments.Query(
		sm.Where(models.SourceDocuments.Columns.ID.EQ(psql.Arg(documentID))),
		sm.ForUpdate(),
	).One(ctx, executor)
	if err != nil {
		return nil, err
	}
	member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(document.HouseholdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberID)),
	))).Exists(ctx, executor)
	if err != nil {
		return nil, err
	}
	if !member {
		return nil, pgx.ErrNoRows
	}

	return document, nil
}
