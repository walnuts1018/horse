package api

import (
	"encoding/json/v2"
	"net/http"
	"time"
)

func (s *Server) purchaseEvidence(w http.ResponseWriter, r *http.Request) {
	principal, err := s.authenticate(r)
	if err != nil {
		writeGraphError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	var member bool
	if err := s.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM purchases p JOIN household_members hm ON hm.household_id=p.household_id AND hm.member_id=$2 WHERE p.id=$1)`, r.PathValue("id"), principal.MemberID).Scan(&member); err != nil || !member {
		http.NotFound(w, r)
		return
	}
	rows, err := s.db.Query(r.Context(), `SELECT e.source_document_id::text,e.item_id::text,d.source_kind,d.mime_type,d.sha256,e.evidence_region,e.observation,e.created_at FROM evidence_links e JOIN source_documents d ON d.id=e.source_document_id AND d.household_id=e.household_id WHERE e.purchase_id=$1 ORDER BY e.created_at,e.source_document_id,e.item_id`, r.PathValue("id"))
	if err != nil {
		http.Error(w, "evidence lookup failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	type evidence struct {
		DocumentID  string    `json:"documentId"`
		ItemID      *string   `json:"itemId"`
		SourceKind  string    `json:"sourceKind"`
		MIMEType    string    `json:"mimeType"`
		SHA256      string    `json:"sha256"`
		Region      any       `json:"region"`
		Observation any       `json:"observation"`
		CreatedAt   time.Time `json:"createdAt"`
	}
	result := make([]evidence, 0)
	for rows.Next() {
		var entry evidence
		var region, observation []byte
		if err := rows.Scan(&entry.DocumentID, &entry.ItemID, &entry.SourceKind, &entry.MIMEType, &entry.SHA256, &region, &observation, &entry.CreatedAt); err != nil {
			http.Error(w, "evidence lookup failed", http.StatusInternalServerError)
			return
		}
		if len(region) > 0 {
			if err := json.Unmarshal(region, &entry.Region); err != nil {
				http.Error(w, "invalid evidence region", http.StatusInternalServerError)
				return
			}
		}
		if err := json.Unmarshal(observation, &entry.Observation); err != nil {
			http.Error(w, "invalid evidence observation", http.StatusInternalServerError)
			return
		}
		result = append(result, entry)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "evidence lookup failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"evidence": result})
}
