package api

import (
	"context"
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
		s.writeAuthenticationFailure(w, r, err)
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
	incoming, err := reconciliationObservations(r.Context(), executor, document.HouseholdID, &document.ID)
	if err != nil {
		http.Error(w, "observation lookup failed", http.StatusInternalServerError)
		return
	}
	if len(incoming) == 0 {
		hasSuccessfulExtraction, err := models.ExtractionJobs.Query(sm.Where(psql.And(
			models.ExtractionJobs.Columns.SourceDocumentID.EQ(psql.Arg(document.ID)),
			models.ExtractionJobs.Columns.HouseholdID.EQ(psql.Arg(document.HouseholdID)),
			models.ExtractionJobs.Columns.Status.EQ(psql.Arg("succeeded")),
		))).Exists(r.Context(), executor)
		if err != nil {
			http.Error(w, "observation lookup failed", http.StatusInternalServerError)
			return
		}
		if !hasSuccessfulExtraction {
			http.NotFound(w, r)
			return
		}
	}
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
	// 候補検索は確定した購入へ自動統合せず、同じ家庭の全原本に保存された観測を照合する。
	existing, err := reconciliationObservations(r.Context(), executor, document.HouseholdID, nil)
	if err != nil {
		http.Error(w, "observation lookup failed", http.StatusInternalServerError)
		return
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

func reconciliationObservations(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, documentID *gofrsuuid.UUID) ([]ingestion.Observation, error) {
	conditions := []bob.Expression{models.SourceObservations.Columns.HouseholdID.EQ(psql.Arg(householdID))}
	if documentID != nil {
		conditions = append(conditions, models.SourceObservations.Columns.SourceDocumentID.EQ(psql.Arg(*documentID)))
	}
	rows, err := models.SourceObservations.Query(sm.Where(psql.And(conditions...))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	observations := make([]ingestion.Observation, 0, len(rows))
	for _, row := range rows {
		observation := ingestion.Observation{
			DocumentID: row.SourceDocumentID.String(), SourceReference: row.SourceReference,
			Hash: row.SourceHash, SourceKind: row.SourceKind, TransactionKind: row.TransactionKind,
			OrderID: row.OrderID, TransactionID: row.TransactionID, Merchant: row.Merchant,
			Amount: row.AmountYen, CardLast4: row.CardLast4,
		}
		if row.OccurredAt.Valid {
			observation.OccurredAt = row.OccurredAt.V
		}
		observations = append(observations, observation)
	}
	return observations, nil
}
