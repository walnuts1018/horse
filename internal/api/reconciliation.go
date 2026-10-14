package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dialect"
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
	query := r.URL.Query()
	_, hasSourceReference := query["sourceReference"]
	incoming, err := reconciliationObservations(r.Context(), executor, document.HouseholdID, reconciliationObservationQuery{
		documentID:      &document.ID,
		sourceReference: query.Get("sourceReference"),
		hasSourceRef:    hasSourceReference,
		limit:           reconciliationObservationLimit + 1,
	})
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
	truncated := len(incoming) > reconciliationObservationLimit
	if truncated {
		incoming = incoming[:reconciliationObservationLimit]
	}
	if len(incoming) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"observations": []any{}, "truncated": truncated})

		return
	}
	// 候補検索は確定した購入へ自動統合せず、同じ家庭の全原本に保存された観測を照合する。
	existing, err := reconciliationObservations(r.Context(), executor, document.HouseholdID, reconciliationObservationQuery{
		candidates: incoming,
		limit:      reconciliationCandidateLimit + 1,
	})
	if err != nil {
		http.Error(w, "observation lookup failed", http.StatusInternalServerError)
		return
	}
	if len(existing) > reconciliationCandidateLimit {
		existing = existing[:reconciliationCandidateLimit]
		truncated = true
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

const reconciliationObservationLimit = 100
const reconciliationCandidateLimit = 5000

type reconciliationObservationQuery struct {
	documentID      *gofrsuuid.UUID
	sourceReference string
	hasSourceRef    bool
	limit           int
	candidates      []ingestion.Observation
}

func reconciliationObservations(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, query reconciliationObservationQuery) ([]ingestion.Observation, error) {
	conditions := []bob.Expression{models.SourceObservations.Columns.HouseholdID.EQ(psql.Arg(householdID))}
	if query.documentID != nil {
		conditions = append(conditions, models.SourceObservations.Columns.SourceDocumentID.EQ(psql.Arg(*query.documentID)))
	}
	if query.hasSourceRef {
		conditions = append(conditions, models.SourceObservations.Columns.SourceReference.EQ(psql.Arg(query.sourceReference)))
	}
	if len(query.candidates) > 0 {
		filters := make([]bob.Expression, 0, len(query.candidates)*4)
		for _, candidate := range query.candidates {
			filters = append(filters, reconciliationCandidateCondition(candidate)...)
		}
		if len(filters) == 0 {
			return []ingestion.Observation{}, nil
		}
		conditions = append(conditions, psql.Or(filters...))
	}
	mods := []bob.Mod[*dialect.SelectQuery]{
		sm.Where(psql.And(conditions...)),
		sm.OrderBy(models.SourceObservations.Columns.SourceDocumentID),
		sm.OrderBy(models.SourceObservations.Columns.SourceReference),
	}
	if query.limit > 0 {
		mods = append(mods, sm.Limit(query.limit))
	}
	rows, err := models.SourceObservations.Query(mods...).All(ctx, executor)
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

func reconciliationCandidateCondition(candidate ingestion.Observation) []bob.Expression {
	columns := models.SourceObservations.Columns
	conditions := make([]bob.Expression, 0, 4)
	if candidate.Hash != "" {
		conditions = append(conditions, columns.SourceHash.EQ(psql.Arg(candidate.Hash)))
	}
	if candidate.TransactionID != "" {
		conditions = append(conditions, columns.TransactionID.EQ(psql.Arg(candidate.TransactionID)))
	}
	if candidate.OrderID != "" {
		conditions = append(conditions, columns.OrderID.EQ(psql.Arg(candidate.OrderID)))
	}
	if strings.TrimSpace(candidate.Merchant) != "" && !candidate.OccurredAt.IsZero() {
		occurredAt := columns.OccurredAt
		amountConditions := []bob.Expression{columns.AmountYen.EQ(psql.Arg(candidate.Amount))}
		if candidate.Amount != -1<<63 && candidate.Amount != 0 {
			amountConditions = append(amountConditions, columns.AmountYen.EQ(psql.Arg(-candidate.Amount)))
		}
		conditions = append(conditions, psql.And(
			psql.Or(amountConditions...),
			occurredAt.GTE(psql.Arg(candidate.OccurredAt.Add(-72*time.Hour))),
			occurredAt.LTE(psql.Arg(candidate.OccurredAt.Add(72*time.Hour))),
		))
	}

	return conditions
}
