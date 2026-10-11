package api

import (
	"encoding/json/v2"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/walnuts1018/horse/internal/ingestion"
)

func (s *Server) documentMatches(w http.ResponseWriter, r *http.Request) {
	principal, err := s.authenticate(r)
	if err != nil {
		writeGraphError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	var householdID, hash, kind string
	var payload []byte
	err = s.db.QueryRow(r.Context(), `SELECT d.household_id::text,d.sha256,d.source_kind,j.result FROM source_documents d JOIN household_members hm ON hm.household_id=d.household_id AND hm.member_id=$2 JOIN LATERAL (SELECT result FROM extraction_jobs WHERE source_document_id=d.id AND status='succeeded' ORDER BY version DESC LIMIT 1) j ON true WHERE d.id=$1`, r.PathValue("id"), principal.MemberID).Scan(&householdID, &hash, &kind, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "observation lookup failed", http.StatusInternalServerError)
		return
	}
	var verified ingestion.Verification
	if err := json.Unmarshal(payload, &verified); err != nil {
		http.Error(w, "invalid extraction result", http.StatusInternalServerError)
		return
	}
	incoming := ingestion.SourceObservations(r.PathValue("id"), hash, kind, verified)
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
	rows, err := s.db.Query(r.Context(), `SELECT d.id::text,d.sha256,d.source_kind,j.result FROM source_documents d JOIN LATERAL (SELECT result FROM extraction_jobs WHERE source_document_id=d.id AND status='succeeded' ORDER BY version DESC LIMIT 1) j ON true WHERE d.household_id=$1 AND d.id<>$2 ORDER BY d.created_at DESC,d.id DESC LIMIT 100`, householdID, r.PathValue("id"))
	if err != nil {
		http.Error(w, "observation lookup failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	existing := make([]ingestion.Observation, 0)
	for rows.Next() {
		var documentID string
		if err := rows.Scan(&documentID, &hash, &kind, &payload); err != nil {
			http.Error(w, "observation lookup failed", http.StatusInternalServerError)
			return
		}
		verified = ingestion.Verification{}
		if err := json.Unmarshal(payload, &verified); err != nil {
			http.Error(w, "invalid extraction result", http.StatusInternalServerError)
			return
		}
		existing = append(existing, ingestion.SourceObservations(documentID, hash, kind, verified)...)
		if len(existing) > 5000 {
			existing = existing[:5000]
			truncated = true
			break
		}
	}
	if err := rows.Err(); err != nil {
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
