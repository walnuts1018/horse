package graph

import (
	"os"
	"testing"
	"time"

	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const (
	testCorrectionPersonalPoolName = "A wallet"
	testCorrectionRefundReason     = "Refund"
)

func TestPurchaseRefundSettlementUsesOriginalFundingSnapshotAfterRuleChanges(t *testing.T) {
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
	defer db.Close()
	if err := db.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	memberA, memberB := uuid.NewV7().String(), uuid.NewV7().String()
	for id, name := range map[string]string{memberA: "A", memberB: "B"} {
		memberUUID := generatedUUID(id)
		subject := "correction-settlement-snapshot:" + id
		if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &name}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberA)))).Exec(t.Context(), bobpgx.NewPool(db))
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberB)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberA, Subject: "correction-settlement-snapshot:" + memberA, DisplayName: "A"})
	apiResolver := &Resolver{DB: db}
	mutation := &mutationResolver{Resolver: apiResolver}
	household, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "correction funding snapshot"})
	if err != nil {
		t.Fatal(err)
	}
	householdUUID := generatedUUID(household.ID)
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	memberBUUID := generatedUUID(memberB)
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdUUID, MemberID: &memberBUUID}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	personalA, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: testCorrectionPersonalPoolName, Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	personalB, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "B wallet", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	shared, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: testSharedPoolName, Kind: model.PoolKindShared})
	if err != nil {
		t.Fatal(err)
	}
	for _, configuration := range []model.SetPoolFundingRuleInput{
		{HouseholdID: household.ID, PoolID: personalA.ID, Configuration: personalPoolFundingConfiguration(memberA), ExpectedVersion: 1},
		{HouseholdID: household.ID, PoolID: personalB.ID, Configuration: personalPoolFundingConfiguration(memberB), ExpectedVersion: 1},
		{HouseholdID: household.ID, PoolID: shared.ID, Configuration: sharedPoolFundingConfiguration([]*model.PoolFundingShareInput{{MemberID: memberA, Weight: 60}, {MemberID: memberB, Weight: 40}}, model.SharedPoolSettlementPolicyBalanceToStandard), ExpectedVersion: 1},
	} {
		if _, err := mutation.setPoolFundingRule(ctx, configuration); err != nil {
			t.Fatal(err)
		}
	}
	sharedAccount, err := mutation.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "shared card", Kind: model.AccountKindCard, PoolID: &shared.ID})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	period := accountingPeriod(now)
	purchase, err := mutation.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: model.DateTime(now.UTC().Format(time.RFC3339)), Merchant: "snapshot shop",
		PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: sharedAccount.ID, AmountYen: 1000}},
		Items:              []*model.PurchaseItemInput{{Name: "personal item", AmountYen: 1000, Allocations: []*model.AllocationInput{{PoolID: personalA.ID, Weight: 1}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.postPurchase(ctx, purchase.ID, purchase.Revision, uuid.NewV7().String()); err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{
		HouseholdID: household.ID, PoolID: shared.ID,
		Configuration:   sharedPoolFundingConfiguration([]*model.PoolFundingShareInput{{MemberID: memberA, Weight: 40}, {MemberID: memberB, Weight: 60}}, model.SharedPoolSettlementPolicyReimbursePersonal),
		ExpectedVersion: 2,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{
		HouseholdID: household.ID, PoolID: personalA.ID, Configuration: personalPoolFundingConfiguration(memberB), ExpectedVersion: 2,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.correctPurchase(ctx, model.CorrectPurchaseInput{
		HouseholdID:    household.ID,
		Target:         &model.CorrectPurchaseTargetInput{Item: &model.PurchaseCorrectionItemTargetInput{PurchaseID: purchase.ID, ItemID: purchase.Items[0].ID}},
		AmountDeltaYen: -100, Reason: testCorrectionRefundReason, IdempotencyKey: uuid.NewV7().String(),
	}); err != nil {
		t.Fatal(err)
	}
	settlement, err := apiResolver.Query().Settlement(ctx, household.ID, model.YearMonth(period))
	if err != nil {
		t.Fatal(err)
	}
	lines := settlementByMember(t, settlementData(t, settlement).Lines)
	if lines[memberA].MonthlyAdjustmentYen != 360 || lines[memberB].MonthlyAdjustmentYen != -360 {
		t.Fatalf("refund settlement used current funding rules instead of purchase snapshots: A=%d B=%d", lines[memberA].MonthlyAdjustmentYen, lines[memberB].MonthlyAdjustmentYen)
	}
}
