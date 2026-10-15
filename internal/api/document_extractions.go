package api

import (
	"database/sql"
	stdjson "encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type documentExtractionVersion struct {
	ID                string    `json:"id"`
	Version           int32     `json:"version"`
	Model             string    `json:"model"`
	CreatedAt         time.Time `json:"createdAt"`
	ConversationTurns int       `json:"conversationTurns"`
}

func (s *Server) documentExtractions(w http.ResponseWriter, r *http.Request) {
	principal, err := s.authenticate(r)
	if err != nil {
		s.writeAuthenticationFailure(w, r, err)
		return
	}
	documentID, err := uuid.FromString(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	memberID, err := uuid.FromString(principal.MemberID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	executor := bobpgx.NewPool(s.db)
	document, err := documentForMember(r.Context(), executor, documentID, memberID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.logger.ErrorContext(r.Context(), "document extraction history lookup failed", "error", err)
		http.Error(w, "document extraction history lookup failed", http.StatusInternalServerError)
		return
	}
	jobs, err := models.ExtractionJobs.Query(
		sm.Where(psql.And(
			models.ExtractionJobs.Columns.HouseholdID.EQ(psql.Arg(document.HouseholdID)),
			models.ExtractionJobs.Columns.SourceDocumentID.EQ(psql.Arg(document.ID)),
			models.ExtractionJobs.Columns.Status.EQ(psql.Arg("succeeded")),
		)),
		sm.OrderBy(models.ExtractionJobs.Columns.Version).Desc(),
	).All(r.Context(), executor)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "document extraction history query failed", "error", err)
		http.Error(w, "document extraction history lookup failed", http.StatusInternalServerError)
		return
	}
	versions := make([]documentExtractionVersion, 0, len(jobs))
	for _, job := range jobs {
		if !job.Result.Valid {
			continue
		}
		var result ingestion.Verification
		if err := stdjson.Unmarshal(job.Result.V.Val, &result); err != nil || len(result.Receipt.Items) == 0 && len(result.Statements) == 0 {
			continue
		}
		versions = append(versions, documentExtractionVersion{
			ID: job.ID.String(), Version: job.Version, Model: extractionJobModel(job), CreatedAt: job.CreatedAt,
			ConversationTurns: len(result.Conversation) / 2,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

func (s *Server) documentExtraction(w http.ResponseWriter, r *http.Request) {
	principal, err := s.authenticate(r)
	if err != nil {
		s.writeAuthenticationFailure(w, r, err)
		return
	}
	documentID, err := uuid.FromString(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	memberID, err := uuid.FromString(principal.MemberID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	executor := bobpgx.NewPool(s.db)
	document, err := documentForMember(r.Context(), executor, documentID, memberID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.logger.ErrorContext(r.Context(), "document extraction lookup failed", "error", err)
		http.Error(w, "document extraction lookup failed", http.StatusInternalServerError)
		return
	}
	jobID, err := uuid.FromString(r.PathValue("versionId"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	job, err := models.ExtractionJobs.Query(sm.Where(psql.And(
		models.ExtractionJobs.Columns.ID.EQ(psql.Arg(jobID)),
		models.ExtractionJobs.Columns.HouseholdID.EQ(psql.Arg(document.HouseholdID)),
		models.ExtractionJobs.Columns.SourceDocumentID.EQ(psql.Arg(document.ID)),
		models.ExtractionJobs.Columns.Status.EQ(psql.Arg("succeeded")),
	))).One(r.Context(), executor)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.logger.ErrorContext(r.Context(), "document extraction version lookup failed", "error", err)
		http.Error(w, "document extraction lookup failed", http.StatusInternalServerError)
		return
	}
	if !job.Result.Valid {
		http.NotFound(w, r)
		return
	}
	var result ingestion.Verification
	if err := stdjson.Unmarshal(job.Result.V.Val, &result); err != nil {
		http.Error(w, "document extraction result is unavailable", http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": job.ID.String(), "version": job.Version, "model": extractionJobModel(job), "createdAt": job.CreatedAt,
		"conversationTurns": len(result.Conversation) / 2, "result": result,
	})
}
