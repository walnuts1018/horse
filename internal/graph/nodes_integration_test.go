package graph

import (
	"context"
	"os"
	"testing"

	"github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestNodesBatchPreservesOrderDuplicatesAndHouseholdAuthorization(t *testing.T) {
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

	memberID, outsiderID := newID(), newID()
	insertMember := func(id string) {
		t.Helper()
		memberUUID, err := uuid.FromString(id)
		if err != nil {
			t.Fatal(err)
		}
		subject, name := "nodes-integration:"+id, testNodesIntegrationName
		if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &name}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
			t.Fatal(err)
		}
	}
	insertMember(memberID)
	insertMember(outsiderID)
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(psql.Or(models.Members.Columns.ID.EQ(psql.Arg(memberID)), models.Members.Columns.ID.EQ(psql.Arg(outsiderID))))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})

	ownerContext := auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: "nodes-integration:" + memberID, DisplayName: testNodesIntegrationName})
	outsiderContext := auth.WithPrincipal(ctx, auth.Principal{MemberID: outsiderID, Subject: "nodes-integration:" + outsiderID, DisplayName: testNodesIntegrationName})
	mutation := &mutationResolver{Resolver: &Resolver{DB: db}}
	ownedHousehold, err := mutation.createHousehold(ownerContext, model.CreateHouseholdInput{Name: "nodes integration owned"})
	if err != nil {
		t.Fatal(err)
	}
	foreignHousehold, err := mutation.createHousehold(outsiderContext, model.CreateHouseholdInput{Name: "nodes integration foreign"})
	if err != nil {
		t.Fatal(err)
	}
	for _, householdID := range []string{ownedHousehold.ID, foreignHousehold.ID} {
		householdUUID, err := uuid.FromString(householdID)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
		})
	}
	ownedAccount, err := mutation.createAccount(ownerContext, model.CreateAccountInput{HouseholdID: ownedHousehold.ID, Name: "owned", Kind: model.AccountKindBank})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := mutation.createPool(ownerContext, model.CreatePoolInput{HouseholdID: ownedHousehold.ID, Name: "nodes integration", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.setDefaultAllocation(ownerContext, model.SetDefaultAllocationInput{HouseholdID: ownedHousehold.ID, Allocations: []*model.AllocationInput{{PoolID: pool.ID, Weight: 1}}}); err != nil {
		t.Fatal(err)
	}
	ownedPurchase, err := mutation.createPurchase(ownerContext, model.CreatePurchaseInput{
		IdempotencyKey: uuid.Must(uuid.NewV7()).String(), HouseholdID: ownedHousehold.ID,
		OccurredAt: "2026-10-14T12:00:00Z", Merchant: "nodes integration",
		Items: []*model.PurchaseItemInput{{Name: "batch node", AmountYen: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	foreignAccount, err := mutation.createAccount(outsiderContext, model.CreateAccountInput{HouseholdID: foreignHousehold.ID, Name: "foreign", Kind: model.AccountKindBank})
	if err != nil {
		t.Fatal(err)
	}
	ownedTag, err := mutation.createTag(ownerContext, model.CreateTagInput{HouseholdID: ownedHousehold.ID, Name: "owned tag"})
	if err != nil {
		t.Fatal(err)
	}
	globalID := func(kind, id string) string {
		t.Helper()
		value, err := encodeNodeID(kind, id)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}

	got, err := (&queryResolver{Resolver: &Resolver{DB: db}}).nodes(ownerContext, []string{
		globalID("Tag", ownedTag.ID), globalID("Household", ownedHousehold.ID), globalID("Account", ownedAccount.ID),
		globalID("Tag", ownedTag.ID), globalID("Account", foreignAccount.ID), globalID("Account", newID()),
		globalID(nodeTypeMember, memberID), globalID(nodeTypePool, pool.ID),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 8 {
		t.Fatalf("nodes returned %d values, want 8", len(got))
	}
	if value, ok := got[0].(*model.Tag); !ok || value.ID != ownedTag.ID {
		t.Fatalf("nodes[0] = %#v, want owned tag", got[0])
	}
	if value, ok := got[1].(*model.Household); !ok || value.ID != ownedHousehold.ID {
		t.Fatalf("nodes[1] = %#v, want owned household", got[1])
	}
	if value, ok := got[2].(*model.Account); !ok || value.ID != ownedAccount.ID {
		t.Fatalf("nodes[2] = %#v, want owned account", got[2])
	}
	if value, ok := got[3].(*model.Tag); !ok || value.ID != ownedTag.ID {
		t.Fatalf("nodes[3] = %#v, want duplicate owned tag", got[3])
	}
	if got[4] != nil || got[5] != nil {
		t.Fatalf("nodes[4:6] = %#v, want nil for foreign and missing household accounts", got[4:6])
	}
	if value, ok := got[6].(*model.Member); !ok || value.ID != memberID {
		t.Fatalf("nodes[6] = %#v, want authorized member", got[6])
	}
	if value, ok := got[7].(model.PoolVariant); !ok || value.GetID() != pool.ID {
		t.Fatalf("nodes[7] = %#v, want authorized pool", got[7])
	}

	purchaseID := globalID(nodeTypePurchase, ownedPurchase.ID)
	query := &queryResolver{Resolver: &Resolver{DB: db}}
	gotNode, err := query.node(ownerContext, purchaseID)
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := gotNode.(*model.Purchase); !ok || value.ID != ownedPurchase.ID {
		t.Fatalf("node = %#v, want owned purchase", gotNode)
	}
	foreignNode, err := query.node(outsiderContext, purchaseID)
	if err != nil {
		t.Fatal(err)
	}
	if foreignNode != nil {
		t.Fatalf("node = %#v, want nil for another household's purchase", foreignNode)
	}
	gotBatch, err := query.nodes(ownerContext, []string{purchaseID, purchaseID})
	if err != nil {
		t.Fatal(err)
	}
	for i, node := range gotBatch {
		if value, ok := node.(*model.Purchase); !ok || value.ID != ownedPurchase.ID {
			t.Fatalf("nodes[%d] = %#v, want owned purchase", i, node)
		}
	}
}
