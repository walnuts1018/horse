package graph

import (
	"context"
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

func TestBudgetConnectionPaginatesOverlappingBudgetsWithinHousehold(t *testing.T) {
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
	if err := db.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	memberID := uuid.NewV7().String()
	memberUUID, err := gofrsuuid.FromString(memberID)
	if err != nil {
		t.Fatal(err)
	}
	subject := "budget-connection-integration:" + memberID
	displayName := "budget connection integration"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &displayName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})

	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: displayName})
	query := &queryResolver{Resolver: &Resolver{DB: db}}
	mutation := &mutationResolver{Resolver: query.Resolver}
	household, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "budget connection integration"})
	if err != nil {
		t.Fatal(err)
	}
	householdID, err := gofrsuuid.FromString(household.ID)
	if err != nil {
		t.Fatal(err)
	}
	otherHousehold, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "other budget connection integration"})
	if err != nil {
		t.Fatal(err)
	}
	otherHouseholdID, err := gofrsuuid.FromString(otherHousehold.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.WithoutCancel(t.Context())
		executor := bobpgx.NewPool(db)
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.In(psql.Arg(householdID), psql.Arg(otherHouseholdID)))).Exec(cleanupCtx, executor)
	})

	insertBudget := func(householdID gofrsuuid.UUID, name, startsOn string, endsOn *string) gofrsuuid.UUID {
		t.Helper()
		id := gofrsuuid.Must(gofrsuuid.NewV7())
		start, err := time.Parse(time.DateOnly, startsOn)
		if err != nil {
			t.Fatal(err)
		}
		var end sql.Null[time.Time]
		if endsOn != nil {
			parsedEnd, err := time.Parse(time.DateOnly, *endsOn)
			if err != nil {
				t.Fatal(err)
			}
			end = sql.Null[time.Time]{V: parsedEnd, Valid: true}
		}
		amount := int64(10000)
		if _, err := models.Budgets.Insert(&models.BudgetSetter{ID: &id, HouseholdID: &householdID, Name: &name, AmountYen: &amount, StartsOn: &start, EndsOn: &end}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
			t.Fatal(err)
		}
		return id
	}
	firstEnd := "2026-01-31"
	secondEnd := "2026-02-28"
	firstDuplicate := insertBudget(householdID, "食費", "2025-12-01", &firstEnd)
	secondDuplicate := insertBudget(householdID, "食費", "2026-01-01", &secondEnd)
	lastMatching := insertBudget(householdID, "交通費", "2026-01-15", nil)
	insertBudget(householdID, "対象外", "2026-02-01", nil)
	insertBudget(otherHouseholdID, "世帯外", "2025-12-01", nil)

	pageSize := 1
	firstPage, err := query.budgets(ctx, household.ID, "2026-01", nil, nil, &pageSize, nil)
	if err != nil {
		t.Fatal(err)
	}
	if firstPage.TotalCount != 3 || len(firstPage.Edges) != 1 || firstPage.Edges[0].Node.Name != "食費" || !firstPage.PageInfo.HasNextPage || firstPage.PageInfo.HasPreviousPage {
		t.Fatalf("unexpected first budget page: %#v", firstPage)
	}
	secondPage, err := query.budgets(ctx, household.ID, "2026-01", firstPage.PageInfo.EndCursor, nil, &pageSize, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondPage.Edges) != 1 || secondPage.Edges[0].Node.Name != "食費" || !secondPage.PageInfo.HasNextPage || !secondPage.PageInfo.HasPreviousPage {
		t.Fatalf("unexpected second budget page: %#v", secondPage)
	}
	thirdPage, err := query.budgets(ctx, household.ID, "2026-01", secondPage.PageInfo.EndCursor, nil, &pageSize, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(thirdPage.Edges) != 1 || thirdPage.Edges[0].Node.Name != "交通費" || thirdPage.PageInfo.HasNextPage || !thirdPage.PageInfo.HasPreviousPage {
		t.Fatalf("unexpected third budget page: %#v", thirdPage)
	}
	firstID, secondID := firstDuplicate.String(), secondDuplicate.String()
	if secondID < firstID {
		firstID, secondID = secondID, firstID
	}
	if firstPage.Edges[0].Cursor != encodeCursor(nodeTypeBudget, firstID) || secondPage.Edges[0].Cursor != encodeCursor(nodeTypeBudget, secondID) || thirdPage.Edges[0].Cursor != encodeCursor(nodeTypeBudget, lastMatching.String()) {
		t.Fatalf("budget cursor order did not preserve name and id ordering: %s, %s, %s", firstPage.Edges[0].Cursor, secondPage.Edges[0].Cursor, thirdPage.Edges[0].Cursor)
	}
	lastPage, err := query.budgets(ctx, household.ID, "2026-01", nil, thirdPage.PageInfo.StartCursor, nil, &pageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(lastPage.Edges) != 1 || lastPage.Edges[0].Cursor != secondPage.Edges[0].Cursor || !lastPage.PageInfo.HasNextPage || !lastPage.PageInfo.HasPreviousPage {
		t.Fatalf("unexpected backward page before duplicate-name boundary: %#v", lastPage)
	}
}
