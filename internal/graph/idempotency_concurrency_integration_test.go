package graph

import (
	"context"
	"os"
	"sync"
	"testing"

	"uuid"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestConcurrentIdempotentAccountingMutationsReuseResources(t *testing.T) {
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
	memberUUID := gofrsuuid.Must(gofrsuuid.FromString(memberID))
	subject := "idempotency-concurrency:" + memberID
	displayName := "idempotency concurrency"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &displayName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})

	otherMemberID := uuid.NewV7().String()
	otherMemberUUID := gofrsuuid.Must(gofrsuuid.FromString(otherMemberID))
	otherSubject := "idempotency-concurrency:" + otherMemberID
	otherDisplayName := "idempotency concurrency member"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &otherMemberUUID, OidcSubject: &otherSubject, DisplayName: &otherDisplayName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(otherMemberUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})

	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: displayName})
	mutation := &mutationResolver{Resolver: &Resolver{DB: db}}
	household, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "idempotency concurrency"})
	if err != nil {
		t.Fatal(err)
	}
	householdUUID := gofrsuuid.Must(gofrsuuid.FromString(household.ID))
	t.Cleanup(func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdUUID, MemberID: &otherMemberUUID}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	pool, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: testPersonalPoolName, Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{HouseholdID: household.ID, PoolID: pool.ID, Configuration: personalPoolFundingConfiguration(memberID), ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.setDefaultAllocation(ctx, model.SetDefaultAllocationInput{HouseholdID: household.ID, Allocations: []*model.AllocationInput{{PoolID: pool.ID, Weight: 1}}}); err != nil {
		t.Fatal(err)
	}
	account, err := mutation.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: testCardAccountName, Kind: model.AccountKindCard, PoolID: &pool.ID})
	if err != nil {
		t.Fatal(err)
	}
	purchase, err := mutation.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: testPurchaseTime,
		Merchant: "Concurrent purchase", PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: account.ID, AmountYen: 100}},
		Items: []*model.PurchaseItemInput{{Name: testPurchaseItemName, AmountYen: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}

	fundingKey := uuid.NewV7().String()
	fundingInput := model.RecordFundingInput{
		HouseholdID: household.ID, MemberID: memberID, PoolID: pool.ID,
		AmountYen: 250, OccurredAt: testPurchaseTime, IdempotencyKey: fundingKey,
	}
	fundingIDs := runConcurrentIdempotencyCalls(t, func() (string, error) {
		result, err := mutation.recordFunding(ctx, fundingInput)
		if err != nil {
			return "", err
		}
		return result.ID, nil
	})
	assertSameIdempotentResource(t, fundingIDs)
	assertSingleAccountingRecord(t, ctx, db, householdUUID, fundingKey)

	transferKey := uuid.NewV7().String()
	transferInput := model.RecordTransferInput{
		HouseholdID: household.ID, FromMemberID: memberID, ToMemberID: otherMemberID,
		Kind: model.TransferInputKindGift, AmountYen: 125, OccurredAt: testPurchaseTime,
		Description: "Concurrent transfer", IdempotencyKey: transferKey,
	}
	transferIDs := runConcurrentIdempotencyCalls(t, func() (string, error) {
		result, err := mutation.recordTransfer(ctx, transferInput)
		if err != nil {
			return "", err
		}
		return result.ID, nil
	})
	assertSameIdempotentResource(t, transferIDs)
	assertSingleAccountingRecord(t, ctx, db, householdUUID, transferKey)

	purchaseKey := uuid.NewV7().String()
	purchaseIDs := runConcurrentIdempotencyCalls(t, func() (string, error) {
		result, err := mutation.postPurchase(ctx, purchase.ID, purchase.Revision, purchaseKey)
		if err != nil {
			return "", err
		}
		return result.ID, nil
	})
	assertSameIdempotentResource(t, purchaseIDs)
	assertSingleAccountingRecord(t, ctx, db, householdUUID, purchaseKey)
}

func runConcurrentIdempotencyCalls(t *testing.T, call func() (string, error)) []string {
	t.Helper()
	start := make(chan struct{})
	results := make(chan idempotencyCallResult, 2)
	var workers sync.WaitGroup
	workers.Go(func() {
		<-start
		resourceID, err := call()
		results <- idempotencyCallResult{resourceID: resourceID, err: err}
	})
	workers.Go(func() {
		<-start
		resourceID, err := call()
		results <- idempotencyCallResult{resourceID: resourceID, err: err}
	})
	close(start)
	workers.Wait()
	close(results)
	resourceIDs := make([]string, 0, 2)
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent idempotent call failed: %v", result.err)
		}
		resourceIDs = append(resourceIDs, result.resourceID)
	}
	return resourceIDs
}

type idempotencyCallResult struct {
	resourceID string
	err        error
}

func assertSameIdempotentResource(t *testing.T, resourceIDs []string) {
	t.Helper()
	if len(resourceIDs) != 2 || resourceIDs[0] == "" || resourceIDs[0] != resourceIDs[1] {
		t.Fatalf("concurrent idempotent calls returned different resources: %v", resourceIDs)
	}
}

func assertSingleAccountingRecord(t *testing.T, ctx context.Context, db *pgxpool.Pool, householdID gofrsuuid.UUID, key string) {
	t.Helper()
	executor := bobpgx.NewPool(db)
	journals, err := models.Journals.Query(sm.Where(psql.And(
		models.Journals.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Journals.Columns.IdempotencyKey.EQ(psql.Arg(key)),
	))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if len(journals) != 1 {
		t.Fatalf("idempotency key %q created %d journals; want 1", key, len(journals))
	}
	entries, err := models.LedgerEntries.Query(sm.Where(models.LedgerEntries.Columns.JournalID.EQ(psql.Arg(journals[0].ID)))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("idempotency key %q created %d ledger entries; want 1", key, len(entries))
	}
}
