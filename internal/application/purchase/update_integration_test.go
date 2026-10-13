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
	events, err := models.HouseholdEvents.Query(sm.Where(psql.And(
		models.HouseholdEvents.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdEvents.Columns.EntityID.EQ(psql.Arg(purchaseID)),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != "purchase.updated" || events[0].Revision != updated.Revision {
		t.Fatalf("purchase events after update = %#v; want one purchase.updated event at revision 2", events)
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
	if updated.Merchant != updatedMerchant || updated.Revision != 2 {
		t.Fatalf("purchase after failed update has merchant %q and revision %d; want %q and 2", updated.Merchant, updated.Revision, updatedMerchant)
	}
}
