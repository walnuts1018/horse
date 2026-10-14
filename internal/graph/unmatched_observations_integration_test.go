package graph

import (
	"database/sql"
	"os"
	"testing"
	"time"

	"uuid"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestUnmatchedObservationsPagesPersistedRowsAndFiltersConfirmations(t *testing.T) {
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
	memberID := uuid.NewV7().String()
	memberUUID := generatedUUID(memberID)
	subject, displayName := "unmatched-observations:"+memberID, "Unmatched observations"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &displayName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(t.Context(), executor)
	})
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: displayName})
	resolver := &mutationResolver{Resolver: &Resolver{DB: db}}
	household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "Unmatched observations"})
	if err != nil {
		t.Fatal(err)
	}
	householdID := generatedUUID(household.ID)
	t.Cleanup(func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).Exec(t.Context(), executor)
	})

	createDocument := func(createdAt time.Time, observations ...struct {
		reference  string
		occurredAt time.Time
	}) gofrsuuid.UUID {
		documentID, hash, objectKey, mimeType, sourceKind := generatedUUID(uuid.NewV7().String()), "hash-"+uuid.NewV7().String(), household.ID+"/originals/"+uuid.NewV7().String(), "application/pdf", "card"
		if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{
			ID: &documentID, HouseholdID: &householdID, Sha256: &hash, ObjectKey: &objectKey,
			MimeType: &mimeType, SourceKind: &sourceKind, CreatedBy: &memberUUID, CreatedAt: &createdAt,
		}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
		for _, item := range observations {
			occurredAt := sql.Null[time.Time]{V: item.occurredAt, Valid: true}
			transactionKind, merchant := "CARD_PURCHASE", "Market"
			amount := int64(1000)
			if _, err := models.SourceObservations.Insert(&models.SourceObservationSetter{
				HouseholdID: &householdID, SourceDocumentID: &documentID, SourceReference: &item.reference,
				SourceHash: &hash, SourceKind: &sourceKind, TransactionKind: &transactionKind,
				Merchant: &merchant, OccurredAt: &occurredAt, AmountYen: &amount,
			}).Exec(ctx, executor); err != nil {
				t.Fatal(err)
			}
		}
		return documentID
	}
	newDocument := createDocument(time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC),
		struct {
			reference  string
			occurredAt time.Time
		}{"row:2", time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)},
		struct {
			reference  string
			occurredAt time.Time
		}{"row:10", time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)},
		struct {
			reference  string
			occurredAt time.Time
		}{"row:3", time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)},
	)
	oldDocument := createDocument(time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC), struct {
		reference  string
		occurredAt time.Time
	}{"row:1", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)})

	purchaseID, purchaseAt, allocationVersion := generatedUUID(uuid.NewV7().String()), time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), int64(1)
	if _, err := models.Purchases.Insert(&models.PurchaseSetter{
		ID: &purchaseID, HouseholdID: &householdID, OccurredAt: &purchaseAt,
		AllocationRuleVersion: &allocationVersion, CreatedBy: &memberUUID,
	}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	sourceReference := "row:2"
	confirmedPurchase := sql.Null[gofrsuuid.UUID]{V: purchaseID, Valid: true}
	if _, err := models.DocumentConfirmations.Insert(&models.DocumentConfirmationSetter{
		SourceDocumentID: &newDocument, HouseholdID: &householdID, SourceReference: &sourceReference,
		PurchaseID: &confirmedPurchase, CreatedBy: &memberUUID,
	}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}

	october := "2026-10"
	pageSize := 2
	page, err := (&queryResolver{Resolver: resolver.Resolver}).unmatchedObservations(ctx, household.ID, &october, &october, nil, nil, nil, nil, &pageSize, nil)
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalCount != 3 || len(page.Edges) != 2 {
		t.Fatalf("October page has total=%d edges=%d, want total=3 and 2 edges", page.TotalCount, len(page.Edges))
	}
	if page.Edges[0].Node.GetSourceReference() != "row:10" || page.Edges[1].Node.GetSourceReference() != "row:2" {
		t.Fatalf("stable observation order = [%s, %s], want [row:10, row:2]", page.Edges[0].Node.GetSourceReference(), page.Edges[1].Node.GetSourceReference())
	}
	if _, ok := page.Edges[1].Node.(*model.ConfirmedObservation); !ok {
		t.Fatalf("confirmed row node has type %T, want *model.ConfirmedObservation", page.Edges[1].Node)
	}
	if !page.PageInfo.HasNextPage || page.PageInfo.HasPreviousPage {
		t.Fatalf("first page info = %#v, want next=true previous=false", page.PageInfo)
	}
	after := page.PageInfo.EndCursor
	next, err := (&queryResolver{Resolver: resolver.Resolver}).unmatchedObservations(ctx, household.ID, &october, &october, nil, nil, after, nil, &pageSize, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next.TotalCount != 3 || len(next.Edges) != 1 || next.Edges[0].Node.GetDocumentID() != oldDocument.String() || next.Edges[0].Node.GetSourceReference() != "row:1" {
		t.Fatalf("page after row:2 = %#v, want the single older row:1", next)
	}
	if next.PageInfo.HasNextPage || !next.PageInfo.HasPreviousPage {
		t.Fatalf("next page info = %#v, want next=false previous=true", next.PageInfo)
	}
	lastPageSize := 1
	previous, err := (&queryResolver{Resolver: resolver.Resolver}).unmatchedObservations(ctx, household.ID, &october, &october, nil, nil, nil, next.PageInfo.StartCursor, nil, &lastPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if previous.TotalCount != 3 || len(previous.Edges) != 1 || previous.Edges[0].Node.GetSourceReference() != "row:2" {
		t.Fatalf("last row before row:1 = %#v, want row:2", previous)
	}
	if !previous.PageInfo.HasPreviousPage || !previous.PageInfo.HasNextPage {
		t.Fatalf("backward page info = %#v, want previous=true next=true", previous.PageInfo)
	}

	confirmedState := model.UnmatchedObservationStateConfirmed
	confirmedPage, err := (&queryResolver{Resolver: resolver.Resolver}).unmatchedObservations(ctx, household.ID, nil, nil, nil, &confirmedState, nil, nil, &pageSize, nil)
	if err != nil {
		t.Fatal(err)
	}
	if confirmedPage.TotalCount != 1 || len(confirmedPage.Edges) != 1 {
		t.Fatalf("confirmed observations total=%d edges=%d, want one", confirmedPage.TotalCount, len(confirmedPage.Edges))
	}
	confirmedNode, ok := confirmedPage.Edges[0].Node.(*model.ConfirmedObservation)
	if !ok || confirmedNode.PurchaseID != purchaseID.String() {
		t.Fatalf("confirmed node = %#v, want purchase %s", confirmedPage.Edges[0].Node, purchaseID)
	}
	unmatchedState := model.UnmatchedObservationStateUnmatched
	unmatchedPage, err := (&queryResolver{Resolver: resolver.Resolver}).unmatchedObservations(ctx, household.ID, nil, nil, nil, &unmatchedState, nil, nil, &pageSize, nil)
	if err != nil {
		t.Fatal(err)
	}
	if unmatchedPage.TotalCount != 3 || len(unmatchedPage.Edges) != 2 {
		t.Fatalf("unmatched observations total=%d edges=%d, want total=3 and first 2", unmatchedPage.TotalCount, len(unmatchedPage.Edges))
	}
	if unmatchedPage.Edges[0].Node.GetSourceReference() != "row:10" || unmatchedPage.Edges[1].Node.GetSourceReference() != "row:3" {
		t.Fatalf("unmatched order = [%s, %s], want [row:10, row:3]", unmatchedPage.Edges[0].Node.GetSourceReference(), unmatchedPage.Edges[1].Node.GetSourceReference())
	}
}
