package purchase

import (
	"context"
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

func TestServiceUpdatePersistsRevisionEventAndRejectsClosedPeriod(t *testing.T) {
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

	memberID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	subject := "purchase-update-integration:" + memberID.String()
	displayName := "Purchase update integration"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberID, OidcSubject: &subject, DisplayName: &displayName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.WithoutCancel(t.Context())
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberID)))).Exec(cleanupCtx, bobpgx.NewPool(db))
	})

	householdID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	householdName := "Purchase update integration"
	if _, err := models.Households.Insert(&models.HouseholdSetter{ID: &householdID, Name: &householdName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	role := "OWNER"
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdID, MemberID: &memberID, Role: &role}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.WithoutCancel(t.Context())
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).Exec(cleanupCtx, bobpgx.NewPool(db))
	})

	purchaseID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	occurredAt := time.Date(2026, time.October, 13, 12, 0, 0, 0, time.FixedZone("JST", 9*60*60))
	merchant, status, origin := "Before update", "DRAFT", "MANUAL"
	allocationVersion := int64(0)
	if _, err := models.Purchases.Insert(&models.PurchaseSetter{
		ID: &purchaseID, HouseholdID: &householdID, OccurredAt: &occurredAt, Merchant: &merchant,
		Status: &status, AllocationRuleVersion: &allocationVersion, CreatedBy: &memberID, Origin: &origin,
	}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}

	updatedMerchant := "After update"
	service := NewService(db)
	result, err := service.Update(ctx, UpdateCommand{
		ActorID: memberID.String(), PurchaseID: purchaseID.String(), BaseRevision: 1,
		Merchant: &updatedMerchant, ChangedFields: []string{updateFieldMerchant},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.PurchaseID != purchaseID.String() {
		t.Fatalf("Update returned purchase %q; want %q", result.PurchaseID, purchaseID)
	}

	updated, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseID)))).One(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Merchant != updatedMerchant || updated.Revision != 2 {
		t.Fatalf("purchase after update has merchant %q and revision %d; want %q and 2", updated.Merchant, updated.Revision, updatedMerchant)
	}
	bulkMerchant := "After bulk update"
	bulkResult, err := service.BulkUpdate(ctx, BulkUpdateCommand{
		ActorID: memberID.String(),
		Targets: []BulkUpdateTarget{{
			PurchaseID: purchaseID.String(), BaseRevision: updated.Revision,
			Merchant: &bulkMerchant, ChangedFields: []string{updateFieldMerchant},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(bulkResult.PurchaseIDs) != 1 || bulkResult.PurchaseIDs[0] != purchaseID.String() {
		t.Fatalf("BulkUpdate returned purchase IDs %v; want [%s]", bulkResult.PurchaseIDs, purchaseID)
	}
	updated, err = models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseID)))).One(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Merchant != bulkMerchant || updated.Revision != 3 {
		t.Fatalf("purchase after bulk update has merchant %q and revision %d; want %q and 3", updated.Merchant, updated.Revision, bulkMerchant)
	}
	itemID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	itemName, itemAmount := "Before item update", int64(0)
	if _, err := models.PurchaseItems.Insert(&models.PurchaseItemSetter{
		ID: &itemID, HouseholdID: &householdID, PurchaseID: &purchaseID, Name: &itemName, AmountYen: &itemAmount,
	}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	updatedItemName := "After item update"
	itemResult, err := service.UpdateItem(ctx, UpdateItemCommand{
		ActorID: memberID.String(), ItemID: itemID.String(), BaseRevision: 1,
		ChangedFields: []string{itemFieldName}, Name: &updatedItemName,
	})
	if err != nil {
		t.Fatal(err)
	}
	if itemResult.PurchaseID != purchaseID.String() {
		t.Fatalf("UpdateItem returned purchase %q; want %q", itemResult.PurchaseID, purchaseID)
	}
	updatedItem, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemID)))).One(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if updatedItem.Name != updatedItemName || updatedItem.Revision != 2 {
		t.Fatalf("item after update has name %q and revision %d; want %q and 2", updatedItem.Name, updatedItem.Revision, updatedItemName)
	}
	itemEvents, err := models.HouseholdEvents.Query(sm.Where(psql.And(
		models.HouseholdEvents.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdEvents.Columns.EntityID.EQ(psql.Arg(itemID)),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if len(itemEvents) != 1 || itemEvents[0].Type != "purchase.item.updated" || itemEvents[0].Revision != updatedItem.Revision {
		t.Fatalf("item events after update = %#v; want one purchase.item.updated event at revision 2", itemEvents)
	}
	updated, err = models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseID)))).One(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 4 {
		t.Fatalf("purchase revision after item update = %d; want 4", updated.Revision)
	}
	events, err := models.HouseholdEvents.Query(sm.Where(psql.And(
		models.HouseholdEvents.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdEvents.Columns.EntityID.EQ(psql.Arg(purchaseID)),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	seenRevisions := make(map[int64]bool, len(events))
	for _, event := range events {
		if event.Type == "purchase.updated" {
			seenRevisions[event.Revision] = true
		}
	}
	if len(events) != 2 || !seenRevisions[2] || !seenRevisions[3] {
		t.Fatalf("purchase events after updates = %#v; want purchase.updated events at revisions 2 and 3", events)
	}

	period, closed := "2026-10", "CLOSED"
	if _, err := models.AccountingPeriods.Update((&models.AccountingPeriodSetter{Status: &closed}).UpdateMod(), um.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(period)),
	))).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	rollbackMerchant := "Must roll back"
	_, err = NewService(db).Update(ctx, UpdateCommand{
		ActorID: memberID.String(), PurchaseID: purchaseID.String(), BaseRevision: updated.Revision,
		Merchant: &rollbackMerchant, ChangedFields: []string{updateFieldMerchant},
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Update error = %v; want ErrConflict for closed accounting period", err)
	}
	updated, err = models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseID)))).One(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Merchant != bulkMerchant || updated.Revision != 4 {
		t.Fatalf("purchase after failed update has merchant %q and revision %d; want %q and 4", updated.Merchant, updated.Revision, bulkMerchant)
	}
}

func TestServiceSaveItemEditsUpdatesMultipleItemsAndPurchase(t *testing.T) {
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

	memberID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	subject := "purchase-save-item-edits-integration:" + memberID.String()
	displayName := "Purchase save item edits integration"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberID, OidcSubject: &subject, DisplayName: &displayName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.WithoutCancel(t.Context())
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberID)))).Exec(cleanupCtx, bobpgx.NewPool(db))
	})

	householdID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	householdName := "Purchase save item edits integration"
	if _, err := models.Households.Insert(&models.HouseholdSetter{ID: &householdID, Name: &householdName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	role := "OWNER"
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdID, MemberID: &memberID, Role: &role}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.WithoutCancel(t.Context())
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).Exec(cleanupCtx, bobpgx.NewPool(db))
	})

	purchaseID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	occurredAt := time.Date(2026, time.October, 13, 12, 0, 0, 0, time.FixedZone("JST", 9*60*60))
	merchant, status, origin := "Before save", "DRAFT", "MANUAL"
	allocationVersion := int64(0)
	if _, err := models.Purchases.Insert(&models.PurchaseSetter{
		ID: &purchaseID, HouseholdID: &householdID, OccurredAt: &occurredAt, Merchant: &merchant,
		Status: &status, AllocationRuleVersion: &allocationVersion, CreatedBy: &memberID, Origin: &origin,
	}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}

	firstItemID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	secondItemID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	firstName, secondName, itemAmount := "First before save", "Second before save", int64(0)
	for _, item := range []struct {
		id   gofrsuuid.UUID
		name *string
	}{{firstItemID, &firstName}, {secondItemID, &secondName}} {
		if _, err := models.PurchaseItems.Insert(&models.PurchaseItemSetter{
			ID: &item.id, HouseholdID: &householdID, PurchaseID: &purchaseID, Name: item.name, AmountYen: &itemAmount,
		}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
			t.Fatal(err)
		}
	}

	firstUpdatedName, secondUpdatedName, updatedMerchant := "First saved", "Second saved", "After save"
	result, err := NewService(db).SaveItemEdits(ctx, SaveItemEditsCommand{
		ActorID: memberID.String(), PurchaseID: purchaseID.String(),
		Items: []UpdateItemCommand{
			{ItemID: secondItemID.String(), BaseRevision: 1, ChangedFields: []string{itemFieldName}, Name: &secondUpdatedName},
			{ItemID: firstItemID.String(), BaseRevision: 1, ChangedFields: []string{itemFieldName}, Name: &firstUpdatedName},
		},
		PurchaseUpdate: &UpdateCommand{BaseRevision: 1, Merchant: &updatedMerchant, ChangedFields: []string{updateFieldMerchant}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 2 || result.Items[0].ID != secondItemID.String() || result.Items[1].ID != firstItemID.String() {
		t.Fatalf("SaveItemEdits returned items %#v; want command order [%s, %s]", result.Items, secondItemID, firstItemID)
	}
	for _, expected := range []struct {
		id   gofrsuuid.UUID
		name string
	}{{firstItemID, firstUpdatedName}, {secondItemID, secondUpdatedName}} {
		item, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(expected.id)))).One(ctx, bobpgx.NewPool(db))
		if err != nil {
			t.Fatal(err)
		}
		if item.Name != expected.name || item.Revision != 2 {
			t.Fatalf("item %s after save has name %q and revision %d; want %q and 2", expected.id, item.Name, item.Revision, expected.name)
		}
	}
	purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseID)))).One(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if purchase.Merchant != updatedMerchant || purchase.Revision != 4 {
		t.Fatalf("purchase after save has merchant %q and revision %d; want %q and 4", purchase.Merchant, purchase.Revision, updatedMerchant)
	}
}
