package migration

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
	"github.com/walnuts1018/horse/internal/testinfra"
)

func TestSourceObservationsMigrationBackfillsLatestSuccessfulExtraction(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	databaseURL, terminate, err := testinfra.StartPostgres(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := terminate(cleanupCtx); err != nil {
			t.Errorf("terminate PostgreSQL test container: %v", err)
		}
	})
	if err := applyMigrationsThroughVersion(ctx, databaseURL, "20261014250000"); err != nil {
		t.Fatal(err)
	}
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	executor := bobpgx.NewPool(db)
	householdID, memberID, documentID := migrationUUID(t), migrationUUID(t), migrationUUID(t)
	memberSubject, householdName := "source-observation-backfill:"+memberID.String(), "Source observation backfill"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberID, OidcSubject: &memberSubject, DisplayName: &memberSubject}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	if _, err := models.Households.Insert(&models.HouseholdSetter{ID: &householdID, Name: &householdName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	digest, objectKey, mimeType, sourceKind := "legacy-observation-hash", "legacy-observation-object", "application/pdf", "card"
	if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{
		ID: &documentID, HouseholdID: &householdID, Sha256: &digest, ObjectKey: &objectKey,
		MimeType: &mimeType, SourceKind: &sourceKind, CreatedBy: &memberID,
	}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	oldResult := types.JSON[json.RawMessage]{Val: json.RawMessage(`{"receipt":{"total":100,"items":[{}]},"statements":[]}`)}
	latestResult := types.JSON[json.RawMessage]{Val: json.RawMessage(`{"receipt":{"merchant":"店舗","purchasedAt":"2026-10-10T10:00:00Z","total":1200,"orderId":"order-2","transactionId":"transaction-2","items":[{}],"payments":[{"cardLast4":"1234"}]},"statements":[{"row":2,"sourceReference":"","kind":"PURCHASE_PAYMENT","occurredAt":"2026-10-10T10:00:00Z","merchant":"カード店舗","amount":-1200,"transactionId":"transaction-2","orderId":"order-2","cardLast4":"1234"}]}`)}
	for version, result := range []types.JSON[json.RawMessage]{oldResult, latestResult} {
		jobID, status := migrationUUID(t), "succeeded"
		versionValue := int32(version + 1)
		resultValue := sql.Null[types.JSON[json.RawMessage]]{V: result, Valid: true}
		if _, err := models.ExtractionJobs.Insert(&models.ExtractionJobSetter{
			ID: &jobID, HouseholdID: &householdID, SourceDocumentID: &documentID,
			Version: &versionValue, Status: &status, Result: &resultValue,
		}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
	}
	if err := testinfra.ApplyMigrations(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	observations, err := models.SourceObservations.Query(sm.Where(
		models.SourceObservations.Columns.SourceDocumentID.EQ(psql.Arg(documentID)),
	)).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 2 {
		t.Fatalf("backfill persisted %d source observations, want 2", len(observations))
	}
	byReference := make(map[string]*models.SourceObservation, len(observations))
	for _, observation := range observations {
		byReference[observation.SourceReference] = observation
	}
	receipt, ok := byReference[""]
	if !ok || receipt.TransactionKind != "RECEIPT_PURCHASE" || receipt.AmountYen != 1200 || receipt.SourceHash != digest || receipt.CardLast4 != "1234" || receipt.OrderID != "order-2" {
		t.Fatalf("backfilled receipt observation is incorrect: %+v", receipt)
	}
	statement, ok := byReference["row:2"]
	if !ok || statement.TransactionKind != "PURCHASE_PAYMENT" || statement.AmountYen != -1200 || statement.TransactionID != "transaction-2" || statement.SourceHash != digest {
		t.Fatalf("backfilled statement observation is incorrect: %+v", statement)
	}
}

func migrationUUID(t *testing.T) gofrsuuid.UUID {
	t.Helper()
	return gofrsuuid.Must(gofrsuuid.NewV7())
}
