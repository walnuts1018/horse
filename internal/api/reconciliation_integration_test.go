package api

import (
	"os"
	"testing"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestRecentReconciliationDocumentsFiltersAndLimitsInDatabase(t *testing.T) {
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
	executor := bobpgx.NewPool(db)
	householdID, memberID := newReconciliationUUID(t), newReconciliationUUID(t)
	name, subject := "reconciliation window test", "reconciliation-window:"+memberID.String()
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

	baseTime := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	insertDocument := func(createdAt time.Time, status string) gofrsuuid.UUID {
		t.Helper()
		documentID, jobID := newReconciliationUUID(t), newReconciliationUUID(t)
		digest, objectKey, mimeType, sourceKind := "reconciliation:"+documentID.String(), "reconciliation/"+documentID.String(), documentMIMEJPEG, "receipt"
		if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{ID: &documentID, HouseholdID: &householdID, Sha256: &digest, ObjectKey: &objectKey, MimeType: &mimeType, SourceKind: &sourceKind, CreatedBy: &memberID, CreatedAt: &createdAt}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
		if _, err := models.ExtractionJobs.Insert(&models.ExtractionJobSetter{ID: &jobID, HouseholdID: &householdID, SourceDocumentID: &documentID, Status: &status}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
		return documentID
	}

	eligible := make([]gofrsuuid.UUID, 0, 103)
	for i := range 103 {
		eligible = append(eligible, insertDocument(baseTime.Add(time.Duration(i)*time.Minute), "succeeded"))
	}
	for i := range 2 {
		insertDocument(baseTime.Add(time.Duration(200+i)*time.Minute), "failed")
	}
	currentDocumentID := insertDocument(baseTime.Add(300*time.Minute), "succeeded")

	documents, truncated, err := recentReconciliationDocuments(ctx, executor, householdID, currentDocumentID)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated {
		t.Fatal("expected truncation when more than 100 successful source documents exist")
	}
	if len(documents) != 100 {
		t.Fatalf("got %d candidate documents, want 100", len(documents))
	}
	for i, document := range documents {
		want := eligible[len(eligible)-1-i]
		if document.ID != want {
			t.Fatalf("candidate %d is %s, want newest eligible document %s", i, document.ID, want)
		}
	}
}

func newReconciliationUUID(t *testing.T) gofrsuuid.UUID {
	t.Helper()
	return gofrsuuid.Must(gofrsuuid.NewV7())
}
