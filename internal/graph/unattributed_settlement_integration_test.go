package graph

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"uuid"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestSettlementRejectsPostedPurchasePaidFromAccountWithoutPool(t *testing.T) {
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
	subject, displayName := "unattributed-settlement:"+memberID, "unattributed settlement"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &displayName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(context.Background(), bobpgx.NewPool(db))
	})

	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: displayName})
	resolver := &Resolver{DB: db}
	household, err := (&mutationResolver{Resolver: resolver}).createHousehold(ctx, model.CreateHouseholdInput{Name: "unattributed settlement"})
	if err != nil {
		t.Fatal(err)
	}
	householdUUID, err := gofrsuuid.FromString(household.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(context.Background(), bobpgx.NewPool(db))
	})

	mutation := &mutationResolver{Resolver: resolver}
	pool, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "Personal", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{HouseholdID: household.ID, PoolID: pool.ID, OwnerMemberID: &memberID, FundingShares: []*model.PoolFundingShareInput{}, ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.setDefaultAllocation(ctx, model.SetDefaultAllocationInput{HouseholdID: household.ID, Allocations: []*model.AllocationInput{{PoolID: pool.ID, Weight: 1}}}); err != nil {
		t.Fatal(err)
	}
	account, err := mutation.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Unassigned card", Kind: model.AccountKindCard, PoolID: &pool.ID})
	if err != nil {
		t.Fatal(err)
	}
	assignedAccount, err := mutation.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Assigned card", Kind: model.AccountKindCard, PoolID: &pool.ID})
	if err != nil {
		t.Fatal(err)
	}
	purchase, err := mutation.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: "2026-10-13T10:00:00+09:00",
		Merchant: "Unassigned payment", PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: assignedAccount.ID, AmountYen: 100}},
		Items: []*model.PurchaseItemInput{{Name: "item", AmountYen: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.postPurchase(ctx, purchase.ID, purchase.Revision, uuid.NewV7().String()); err != nil {
		t.Fatal(err)
	}
	purchaseUUID, err := gofrsuuid.FromString(purchase.ID)
	if err != nil {
		t.Fatal(err)
	}
	accountUUID, err := gofrsuuid.FromString(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	firstLineNo, secondLineNo := int32(1), int32(2)
	firstAmount, secondAmount := int64(50), int64(50)
	if _, err := models.PurchasePayments.Update((&models.PurchasePaymentSetter{AmountYen: &firstAmount}).UpdateMod(), um.Where(psql.And(
		models.PurchasePayments.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.PurchasePayments.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)),
		models.PurchasePayments.Columns.LineNo.EQ(psql.Arg(firstLineNo)),
	))).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	if _, err := models.PurchasePayments.Insert(&models.PurchasePaymentSetter{
		HouseholdID: &householdUUID, PurchaseID: &purchaseUUID, LineNo: &secondLineNo, AccountID: &accountUUID, AmountYen: &secondAmount,
	}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}

	_, err = resolver.Query().Settlement(ctx, household.ID, "2026-10")
	if !errors.Is(err, ErrUnattributedPurchasePayment) || !strings.Contains(err.Error(), "no funding pool") || !strings.Contains(err.Error(), purchase.ID) || !strings.Contains(err.Error(), account.ID) {
		t.Fatalf("settlement preview should report the unattributed payment and its source, got %v", err)
	}
	_, err = mutation.closePeriod(ctx, model.ClosePeriodInput{HouseholdID: household.ID, Period: "2026-10", ExpectedRevision: 0})
	if !errors.Is(err, ErrUnattributedPurchasePayment) || !strings.Contains(err.Error(), "no funding pool") || !strings.Contains(err.Error(), purchase.ID) || !strings.Contains(err.Error(), account.ID) {
		t.Fatalf("settlement close should report the unattributed payment and its source, got %v", err)
	}
}
