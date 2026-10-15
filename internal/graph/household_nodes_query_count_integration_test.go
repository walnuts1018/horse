package graph

import (
	"context"
	"os"
	"sync/atomic"
	"testing"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestHouseholdAndMixedNodeQueryCountsDoNotScaleWithHouseholdCount(t *testing.T) {
	databaseURL := os.Getenv("HORSE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("HORSE_TEST_DATABASE_URL is not set")
	}
	ctx := t.Context()
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	counter := &householdNodesQueryCounter{}
	config.ConnConfig.Tracer = counter
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	executor := bobpgx.NewPool(db)

	memberID := generatedUUID(gofrsuuid.Must(gofrsuuid.NewV7()).String())
	subject, displayName := "household-nodes-query-count:"+memberID.String(), "Household query count"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberID, OidcSubject: &subject, DisplayName: &displayName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberID)))).Exec(context.WithoutCancel(t.Context()), executor)
	})
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID.String(), Subject: subject, DisplayName: displayName})
	mutation := &mutationResolver{Resolver: &Resolver{DB: db}}
	households := make([]*model.Household, 0, 10)
	accounts, categories, pools := make([]string, 0, 10), make([]string, 0, 10), make([]string, 0, 10)
	for range 10 {
		household, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "query count household"})
		if err != nil {
			t.Fatal(err)
		}
		householdUUID := generatedUUID(household.ID)
		t.Cleanup(func() {
			_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(context.WithoutCancel(t.Context()), executor)
		})
		account, err := mutation.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "account", Kind: model.AccountKindBank})
		if err != nil {
			t.Fatal(err)
		}
		category, err := mutation.createCategory(ctx, model.CreateCategoryInput{HouseholdID: household.ID, Name: "category"})
		if err != nil {
			t.Fatal(err)
		}
		pool, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "pool", Kind: model.PoolKindPersonal})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := mutation.setDefaultAllocation(ctx, model.SetDefaultAllocationInput{HouseholdID: household.ID, Allocations: []*model.AllocationInput{{PoolID: pool.ID, Weight: 1}}}); err != nil {
			t.Fatal(err)
		}
		households = append(households, household)
		accounts = append(accounts, account.ID)
		categories = append(categories, category.ID)
		pools = append(pools, pool.ID)
	}
	ids := make([]gofrsuuid.UUID, len(households))
	for index, household := range households {
		ids[index] = generatedUUID(household.ID)
	}
	measureHouseholds := func(count int) int64 {
		t.Helper()
		counter.count.Store(0)
		loaded, err := loadHouseholds(ctx, db, ids[:count], allHouseholdFields)
		if err != nil {
			t.Fatal(err)
		}
		if len(loaded) != count {
			t.Fatalf("loaded %d households, want %d", len(loaded), count)
		}
		return counter.count.Load()
	}
	measureNodes := func(count int) int64 {
		t.Helper()
		globalIDs := make([]string, 0, count*6)
		for index := range count {
			for _, node := range []struct{ kind, id string }{
				{nodeTypeHousehold, households[index].ID}, {nodeTypeAccount, accounts[index]},
				{nodeTypeCategory, categories[index]}, {nodeTypePool, pools[index]},
				{nodeTypeMember, memberID.String()},
			} {
				id, err := encodeNodeID(node.kind, node.id)
				if err != nil {
					t.Fatal(err)
				}
				globalIDs = append(globalIDs, id)
			}
			settlementID, err := encodeSettlementNodeID(households[index].ID, "2026-10")
			if err != nil {
				t.Fatal(err)
			}
			globalIDs = append(globalIDs, settlementID)
		}
		counter.count.Store(0)
		resolved, err := (&queryResolver{Resolver: &Resolver{DB: db}}).nodes(ctx, globalIDs)
		if err != nil {
			t.Fatal(err)
		}
		if len(resolved) != len(globalIDs) {
			t.Fatalf("resolved %d nodes, want %d", len(resolved), len(globalIDs))
		}
		for index, node := range resolved {
			if node == nil {
				t.Fatalf("resolved node %d is nil", index)
			}
			if index%6 == 5 {
				if _, ok := node.(*model.OpenSettlement); !ok {
					t.Fatalf("resolved node %d = %T, want open Settlement", index, node)
				}
			}
		}
		return counter.count.Load()
	}
	oneHouseholdQueries, fiveHouseholdQueries, tenHouseholdQueries := measureHouseholds(1), measureHouseholds(5), measureHouseholds(10)
	oneMixedNodeQueries, fiveMixedNodeQueries, tenMixedNodeQueries := measureNodes(1), measureNodes(5), measureNodes(10)
	t.Logf("loadHouseholds(all fields): one=%d five=%d ten=%d; nodes(mixed types including open Settlement): one=%d five=%d ten=%d", oneHouseholdQueries, fiveHouseholdQueries, tenHouseholdQueries, oneMixedNodeQueries, fiveMixedNodeQueries, tenMixedNodeQueries)
	if oneHouseholdQueries != fiveHouseholdQueries || oneHouseholdQueries != tenHouseholdQueries {
		t.Fatalf("loadHouseholds query count scaled: one=%d five=%d ten=%d", oneHouseholdQueries, fiveHouseholdQueries, tenHouseholdQueries)
	}
	if oneMixedNodeQueries != fiveMixedNodeQueries || oneMixedNodeQueries != tenMixedNodeQueries {
		t.Fatalf("mixed nodes query count scaled: one=%d five=%d ten=%d", oneMixedNodeQueries, fiveMixedNodeQueries, tenMixedNodeQueries)
	}
}

type householdNodesQueryCounter struct{ count atomic.Int64 }

func (counter *householdNodesQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	counter.count.Add(1)
	return ctx
}

func (*householdNodesQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
