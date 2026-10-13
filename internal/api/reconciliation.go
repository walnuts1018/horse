package api

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"net/http"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (s *Server) documentMatches(w http.ResponseWriter, r *http.Request) {
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
	executor := bobpgx.NewPool(s.db)
	document, err := documentForMember(r.Context(), executor, documentID, memberID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "observation lookup failed", http.StatusInternalServerError)
		return
	}
	job, err := models.ExtractionJobs.Query(sm.Where(psql.And(
		models.ExtractionJobs.Columns.SourceDocumentID.EQ(psql.Arg(document.ID)),
		models.ExtractionJobs.Columns.Status.EQ(psql.Arg("succeeded")),
	)), sm.OrderBy(models.ExtractionJobs.Columns.Version).Desc(), sm.Limit(1)).One(r.Context(), executor)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "observation lookup failed", http.StatusInternalServerError)
		return
	}
	if !job.Result.Valid {
		http.Error(w, "invalid extraction result", http.StatusInternalServerError)
		return
	}
	payload := job.Result.V.Val
	var verified ingestion.Verification
	if err := json.Unmarshal(payload, &verified); err != nil {
		http.Error(w, "invalid extraction result", http.StatusInternalServerError)
		return
	}
	incoming := ingestion.SourceObservations(document.ID.String(), document.Sha256, document.SourceKind, verified)
	truncated := false
	if reference := r.URL.Query().Get("sourceReference"); reference != "" {
		selected := make([]ingestion.Observation, 0, 1)
		for _, observation := range incoming {
			if observation.SourceReference == reference {
				selected = append(selected, observation)
			}
		}
		incoming = selected
	}
	if len(incoming) > 100 {
		incoming = incoming[:100]
		truncated = true
	}
	// 候補検索は確定した購入へ自動統合せず、同じ家庭の直近原本との照合結果を返す。
	documents, documentsTruncated, err := recentReconciliationDocuments(r.Context(), executor, document.HouseholdID, document.ID)
	if err != nil {
		http.Error(w, "observation lookup failed", http.StatusInternalServerError)
		return
	}
	if documentsTruncated {
		truncated = true
	}
	documentIDArgs := make([]bob.Expression, 0, len(documents))
	for _, document := range documents {
		documentIDArgs = append(documentIDArgs, psql.Arg(document.ID))
	}
	var jobs models.ExtractionJobSlice
	if len(documents) > 0 {
		jobs, err = models.ExtractionJobs.Query(
			sm.Where(psql.And(
				models.ExtractionJobs.Columns.HouseholdID.EQ(psql.Arg(document.HouseholdID)),
				models.ExtractionJobs.Columns.SourceDocumentID.In(documentIDArgs...),
				models.ExtractionJobs.Columns.Status.EQ(psql.Arg("succeeded")),
			)),
			sm.OrderBy(models.ExtractionJobs.Columns.SourceDocumentID),
			sm.OrderBy(models.ExtractionJobs.Columns.Version).Desc(),
		).All(r.Context(), executor)
		if err != nil {
			http.Error(w, "observation lookup failed", http.StatusInternalServerError)
			return
		}
	}
	jobsByDocument := make(map[gofrsuuid.UUID]*models.ExtractionJob, len(documents))
	for _, extractionJob := range jobs {
		if _, ok := jobsByDocument[extractionJob.SourceDocumentID]; !ok {
			jobsByDocument[extractionJob.SourceDocumentID] = extractionJob
		}
	}
	existing := make([]ingestion.Observation, 0)
	for _, existingDocument := range documents {
		extractionJob, ok := jobsByDocument[existingDocument.ID]
		if !ok {
			continue
		}
		if !extractionJob.Result.Valid {
			http.Error(w, "invalid extraction result", http.StatusInternalServerError)
			return
		}
		verified = ingestion.Verification{}
		if err := json.Unmarshal(extractionJob.Result.V.Val, &verified); err != nil {
			http.Error(w, "invalid extraction result", http.StatusInternalServerError)
			return
		}
		existing = append(existing, ingestion.SourceObservations(existingDocument.ID.String(), existingDocument.Sha256, existingDocument.SourceKind, verified)...)
		if len(existing) > 5000 {
			existing = existing[:5000]
			truncated = true
			break
		}
	}
	type candidate struct {
		SourceReference string            `json:"sourceReference"`
		Matches         []ingestion.Match `json:"matches"`
	}
	result := make([]candidate, 0, len(incoming))
	for _, observation := range incoming {
		matches := ingestion.Reconcile(observation, existing)
		if len(matches) > 20 {
			matches = matches[:20]
			truncated = true
		}
		if matches == nil {
			matches = make([]ingestion.Match, 0)
		}
		result = append(result, candidate{SourceReference: observation.SourceReference, Matches: matches})
	}
	writeJSON(w, http.StatusOK, map[string]any{"observations": result, "truncated": truncated})
}

func recentReconciliationDocuments(ctx context.Context, executor bob.Executor, householdID, currentDocumentID gofrsuuid.UUID) (models.SourceDocumentSlice, bool, error) {
	successfulExtraction := psql.Exists(psql.Select(
		sm.Columns(psql.Arg("1")),
		sm.From(models.ExtractionJobs.NameExpr()),
		sm.Where(psql.And(
			models.ExtractionJobs.Columns.SourceDocumentID.EQ(models.SourceDocuments.Columns.ID),
			models.ExtractionJobs.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.ExtractionJobs.Columns.Status.EQ(psql.Arg("succeeded")),
		)),
	))
	documents, err := models.SourceDocuments.Query(
		sm.Where(psql.And(
			models.SourceDocuments.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			psql.Not(models.SourceDocuments.Columns.ID.EQ(psql.Arg(currentDocumentID))),
			successfulExtraction,
		)),
		sm.OrderBy(models.SourceDocuments.Columns.CreatedAt).Desc(),
		sm.OrderBy(models.SourceDocuments.Columns.ID).Desc(),
		sm.Limit(101),
	).All(ctx, executor)
	if err != nil {
		return nil, false, err
	}
	if len(documents) <= 100 {
		return documents, false, nil
	}
	return documents[:100], true, nil
}
