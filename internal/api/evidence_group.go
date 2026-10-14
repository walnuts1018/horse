package api

import (
	"context"
	"database/sql"
	stdjson "encoding/json"
	"encoding/json/v2"
	"errors"
	"net/http"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type evidenceSourceInput struct {
	DocumentID       string  `json:"documentId"`
	SourceReference  string  `json:"sourceReference"`
	ItemID           *string `json:"itemId"`
	ObservationJobID *string `json:"observationJobId"`
	SourceItemIndex  *int    `json:"sourceItemIndex"`
}

func (s *Server) confirmPurchaseEvidence(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(w, r) {
		return
	}
	principal, err := s.authenticate(r)
	if err != nil {
		writeUnauthenticatedGraphError(w)
		return
	}
	var input struct {
		Sources []evidenceSourceInput `json:"sources"`
	}
	if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, 1<<20), &input); err != nil || len(input.Sources) == 0 || len(input.Sources) > 100 {
		http.Error(w, "invalid evidence sources", http.StatusBadRequest)
		return
	}
	for _, source := range input.Sources {
		if source.DocumentID == "" || len(source.SourceReference) > 256 || (source.SourceItemIndex != nil && (source.ItemID == nil || source.ObservationJobID == nil || *source.SourceItemIndex < 0)) || (source.ObservationJobID != nil && source.SourceItemIndex == nil) {
			http.Error(w, "invalid evidence source", http.StatusBadRequest)
			return
		}
	}
	purchaseID, err := parseNodeIDOrUUID(r.PathValue("id"), "Purchase")
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
		http.Error(w, "evidence confirmation failed", http.StatusInternalServerError)
		return
	}
	defer func(ctx context.Context) { _ = tx.Rollback(ctx) }(context.WithoutCancel(r.Context()))
	executor := bobpgx.NewTx(tx, nil)
	purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseID))), sm.ForUpdate()).One(r.Context(), executor)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "purchase lookup failed", http.StatusInternalServerError)
		return
	}
	member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(purchase.HouseholdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberID)),
	))).Exists(r.Context(), executor)
	if err != nil || !member {
		http.NotFound(w, r)
		return
	}
	for _, source := range input.Sources {
		if err := s.linkPurchaseEvidenceSource(r.Context(), executor, purchase, memberID, source); err != nil {
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				http.NotFound(w, r)
			case errors.Is(err, errEvidenceConflict):
				http.Error(w, "evidence source is already linked elsewhere", http.StatusConflict)
			case errors.Is(err, errEvidenceInput):
				http.Error(w, "invalid evidence source", http.StatusBadRequest)
			default:
				http.Error(w, "evidence confirmation failed", http.StatusInternalServerError)
			}
			return
		}
	}
	eventType := "evidence.group_linked"
	changedFields := types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(`["evidence"]`)}
	actorID := sql.Null[gofrsuuid.UUID]{V: memberID, Valid: true}
	if _, err := models.HouseholdEvents.Insert(&models.HouseholdEventSetter{
		HouseholdID: &purchase.HouseholdID, Type: &eventType, EntityID: &purchase.ID,
		Revision: &purchase.Revision, ChangedFields: &changedFields, ActorID: &actorID,
	}).Exec(r.Context(), executor); err != nil {
		http.Error(w, "evidence event failed", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "evidence confirmation failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"linked": true, "sourceCount": len(input.Sources)})
}

var (
	errEvidenceConflict = errors.New("evidence confirmation conflict")
	errEvidenceInput    = errors.New("invalid evidence input")
)

func (s *Server) linkPurchaseEvidenceSource(ctx context.Context, executor bob.Executor, purchase *models.Purchase, memberID gofrsuuid.UUID, input evidenceSourceInput) error {
	documentID, err := gofrsuuid.FromString(input.DocumentID)
	if err != nil {
		return errEvidenceInput
	}
	document, err := documentForMember(ctx, executor, documentID, memberID, true)
	if err != nil {
		return err
	}
	if document.HouseholdID != purchase.HouseholdID {
		return pgx.ErrNoRows
	}
	confirmation, err := models.DocumentConfirmations.Query(sm.Where(psql.And(
		models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(document.ID)),
		models.DocumentConfirmations.Columns.SourceReference.EQ(psql.Arg(input.SourceReference)),
	))).One(ctx, executor)
	if err == nil && (!confirmation.PurchaseID.Valid || confirmation.PurchaseID.V != purchase.ID || confirmation.CorrectionID.Valid) {
		return errEvidenceConflict
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if input.ItemID != nil {
		itemID, err := parseNodeIDOrUUID(*input.ItemID, "PurchaseItem")
		if err != nil {
			return errEvidenceInput
		}
		exists, err := models.PurchaseItems.Query(sm.Where(psql.And(
			models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemID)),
			models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchase.ID)),
		))).Exists(ctx, executor)
		if err != nil {
			return err
		}
		if !exists {
			return pgx.ErrNoRows
		}
	}
	purchaseID := sql.Null[gofrsuuid.UUID]{V: purchase.ID, Valid: true}
	if _, err := models.DocumentConfirmations.Insert(&models.DocumentConfirmationSetter{
		SourceDocumentID: &document.ID, SourceReference: &input.SourceReference,
		HouseholdID: &purchase.HouseholdID, PurchaseID: &purchaseID, CreatedBy: &memberID,
	}, im.OnConflict("source_document_id", "source_reference").DoNothing()).Exec(ctx, executor); err != nil {
		return err
	}
	var itemID sql.Null[gofrsuuid.UUID]
	if input.ItemID != nil {
		parsed, err := parseNodeIDOrUUID(*input.ItemID, "PurchaseItem")
		if err != nil {
			return errEvidenceInput
		}
		itemID = sql.Null[gofrsuuid.UUID]{V: parsed, Valid: true}
	}
	var region sql.Null[types.JSON[stdjson.RawMessage]]
	observation := []byte(`{}`)
	if input.SourceItemIndex != nil {
		jobID, err := gofrsuuid.FromString(*input.ObservationJobID)
		if err != nil {
			return errEvidenceInput
		}
		job, err := models.ExtractionJobs.Query(sm.Where(psql.And(
			models.ExtractionJobs.Columns.ID.EQ(psql.Arg(jobID)),
			models.ExtractionJobs.Columns.SourceDocumentID.EQ(psql.Arg(document.ID)),
			models.ExtractionJobs.Columns.HouseholdID.EQ(psql.Arg(purchase.HouseholdID)),
			models.ExtractionJobs.Columns.Status.EQ(psql.Arg("succeeded")),
		))).One(ctx, executor)
		if err != nil || !job.Result.Valid {
			return pgx.ErrNoRows
		}
		var verified ingestion.Verification
		if err := json.Unmarshal(job.Result.V.Val, &verified); err != nil || *input.SourceItemIndex >= len(verified.Receipt.Items) {
			return errEvidenceInput
		}
		item := verified.Receipt.Items[*input.SourceItemIndex]
		observation, err = json.Marshal(map[string]any{
			jobIDJSONKey: input.ObservationJobID, "itemIndex": *input.SourceItemIndex,
			"item": item, "transform": verified.Transform, "transformPage": verified.TransformPage,
		})
		if err != nil {
			return err
		}
		if item.Region != nil {
			encoded, err := json.Marshal(item.Region)
			if err != nil {
				return err
			}
			region = sql.Null[types.JSON[stdjson.RawMessage]]{
				V: types.JSON[stdjson.RawMessage]{Val: encoded}, Valid: true,
			}
		}
	}
	observationJSON := types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(observation)}
	_, err = psql.Insert(
		im.Into(psql.Quote("evidence_links"), "household_id", "source_document_id", "purchase_id", "item_id", "evidence_region", "observation"),
		im.Values(psql.Arg(purchase.HouseholdID, document.ID, purchase.ID, itemID, region, observationJSON)),
		im.OnConflict().DoNothing(),
	).Exec(ctx, executor)
	return err
}
