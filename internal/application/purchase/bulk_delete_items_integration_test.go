package purchase

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestServiceBulkDeleteItemsCommitsAuditAndRejectsLastItem(t *testing.T) {
	t.Parallel()
	databaseURL := os.Getenv("HORSE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("HORSE_TEST_DATABASE_URL is not set")
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
	executor := bobpgx.NewPool(db)
	memberID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	subject := "purchase-bulk-delete:" + memberID.String()
	displayName := "Purchase bulk delete integration"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberID, OidcSubject: &subject, DisplayName: &displayName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.WithoutCancel(t.Context())
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberID)))).Exec(cleanupCtx, executor)
	})
	householdID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	householdName := "Purchase bulk delete integration"
	if _, err := models.Households.Insert(&models.HouseholdSetter{ID: &householdID, Name: &householdName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	role := "OWNER"
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdID, MemberID: &memberID, Role: &role}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.WithoutCancel(t.Context())
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).Exec(cleanupCtx, executor)
	})
	purchaseID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	occurredAt := time.Date(2026, time.October, 13, 12, 0, 0, 0, time.UTC)
	merchant, status, origin := "Bulk delete", "DRAFT", "MANUAL"
	allocationVersion := int64(0)
	if _, err := models.Purchases.Insert(&models.PurchaseSetter{
		ID: &purchaseID, HouseholdID: &householdID, OccurredAt: &occurredAt, Merchant: &merchant,
		Status: &status, AllocationRuleVersion: &allocationVersion, CreatedBy: &memberID, Origin: &origin,
	}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	itemIDs := make([]gofrsuuid.UUID, 3)
	for index := range itemIDs {
		itemIDs[index], err = gofrsuuid.NewV7()
		if err != nil {
			t.Fatal(err)
		}
		name, amount := "Item", int64(0)
		if _, err := models.PurchaseItems.Insert(&models.PurchaseItemSetter{
			ID: &itemIDs[index], HouseholdID: &householdID, PurchaseID: &purchaseID, Name: &name, AmountYen: &amount,
		}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
	}
	service := NewService(db)
	result, err := service.BulkDeleteItems(ctx, BulkDeleteItemsCommand{
		ActorID: memberID.String(),
		Targets: []BulkItemTarget{{ItemID: itemIDs[0].String(), BaseRevision: 1}, {ItemID: itemIDs[1].String(), BaseRevision: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.DeletedItemIDs) != 2 {
		t.Fatalf("deleted item IDs = %v; want 2", result.DeletedItemIDs)
	}
	poolID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	poolName, poolKind := "Payment test pool", "PERSONAL"
	if _, err := models.Pools.Insert(&models.PoolSetter{ID: &poolID, HouseholdID: &householdID, Name: &poolName, Kind: &poolKind}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	fundingRuleVersion := int32(1)
	ownerMember := sql.Null[gofrsuuid.UUID]{V: memberID, Valid: true}
	if _, err := models.PoolFundingRules.Insert(&models.PoolFundingRuleSetter{
		HouseholdID: &householdID, PoolID: &poolID, Version: &fundingRuleVersion,
		OwnerMemberID: &ownerMember, CreatedBy: &memberID,
	}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	accountID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	accountName, accountKind := "Payment test account", "CASH"
	accountPool := sql.Null[gofrsuuid.UUID]{V: poolID, Valid: true}
	if _, err := models.Accounts.Insert(&models.AccountSetter{
		ID: &accountID, HouseholdID: &householdID, PoolID: &accountPool, Name: &accountName, Kind: &accountKind,
	}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	paidPurchaseID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	paidMerchant, paidStatus, paidOrigin := "Paid purchase", "DRAFT", "MANUAL"
	if _, err := models.Purchases.Insert(&models.PurchaseSetter{
		ID: &paidPurchaseID, HouseholdID: &householdID, OccurredAt: &occurredAt, Merchant: &paidMerchant,
		Status: &paidStatus, AllocationRuleVersion: &allocationVersion, CreatedBy: &memberID, Origin: &paidOrigin,
	}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	paidItemIDs := make([]gofrsuuid.UUID, 3)
	for index := range paidItemIDs {
		paidItemIDs[index], err = gofrsuuid.NewV7()
		if err != nil {
			t.Fatal(err)
		}
	}
	paidTx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = paidTx.Rollback(ctx) }()
	paidExecutor := bobpgx.NewTx(paidTx, nil)
	for _, itemID := range paidItemIDs {
		itemName, itemAmount, weight, ruleVersion := "Paid item", int64(50), int64(1), int64(1)
		if _, err := models.PurchaseItems.Insert(&models.PurchaseItemSetter{
			ID: &itemID, HouseholdID: &householdID, PurchaseID: &paidPurchaseID, Name: &itemName, AmountYen: &itemAmount,
		}).Exec(ctx, paidExecutor); err != nil {
			t.Fatal(err)
		}
		if _, err := models.ItemAllocations.Insert(&models.ItemAllocationSetter{
			HouseholdID: &householdID, PurchaseID: &paidPurchaseID, ItemID: &itemID, PoolID: &poolID,
			Weight: &weight, AmountYen: &itemAmount, RuleVersion: &ruleVersion,
		}).Exec(ctx, paidExecutor); err != nil {
			t.Fatal(err)
		}
	}
	lineNo, paymentAmount := int32(1), int64(150)
	if _, err := models.PurchasePayments.Insert(&models.PurchasePaymentSetter{
		HouseholdID: &householdID, PurchaseID: &paidPurchaseID, LineNo: &lineNo, AccountID: &accountID, AmountYen: &paymentAmount,
	}).Exec(ctx, paidExecutor); err != nil {
		t.Fatal(err)
	}
	if err := paidTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := service.BulkDeleteItems(ctx, BulkDeleteItemsCommand{
		ActorID: memberID.String(), Targets: []BulkItemTarget{
			{ItemID: paidItemIDs[0].String(), BaseRevision: 1},
			{ItemID: paidItemIDs[1].String(), BaseRevision: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}
	paidPayments, err := models.PurchasePayments.Query(sm.Where(models.PurchasePayments.Columns.PurchaseID.EQ(psql.Arg(paidPurchaseID)))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if len(paidPayments) != 1 || paidPayments[0].AmountYen != 50 {
		t.Fatalf("payment rows after deleting a paid item = %#v; want one row for 50 yen", paidPayments)
	}
	postedPurchaseID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	postedMerchant, postedStatus, postedOrigin := "Posted purchase", "POSTED", "MANUAL"
	if _, err := models.Purchases.Insert(&models.PurchaseSetter{
		ID: &postedPurchaseID, HouseholdID: &householdID, OccurredAt: &occurredAt, Merchant: &postedMerchant,
		Status: &postedStatus, AllocationRuleVersion: &allocationVersion, CreatedBy: &memberID, Origin: &postedOrigin,
	}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	postedItemIDs := make([]gofrsuuid.UUID, 2)
	for index := range postedItemIDs {
		postedItemIDs[index], err = gofrsuuid.NewV7()
		if err != nil {
			t.Fatal(err)
		}
	}
	postedJournalID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	postedTx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = postedTx.Rollback(ctx) }()
	postedExecutor := bobpgx.NewTx(postedTx, nil)
	for _, itemID := range postedItemIDs {
		itemName, itemAmount, weight, ruleVersion := "Posted item", int64(50), int64(1), int64(1)
		if _, err := models.PurchaseItems.Insert(&models.PurchaseItemSetter{
			ID: &itemID, HouseholdID: &householdID, PurchaseID: &postedPurchaseID, Name: &itemName, AmountYen: &itemAmount,
		}).Exec(ctx, postedExecutor); err != nil {
			t.Fatal(err)
		}
		if _, err := models.ItemAllocations.Insert(&models.ItemAllocationSetter{
			HouseholdID: &householdID, PurchaseID: &postedPurchaseID, ItemID: &itemID, PoolID: &poolID,
			Weight: &weight, AmountYen: &itemAmount, RuleVersion: &ruleVersion,
		}).Exec(ctx, postedExecutor); err != nil {
			t.Fatal(err)
		}
	}
	postedLineNo, postedPaymentAmount := int32(1), int64(100)
	if _, err := models.PurchasePayments.Insert(&models.PurchasePaymentSetter{
		HouseholdID: &householdID, PurchaseID: &postedPurchaseID, LineNo: &postedLineNo,
		AccountID: &accountID, AmountYen: &postedPaymentAmount,
	}).Exec(ctx, postedExecutor); err != nil {
		t.Fatal(err)
	}
	snapshotWeight := int64(1)
	if _, err := models.PurchasePaymentFundingSnapshots.Insert(&models.PurchasePaymentFundingSnapshotSetter{
		HouseholdID: &householdID, PurchaseID: &postedPurchaseID, PaymentLineNo: &postedLineNo,
		PoolID: &poolID, MemberID: &memberID, FundingRuleVersion: &fundingRuleVersion,
		FundingWeight: &snapshotWeight, SnapshotSource: &postedStatus,
	}).Exec(ctx, postedExecutor); err != nil {
		t.Fatal(err)
	}
	journalKind, journalKey := "PURCHASE", "posted-delete:"+postedJournalID.String()
	if _, err := models.Journals.Insert(&models.JournalSetter{
		ID: &postedJournalID, HouseholdID: &householdID, SourceKind: &journalKind,
		SourceID: &postedPurchaseID, IdempotencyKey: &journalKey, OccurredAt: &occurredAt, CreatedBy: &memberID,
	}).Exec(ctx, postedExecutor); err != nil {
		t.Fatal(err)
	}
	if err := postedTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := service.BulkDeleteItems(ctx, BulkDeleteItemsCommand{
		ActorID: memberID.String(), Targets: []BulkItemTarget{{ItemID: postedItemIDs[0].String(), BaseRevision: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	postedPayments, err := models.PurchasePayments.Query(sm.Where(models.PurchasePayments.Columns.PurchaseID.EQ(psql.Arg(postedPurchaseID)))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if len(postedPayments) != 1 || postedPayments[0].AmountYen != 50 {
		t.Fatalf("posted payment rows after deletion = %#v; want one row for 50 yen", postedPayments)
	}
	postedSnapshots, err := models.PurchasePaymentFundingSnapshots.Query(sm.Where(models.PurchasePaymentFundingSnapshots.Columns.PurchaseID.EQ(psql.Arg(postedPurchaseID)))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if len(postedSnapshots) != 1 || postedSnapshots[0].PaymentLineNo != 1 {
		t.Fatalf("posted funding snapshots after deletion = %#v; want one snapshot for payment line 1", postedSnapshots)
	}
	postedEntries, err := models.JournalEntries.Query(sm.Where(models.JournalEntries.Columns.JournalID.EQ(psql.Arg(postedJournalID)))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	var debitTotal, creditTotal int64
	for _, entry := range postedEntries {
		debitTotal += entry.DebitYen
		creditTotal += entry.CreditYen
	}
	if len(postedEntries) != 2 || debitTotal != 50 || creditTotal != 50 {
		t.Fatalf("posted journal entries after deletion = %#v; want balanced 50 yen entries", postedEntries)
	}
	remaining, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseID)))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].ID != itemIDs[2] {
		t.Fatalf("remaining items = %v; want only %s", remaining, itemIDs[2])
	}
	events, err := models.HouseholdEvents.Query(sm.Where(psql.And(
		models.HouseholdEvents.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdEvents.Columns.Type.EQ(psql.Arg("purchase.item.deleted")),
	))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 5 {
		t.Fatalf("deletion events = %d; want 5", len(events))
	}
	_, err = service.BulkDeleteItems(ctx, BulkDeleteItemsCommand{
		ActorID: memberID.String(), Targets: []BulkItemTarget{{ItemID: itemIDs[2].String(), BaseRevision: 1}},
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("deleting the last item error = %v; want ErrConflict", err)
	}
	remaining, err = models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemIDs[2])))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 {
		t.Fatalf("last item count after rejected deletion = %d; want 1", len(remaining))
	}
	secondItemID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	name, amount := "Second item", int64(0)
	if _, err := models.PurchaseItems.Insert(&models.PurchaseItemSetter{
		ID: &secondItemID, HouseholdID: &householdID, PurchaseID: &purchaseID, Name: &name, AmountYen: &amount,
	}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	closedStatus := "CLOSED"
	if _, err := models.Purchases.Update((&models.PurchaseSetter{Status: &closedStatus}).UpdateMod(), um.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseID)))).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	_, err = service.BulkDeleteItems(ctx, BulkDeleteItemsCommand{
		ActorID: memberID.String(), Targets: []BulkItemTarget{{ItemID: itemIDs[2].String(), BaseRevision: 1}},
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("deleting a CLOSED purchase item error = %v; want ErrConflict", err)
	}
	openStatus := "DRAFT"
	if _, err := models.Purchases.Update((&models.PurchaseSetter{Status: &openStatus}).UpdateMod(), um.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseID)))).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	journalID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	journalKind, idempotencyKey := "CORRECTION", "bulk-delete-correction:"+journalID.String()
	if _, err := models.Journals.Insert(&models.JournalSetter{
		ID: &journalID, HouseholdID: &householdID, SourceKind: &journalKind, SourceID: &journalID,
		IdempotencyKey: &idempotencyKey, OccurredAt: &occurredAt, CreatedBy: &memberID,
	}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	correctionID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	itemReference := sql.Null[gofrsuuid.UUID]{V: itemIDs[2], Valid: true}
	delta, reason := int64(1), "integration fixture"
	if _, err := models.Corrections.Insert(&models.CorrectionSetter{
		ID: &correctionID, HouseholdID: &householdID, PurchaseID: &purchaseID, ItemID: &itemReference,
		AmountDeltaYen: &delta, Reason: &reason, JournalID: &journalID, CreatedBy: &memberID,
	}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	_, err = service.BulkDeleteItems(ctx, BulkDeleteItemsCommand{
		ActorID: memberID.String(), Targets: []BulkItemTarget{{ItemID: itemIDs[2].String(), BaseRevision: 1}},
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("deleting a correction-referenced item error = %v; want ErrConflict", err)
	}
}
