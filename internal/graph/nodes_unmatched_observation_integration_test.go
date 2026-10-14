package graph

import (
	"database/sql"
	"os"
	"testing"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestFindUnmatchedObservationUsesPersistedRowsAndHouseholdAccess(t *testing.T) {
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
	t.Cleanup(db.Close)
	executor := bobpgx.NewPool(db)
	memberID, outsiderID := newID(), newID()
	memberUUID, outsiderUUID := generatedUUID(memberID), generatedUUID(outsiderID)
	subject, outsiderSubject := "unmatched-node:"+memberID, "unmatched-node:"+outsiderID
	name := "Unmatched node"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &name}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &outsiderUUID, OidcSubject: &outsiderSubject, DisplayName: &name}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.In(psql.Arg(memberUUID), psql.Arg(outsiderUUID)))).Exec(t.Context(), executor)
	})
	ownerContext := auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: name})
	mutation := &mutationResolver{Resolver: &Resolver{DB: db}}
	household, err := mutation.createHousehold(ownerContext, model.CreateHouseholdInput{Name: "Unmatched node"})
	if err != nil {
		t.Fatal(err)
	}
	householdID, documentID := generatedUUID(household.ID), generatedUUID(newID())
	t.Cleanup(func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).Exec(t.Context(), executor)
	})
	hash, objectKey, mimeType, sourceKind := "hash-"+newID(), household.ID+"/originals/"+newID(), "application/pdf", "card"
	if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{
		ID: &documentID, HouseholdID: &householdID, Sha256: &hash, ObjectKey: &objectKey,
		MimeType: &mimeType, SourceKind: &sourceKind, CreatedBy: &memberUUID,
	}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	occurredAt := sql.Null[time.Time]{V: time.Date(2026, 10, 2, 3, 4, 5, 0, time.UTC), Valid: true}
	transactionKind, merchant, orderID, transactionID := "CARD_PURCHASE", "Market", "order-1", "transaction-1"
	amount := int64(1250)
	insertObservation := func(reference string) {
		t.Helper()
		if _, err := models.SourceObservations.Insert(&models.SourceObservationSetter{
			HouseholdID: &householdID, SourceDocumentID: &documentID, SourceReference: &reference,
			SourceHash: &hash, SourceKind: &sourceKind, TransactionKind: &transactionKind,
			OrderID: &orderID, TransactionID: &transactionID, Merchant: &merchant,
			OccurredAt: &occurredAt, AmountYen: &amount,
		}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
	}
	insertObservation("row:1")
	insertObservation("row:2")
	purchaseID, purchaseAt, allocationVersion := generatedUUID(newID()), occurredAt.V, int64(1)
	if _, err := models.Purchases.Insert(&models.PurchaseSetter{
		ID: &purchaseID, HouseholdID: &householdID, OccurredAt: &purchaseAt,
		AllocationRuleVersion: &allocationVersion, CreatedBy: &memberUUID,
	}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	confirmedReference := "row:2"
	confirmedPurchase := sql.Null[gofrsuuid.UUID]{V: purchaseID, Valid: true}
	if _, err := models.DocumentConfirmations.Insert(&models.DocumentConfirmationSetter{
		SourceDocumentID: &documentID, HouseholdID: &householdID, SourceReference: &confirmedReference,
		PurchaseID: &confirmedPurchase, CreatedBy: &memberUUID,
	}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	resolver := &queryResolver{Resolver: &Resolver{DB: db}}
	got, err := resolver.findUnmatchedObservation(ownerContext, documentID.String(), "row:1")
	if err != nil {
		t.Fatal(err)
	}
	unmatched, ok := got.(*model.UnmatchedObservation)
	if !ok {
		t.Fatalf("unmatched observation type = %T, want *model.UnmatchedObservation", got)
	}
	if unmatched.DocumentID != documentID.String() || unmatched.SourceReference != "row:1" || unmatched.SourceKind != model.SourceKindCard || unmatched.Merchant != merchant || unmatched.AmountYen != model.Long(amount) || unmatched.OrderID != orderID || unmatched.TransactionID != transactionID || unmatched.OccurredAt == nil {
		t.Fatalf("unmatched observation fields = %#v, want persisted row values", unmatched.ObservationFields)
	}
	got, err = resolver.findUnmatchedObservation(ownerContext, documentID.String(), "row:2")
	if err != nil {
		t.Fatal(err)
	}
	confirmed, ok := got.(*model.ConfirmedObservation)
	if !ok || confirmed.PurchaseID != purchaseID.String() {
		t.Fatalf("confirmed observation = %#v, want purchase %s", got, purchaseID)
	}
	if got, err := resolver.findUnmatchedObservation(ownerContext, documentID.String(), "missing"); err != nil || got != nil {
		t.Fatalf("missing source reference returned node=%#v error=%v, want nil", got, err)
	}
	outsiderContext := auth.WithPrincipal(ctx, auth.Principal{MemberID: outsiderID, Subject: outsiderSubject, DisplayName: name})
	if got, err := resolver.findUnmatchedObservation(outsiderContext, documentID.String(), "row:1"); err != nil || got != nil {
		t.Fatalf("outsider lookup returned node=%#v error=%v, want nil", got, err)
	}
}
