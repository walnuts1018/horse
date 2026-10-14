package api

import (
	"database/sql"
	"os"
	"strconv"
	"testing"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dialect"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestReconciliationObservationsUseStableBoundedQueries(t *testing.T) {
	t.Parallel()

	databaseURL := os.Getenv("HORSE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("HORSE_TEST_DATABASE_URL is not set")
	}
	ctx := t.Context()
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	executor := pgx.NewPool(db)
	householdID, memberID := newReconciliationUUID(t), newReconciliationUUID(t)
	name, subject := "reconciliation observation test", "reconciliation-observations:"+memberID.String()
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberID, OidcSubject: &subject, DisplayName: &subject}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberID)))).Exec(t.Context(), executor)
	}()
	if _, err := models.Households.Insert(&models.HouseholdSetter{ID: &householdID, Name: &name}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).Exec(t.Context(), executor)
	}()
	otherHouseholdID := newReconciliationUUID(t)
	otherHouseholdName := "other reconciliation household"
	if _, err := models.Households.Insert(&models.HouseholdSetter{ID: &otherHouseholdID, Name: &otherHouseholdName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(otherHouseholdID)))).Exec(t.Context(), executor)
	}()

	insertDocument := func(owner gofrsuuid.UUID, digest string) gofrsuuid.UUID {
		t.Helper()
		documentID := newReconciliationUUID(t)
		objectKey, mimeType, sourceKind := "reconciliation/"+documentID.String(), documentMIMEJPEG, "receipt"
		if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{ID: &documentID, HouseholdID: &owner, Sha256: &digest, ObjectKey: &objectKey, MimeType: &mimeType, SourceKind: &sourceKind, CreatedBy: &memberID}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
		return documentID
	}

	const observationsPerDocument = 50
	const priorDocumentCount = 101
	priorDocuments := make([]gofrsuuid.UUID, 0, priorDocumentCount)
	values := make([]bob.Mod[*dialect.InsertQuery], 0, priorDocumentCount*observationsPerDocument+126)
	occurredAt := sql.Null[time.Time]{V: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), Valid: true}
	for documentIndex := range priorDocumentCount {
		digest := "prior-" + strconv.Itoa(documentIndex)
		documentID := insertDocument(householdID, digest)
		priorDocuments = append(priorDocuments, documentID)
		for observationIndex := range observationsPerDocument {
			values = append(values, im.Values(psql.Arg(
				householdID, documentID, "row:"+strconv.Itoa(observationIndex+1), digest, "card", "PAYMENT",
				"", "", "店舗", occurredAt, int64(1000+observationIndex), "",
			)))
		}
	}
	currentDigest := "current-reconciliation-document"
	currentDocumentID := insertDocument(householdID, currentDigest)
	for observationIndex := range 125 {
		values = append(values, im.Values(psql.Arg(
			householdID, currentDocumentID, "row:"+strconv.Itoa(observationIndex+1), currentDigest, "card", "PAYMENT",
			"", "", "店舗", occurredAt, int64(2000+observationIndex), "",
		)))
	}
	otherDigest := "other-household-reconciliation-document"
	otherDocumentID := insertDocument(otherHouseholdID, otherDigest)
	values = append(values, im.Values(psql.Arg(
		otherHouseholdID, otherDocumentID, "row:1", otherDigest, "card", "PAYMENT",
		"", "", "店舗", occurredAt, int64(1000), "",
	)))
	mods := []bob.Mod[*dialect.InsertQuery]{im.Into(models.SourceObservations.NameAsExpr(),
		"household_id", "source_document_id", "source_reference", "source_hash", "source_kind",
		"transaction_kind", "order_id", "transaction_id", "merchant", "occurred_at", "amount_yen", "card_last4",
	)}
	mods = append(mods, values...)
	_, err = psql.Insert(mods...).Exec(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}

	all, err := reconciliationObservations(ctx, executor, householdID, reconciliationObservationQuery{limit: 101})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 101 {
		t.Fatalf("got %d household observations, want the stable page of 101", len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].DocumentID > all[i].DocumentID || (all[i-1].DocumentID == all[i].DocumentID && all[i-1].SourceReference > all[i].SourceReference) {
			t.Fatalf("observations are not in stable document/reference order at index %d", i)
		}
	}
	current, err := reconciliationObservations(ctx, executor, householdID, reconciliationObservationQuery{documentID: &currentDocumentID, limit: 101})
	if err != nil {
		t.Fatal(err)
	}
	if len(current) != 101 {
		t.Fatalf("got %d observations for the current document, want the stable page of 101", len(current))
	}
	selected, err := reconciliationObservations(ctx, executor, householdID, reconciliationObservationQuery{
		documentID: &currentDocumentID, sourceReference: "row:123", hasSourceRef: true,
		limit: 101,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0].SourceReference != "row:123" {
		t.Fatalf("sourceReference query returned %#v, want row:123", selected)
	}
	candidates, err := reconciliationObservations(ctx, executor, householdID, reconciliationObservationQuery{
		candidates: []ingestion.Observation{{Hash: "prior-7"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != observationsPerDocument {
		t.Fatalf("candidate query returned %d observations, want %d for one matching source hash", len(candidates), observationsPerDocument)
	}
	boundedMatches := make([]ingestion.Observation, 0, priorDocumentCount)
	for documentIndex := range priorDocumentCount {
		boundedMatches = append(boundedMatches, ingestion.Observation{Hash: "prior-" + strconv.Itoa(documentIndex)})
	}
	boundedCandidates, err := reconciliationObservations(ctx, executor, householdID, reconciliationObservationQuery{
		candidates: boundedMatches,
		limit:      reconciliationCandidateLimit + 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(boundedCandidates) != reconciliationCandidateLimit+1 {
		t.Fatalf("candidate query returned %d observations, want the bounded page of %d", len(boundedCandidates), reconciliationCandidateLimit+1)
	}
	amountCandidates, err := reconciliationObservations(ctx, executor, householdID, reconciliationObservationQuery{
		candidates: []ingestion.Observation{{Merchant: "店舗", OccurredAt: occurredAt.V, Amount: 1000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(amountCandidates) != priorDocumentCount {
		t.Fatalf("amount/date candidate query returned %d observations, want %d", len(amountCandidates), priorDocumentCount)
	}
	for _, observation := range all {
		if observation.Hash == otherDigest || observation.DocumentID == otherDocumentID.String() {
			t.Fatal("reconciliation observations included another household")
		}
	}
	if len(priorDocuments) != priorDocumentCount {
		t.Fatalf("test setup created %d prior documents, want %d", len(priorDocuments), priorDocumentCount)
	}
}

func newReconciliationUUID(t *testing.T) gofrsuuid.UUID {
	t.Helper()
	return gofrsuuid.Must(gofrsuuid.NewV7())
}
