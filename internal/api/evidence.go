package api

import (
	"net/http"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (s *Server) purchaseEvidence(w http.ResponseWriter, r *http.Request) {
	principal, err := s.authenticate(r)
	if err != nil {
		writeGraphError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	purchaseID, err := uuid.FromString(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseID)))).One(r.Context(), bobpgx.NewPool(s.db))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	memberID, err := uuid.FromString(principal.MemberID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(purchase.HouseholdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberID)),
	))).Exists(r.Context(), bobpgx.NewPool(s.db))
	if err != nil || !member {
		http.NotFound(w, r)
		return
	}
	evidenceLinks, err := models.EvidenceLinks.Query(
		sm.Where(models.EvidenceLinks.Columns.PurchaseID.EQ(psql.Arg(purchaseID))),
		sm.OrderBy(models.EvidenceLinks.Columns.CreatedAt),
		sm.OrderBy(models.EvidenceLinks.Columns.SourceDocumentID),
		sm.OrderBy(models.EvidenceLinks.Columns.ItemID),
	).All(r.Context(), bobpgx.NewPool(s.db))
	if err != nil {
		http.Error(w, "evidence lookup failed", http.StatusInternalServerError)
		return
	}
	if err := evidenceLinks.LoadSourceDocument(r.Context(), bobpgx.NewPool(s.db)); err != nil {
		http.Error(w, "evidence lookup failed", http.StatusInternalServerError)
		return
	}
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
	for _, link := range evidenceLinks {
		document := link.R.SourceDocument
		if document == nil {
			http.Error(w, "evidence lookup failed", http.StatusInternalServerError)
			return
		}
		entry := evidence{
			DocumentID: document.ID.String(), SourceKind: document.SourceKind,
			MIMEType: document.MimeType, SHA256: document.Sha256,
			Observation: link.Observation.Val, CreatedAt: link.CreatedAt,
		}
		if link.ItemID.Valid {
			id := link.ItemID.V.String()
			entry.ItemID = &id
		}
		if link.EvidenceRegion.Valid {
			entry.Region = link.EvidenceRegion.V.Val
		}
		result = append(result, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"evidence": result})
}
