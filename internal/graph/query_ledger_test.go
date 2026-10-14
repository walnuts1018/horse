package graph

import (
	"context"
	"os"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestLedgerTotalCountRequested(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		query     string
		requested bool
	}{
		{
			name:  "omitted",
			query: `query { ledger { edges { cursor } } }`,
		},
		{
			name:      "aliased through fragment",
			query:     `query { ledger { ...LedgerCount } } fragment LedgerCount on LedgerConnection { count: totalCount }`,
			requested: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx := ledgerContextWithQuery(t, t.Context(), test.query)

			if requested := ledgerTotalCountRequested(ctx); requested != test.requested {
				t.Fatalf("totalCount requested = %t, want %t", requested, test.requested)
			}
		})
	}
}

func TestLedgerConnectionPageInfoUsesCursorPosition(t *testing.T) {
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

	memberID := gofrsuuid.Must(gofrsuuid.NewV7()).String()
	memberUUID, err := gofrsuuid.FromString(memberID)
	if err != nil {
		t.Fatal(err)
	}
	subject := "ledger-cursor-integration:" + memberID
	displayName := "ledger cursor integration"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &displayName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})

	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: displayName})
	resolver := &queryResolver{Resolver: &Resolver{DB: db}}
	mutation := &mutationResolver{Resolver: resolver.Resolver}
	household, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "ledger cursor integration"})
	if err != nil {
		t.Fatal(err)
	}
	householdUUID, err := gofrsuuid.FromString(household.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})
	pool, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "Ledger cursor pool", Kind: model.PoolKindShared})
	if err != nil {
		t.Fatal(err)
	}

	cursorsByDay := make(map[string]string, 3)
	lastDay := "2026-10-13"
	middleDay := "2026-10-14"
	firstDay := "2026-10-15"
	days := []string{lastDay, middleDay, firstDay}
	for _, day := range days {
		entry, err := mutation.recordFunding(ctx, model.RecordFundingInput{
			HouseholdID: household.ID, MemberID: memberID, PoolID: pool.ID, AmountYen: 100,
			OccurredAt: model.DateTime(day + "T10:00:00Z"), IdempotencyKey: gofrsuuid.Must(gofrsuuid.NewV7()).String(),
		})
		if err != nil {
			t.Fatal(err)
		}
		cursorsByDay[day] = encodeCursor(nodeTypeLedgerEntry, entry.ID)
	}

	tests := []struct {
		name            string
		cursorDay       string
		backward        bool
		wantHasPrevious bool
		wantHasNext     bool
		wantEdgeCount   int
	}{
		{name: "after first", cursorDay: firstDay, wantHasNext: true, wantEdgeCount: 1},
		{name: "after middle", cursorDay: middleDay, wantHasPrevious: true, wantEdgeCount: 1},
		{name: "after last", cursorDay: lastDay, wantHasPrevious: true},
		{name: "before first", cursorDay: firstDay, backward: true, wantHasNext: true},
		{name: "before middle", cursorDay: middleDay, backward: true, wantHasNext: true, wantEdgeCount: 1},
		{name: "before last", cursorDay: lastDay, backward: true, wantHasPrevious: true, wantEdgeCount: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			queryCtx := ledgerContextWithQuery(t, ctx, `query { ledger { edges { cursor } } }`)
			cursor := cursorsByDay[test.cursorDay]
			var after, before *string
			if test.backward {
				before = &cursor
			} else {
				after = &cursor
			}
			var first, last *int
			pageSize := 1
			if test.backward {
				last = &pageSize
			} else {
				first = &pageSize
			}

			connection, err := resolver.ledger(queryCtx, household.ID, after, before, first, last)
			if err != nil {
				t.Fatal(err)
			}
			if got := len(connection.Edges); got != test.wantEdgeCount {
				t.Fatalf("edge count = %d, want %d", got, test.wantEdgeCount)
			}
			if connection.PageInfo.HasPreviousPage != test.wantHasPrevious || connection.PageInfo.HasNextPage != test.wantHasNext {
				t.Fatalf("page info = (previous: %t, next: %t), want (previous: %t, next: %t)", connection.PageInfo.HasPreviousPage, connection.PageInfo.HasNextPage, test.wantHasPrevious, test.wantHasNext)
			}
		})
	}
}

func ledgerContextWithQuery(t *testing.T, ctx context.Context, query string) context.Context {
	t.Helper()
	doc, err := parser.ParseQuery(&ast.Source{Input: query})
	if err != nil {
		t.Fatal(err)
	}
	ledgerField := doc.Operations[0].SelectionSet[0].(*ast.Field)
	ctx = graphql.WithOperationContext(ctx, &graphql.OperationContext{Doc: doc, Variables: map[string]any{}})

	return graphql.WithFieldContext(ctx, &graphql.FieldContext{Field: graphql.CollectedField{Field: ledgerField, Selections: ledgerField.SelectionSet}})
}
