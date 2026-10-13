package graph

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestCorrectionRollsBackWhenPostedPurchaseMemberBurdenSnapshotIsMissing(t *testing.T) {
	t.Parallel()
	databaseURL := os.Getenv("HORSE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("HORSE_TEST_DATABASE_URL is not set")
	}
	ctx := t.Context()
	cleanupCtx := context.WithoutCancel(ctx)
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	memberID := uuid.NewV7().String()
	memberUUID := generatedUUID(memberID)
	name := "Correction snapshot member"
	subject := "correction-snapshot:" + memberID
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &name}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(cleanupCtx, bobpgx.NewPool(db))
	}()

	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: name})
	resolver := &mutationResolver{Resolver: &Resolver{DB: db}}
	household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "Correction snapshot rollback"})
	if err != nil {
		t.Fatal(err)
	}
	householdUUID := generatedUUID(household.ID)
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(cleanupCtx, bobpgx.NewPool(db))
	}()

	personalPoolName := "Correction snapshot personal pool"
	pool, err := resolver.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: personalPoolName, Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{
		HouseholdID:     household.ID,
		PoolID:          pool.ID,
		Configuration:   personalPoolFundingConfiguration(memberID),
		ExpectedVersion: 1,
	}); err != nil {
		t.Fatal(err)
	}
	cardAccountName := "Correction snapshot card"
	account, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: cardAccountName, Kind: model.AccountKindCard, PoolID: &pool.ID})
	if err != nil {
		t.Fatal(err)
	}
	purchase, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID,
		OccurredAt: model.DateTime(time.Now().UTC().Format(time.RFC3339)), Merchant: "Correction snapshot shop",
		PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: account.ID, AmountYen: 1000}},
		Items:              []*model.PurchaseItemInput{{Name: "Item", AmountYen: 1000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.postPurchase(ctx, purchase.ID, purchase.Revision, uuid.NewV7().String()); err != nil {
		t.Fatal(err)
	}
	purchaseUUID := generatedUUID(purchase.ID)
	if _, err := models.PurchaseMemberBurdens.Delete(dm.Where(models.PurchaseMemberBurdens.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	correctionPaymentsBefore, err := models.CorrectionPayments.Query(
		sm.InnerJoin(models.Corrections.NameExpr()).On(models.Corrections.Columns.ID.EQ(models.CorrectionPayments.Columns.CorrectionID)),
		sm.Where(models.Corrections.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID))),
	).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	journalEntriesBefore, err := models.JournalEntries.Query(
		sm.InnerJoin(models.Journals.NameExpr()).On(models.Journals.Columns.ID.EQ(models.JournalEntries.Columns.JournalID)),
		sm.Where(models.Journals.Columns.HouseholdID.EQ(psql.Arg(householdUUID))),
	).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}

	correctionKey := uuid.NewV7().String()
	_, err = resolver.correctPurchase(ctx, model.CorrectPurchaseInput{
		HouseholdID:    household.ID,
		Target:         &model.CorrectPurchaseTargetInput{Purchase: &model.PurchaseCorrectionPurchaseTargetInput{PurchaseID: purchase.ID}},
		AmountDeltaYen: -100, Reason: "Refund", IdempotencyKey: correctionKey,
	})
	if !errors.Is(err, ErrMissingPurchaseFundingSnapshot) {
		t.Fatalf("correction with a missing posted burden snapshot returned %v; want ErrMissingPurchaseFundingSnapshot", err)
	}

	executor := bobpgx.NewPool(db)
	corrections, err := models.Corrections.Query(sm.Where(models.Corrections.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, executor)
	if err != nil || len(corrections) != 0 {
		t.Fatalf("correction row survived rollback: rows=%#v error=%v", corrections, err)
	}
	journals, err := models.Journals.Query(sm.Where(psql.And(
		models.Journals.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.Journals.Columns.SourceKind.EQ(psql.Arg("CORRECTION")),
	))).All(ctx, executor)
	if err != nil || len(journals) != 0 {
		t.Fatalf("correction journal survived rollback: rows=%#v error=%v", journals, err)
	}
	ledgerEntries, err := models.LedgerEntries.Query(sm.Where(psql.And(
		models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.LedgerEntries.Columns.Kind.EQ(psql.Arg("CORRECTION")),
		models.LedgerEntries.Columns.SourcePurchaseID.EQ(psql.Arg(purchaseUUID)),
	))).All(ctx, executor)
	if err != nil || len(ledgerEntries) != 0 {
		t.Fatalf("correction ledger entry survived rollback: rows=%#v error=%v", ledgerEntries, err)
	}
	correctionBurdens, err := models.CorrectionMemberBurdens.Query(sm.Where(models.CorrectionMemberBurdens.Columns.HouseholdID.EQ(psql.Arg(householdUUID)))).All(ctx, executor)
	if err != nil || len(correctionBurdens) != 0 {
		t.Fatalf("correction member burden survived rollback: rows=%#v error=%v", correctionBurdens, err)
	}
	correctionPaymentsAfter, err := models.CorrectionPayments.Query(
		sm.InnerJoin(models.Corrections.NameExpr()).On(models.Corrections.Columns.ID.EQ(models.CorrectionPayments.Columns.CorrectionID)),
		sm.Where(models.Corrections.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID))),
	).All(ctx, executor)
	if err != nil || len(correctionPaymentsAfter) != len(correctionPaymentsBefore) {
		t.Fatalf("correction payment survived rollback: before=%d after=%d error=%v", len(correctionPaymentsBefore), len(correctionPaymentsAfter), err)
	}
	journalEntriesAfter, err := models.JournalEntries.Query(
		sm.InnerJoin(models.Journals.NameExpr()).On(models.Journals.Columns.ID.EQ(models.JournalEntries.Columns.JournalID)),
		sm.Where(models.Journals.Columns.HouseholdID.EQ(psql.Arg(householdUUID))),
	).All(ctx, executor)
	if err != nil || len(journalEntriesAfter) != len(journalEntriesBefore) {
		t.Fatalf("correction journal entries survived rollback: before=%d after=%d error=%v", len(journalEntriesBefore), len(journalEntriesAfter), err)
	}
	idempotencyKeys, err := models.IdempotencyKeys.Query(sm.Where(psql.And(
		models.IdempotencyKeys.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.IdempotencyKeys.Columns.Key.EQ(psql.Arg(correctionKey)),
	))).All(ctx, executor)
	if err != nil || len(idempotencyKeys) != 0 {
		t.Fatalf("correction idempotency key survived rollback: rows=%#v error=%v", idempotencyKeys, err)
	}
	settlementAdjustments, err := models.SettlementAdjustments.Query(sm.Where(psql.And(
		models.SettlementAdjustments.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.SettlementAdjustments.Columns.SourceKind.EQ(psql.Arg("CORRECTION")),
	))).All(ctx, executor)
	if err != nil || len(settlementAdjustments) != 0 {
		t.Fatalf("correction settlement adjustment survived rollback: rows=%#v error=%v", settlementAdjustments, err)
	}
}
