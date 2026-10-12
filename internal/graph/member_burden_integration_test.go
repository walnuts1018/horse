package graph

import (
	"context"
	"database/sql"
	stdjson "encoding/json"
	"os"
	"testing"
	"time"

	"uuid"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestMonthlyMemberSpendingUsesSavedFundingSnapshotAndOriginalPurchaseMonth(t *testing.T) {
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
	memberA, memberB, outsider := uuid.NewV7().String(), uuid.NewV7().String(), uuid.NewV7().String()
	for id, name := range map[string]string{memberA: "A", memberB: "B", outsider: "Outsider"} {
		memberID, err := gofrsuuid.FromString(id)
		if err != nil {
			t.Fatal(err)
		}
		subject := "burden-integration:" + id
		if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberID, OidcSubject: &subject, DisplayName: &name}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(outsider)))).Exec(context.Background(), bobpgx.NewPool(db))
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberA)))).Exec(context.Background(), bobpgx.NewPool(db))
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberB)))).Exec(context.Background(), bobpgx.NewPool(db))
	}()
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberA, Subject: "burden-integration:" + memberA, DisplayName: "A"})
	resolver := &mutationResolver{Resolver: (&Resolver{DB: db})}
	household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "burden-integration"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(household.ID)))).Exec(context.Background(), bobpgx.NewPool(db))
	}()
	householdID := generatedUUID(household.ID)
	memberBID := generatedUUID(memberB)
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdID, MemberID: &memberBID}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	pool, err := resolver.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "Shared", Kind: model.PoolKindShared})
	if err != nil {
		t.Fatal(err)
	}
	initialPolicy := model.SharedPoolSettlementPolicyBalanceToStandard
	if _, err := resolver.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{HouseholdID: household.ID, PoolID: pool.ID, FundingShares: []*model.PoolFundingShareInput{{MemberID: memberA, Weight: 60}, {MemberID: memberB, Weight: 40}}, SettlementPolicy: &initialPolicy, ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.setDefaultAllocation(ctx, model.SetDefaultAllocationInput{HouseholdID: household.ID, Allocations: []*model.AllocationInput{{PoolID: pool.ID, Weight: 1}}}); err != nil {
		t.Fatal(err)
	}
	account, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Shared card", Kind: model.AccountKindCard, PoolID: &pool.ID})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	currentMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, accountingLocation)
	purchaseDate := currentMonthStart.AddDate(0, -1, 10)
	occurredAt := purchaseDate.UTC().Format(time.RFC3339)
	period := accountingPeriod(purchaseDate)
	purchase, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: occurredAt, Merchant: "Shop", PaymentAccountID: &account.ID,
		Items: []*model.PurchaseItemInput{{Name: "item", AmountYen: 1000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if purchase.Origin != model.PurchaseOriginManual || purchase.HasEvidence {
		t.Fatalf("manual purchase origin was conflated with evidence state: %#v", purchase)
	}
	if _, err := resolver.postPurchase(ctx, purchase.ID, purchase.Revision, uuid.NewV7().String()); err != nil {
		t.Fatal(err)
	}
	sourceDocumentID := generatedUUID(uuid.NewV7().String())
	memberAID := generatedUUID(memberA)
	hash, objectKey, mimeType, sourceKind := uuid.NewV7().String(), "household/"+household.ID+"/test", "image/jpeg", "receipt"
	if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{ID: &sourceDocumentID, HouseholdID: &householdID, Sha256: &hash, ObjectKey: &objectKey, MimeType: &mimeType, SourceKind: &sourceKind, CreatedBy: &memberAID}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	purchaseUUID := generatedUUID(purchase.ID)
	itemID := sql.Null[gofrsuuid.UUID]{}
	observation := types.JSON[map[string]any]{Val: map[string]any{}}
	if _, err := psql.Insert(im.Into(psql.Quote("evidence_links"), "household_id", "source_document_id", "purchase_id", "item_id", "observation"), im.Values(psql.Arg(householdID, sourceDocumentID, purchaseUUID, itemID, observation))).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	withEvidence, err := loadPurchase(ctx, db, purchase.ID)
	if err != nil {
		t.Fatal(err)
	}
	if withEvidence.Origin != model.PurchaseOriginManual || !withEvidence.HasEvidence || len(withEvidence.SourceKinds) != 1 || withEvidence.SourceKinds[0] != model.SourceKindReceipt {
		t.Fatalf("manual origin must remain distinct from later evidence attachment: %#v", withEvidence)
	}
	query := (&Resolver{DB: db}).Query()
	manualOrigin, evidencePresent := model.PurchaseOriginManual, true
	filtered, err := query.Purchases(ctx, household.ID, nil, nil, &manualOrigin, &evidencePresent, nil, nil, nil, nil)
	if err != nil || filtered.TotalCount != 1 || len(filtered.Edges) != 1 || filtered.Edges[0].Node.Origin != model.PurchaseOriginManual {
		t.Fatalf("origin and evidence filters did not remain independent: connection=%#v error=%v", filtered, err)
	}
	jobID := generatedUUID(uuid.NewV7().String())
	jobVersion, jobStatus := int32(1), "succeeded"
	jobResult := types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(`{"receipt":{"merchant":"Shop","purchasedAt":"` + occurredAt + `","items":[{"name":"item","amount":1000}],"total":1000}}`)}
	jobResultValue := sql.Null[types.JSON[stdjson.RawMessage]]{V: jobResult, Valid: true}
	if _, err := models.ExtractionJobs.Insert(&models.ExtractionJobSetter{ID: &jobID, HouseholdID: &householdID, SourceDocumentID: &sourceDocumentID, Version: &jobVersion, Status: &jobStatus, Result: &jobResultValue}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	queryFrom := period
	queryTo := period
	kind, candidateState := model.SourceKindReceipt, model.UnmatchedObservationStateUnmatched
	observations, err := query.UnmatchedObservations(ctx, household.ID, &queryFrom, &queryTo, &kind, &candidateState, nil, nil, nil, nil)
	if err != nil || observations.TotalCount != 1 || len(observations.Edges) != 1 || observations.Edges[0].Node.State != model.UnmatchedObservationStateUnmatched {
		t.Fatalf("unmatched receipt observation was not listed: connection=%#v error=%v", observations, err)
	}
	emptyReference := ""
	if _, err := models.DocumentConfirmations.Insert(&models.DocumentConfirmationSetter{SourceDocumentID: &sourceDocumentID, HouseholdID: &householdID, PurchaseID: &purchaseUUID, CreatedBy: &memberAID, SourceReference: &emptyReference}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	candidateState = model.UnmatchedObservationStateConfirmed
	observations, err = query.UnmatchedObservations(ctx, household.ID, &queryFrom, &queryTo, &kind, &candidateState, nil, nil, nil, nil)
	if err != nil || observations.TotalCount != 1 || observations.Edges[0].Node.PurchaseID == nil || *observations.Edges[0].Node.PurchaseID != purchase.ID {
		t.Fatalf("confirmed observation did not retain its purchase link: connection=%#v error=%v", observations, err)
	}
	updatedAmount := model.Long(1200)
	if _, err := resolver.updatePurchaseItem(ctx, model.UpdatePurchaseItemInput{ItemID: purchase.Items[0].ID, BaseRevision: purchase.Items[0].Revision, AmountYen: &updatedAmount, ChangedFields: []string{"amountYen"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.closePeriod(ctx, model.ClosePeriodInput{HouseholdID: household.ID, Period: period, ExpectedRevision: 0}); err != nil {
		t.Fatal(err)
	}
	policy := model.SharedPoolSettlementPolicyBalanceToStandard
	if _, err := resolver.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{HouseholdID: household.ID, PoolID: pool.ID, FundingShares: []*model.PoolFundingShareInput{{MemberID: memberA, Weight: 40}, {MemberID: memberB, Weight: 60}}, SettlementPolicy: &policy, ExpectedVersion: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.correctPurchase(ctx, model.CorrectPurchaseInput{HouseholdID: household.ID, PurchaseID: purchase.ID, ItemID: &purchase.Items[0].ID, AmountDeltaYen: -100, Reason: "refund", IdempotencyKey: uuid.NewV7().String()}); err != nil {
		t.Fatal(err)
	}
	purchaseUUID = generatedUUID(purchase.ID)
	corrections, err := models.Corrections.Query(sm.Where(models.Corrections.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(corrections) != 1 || !corrections[0].EffectivePeriod.Valid || corrections[0].EffectivePeriod.V == period {
		t.Fatalf("closed-period correction must be booked in another effective month: corrections=%#v error=%v", corrections, err)
	}
	report, err := (&Resolver{DB: db}).Query().MonthlyMemberSpending(ctx, household.ID, period, period)
	if err != nil {
		t.Fatal(err)
	}
	amountByMember := make(map[string]int64, len(report.Items))
	for _, item := range report.Items {
		amountByMember[item.MemberID] = int64(item.AmountYen)
	}
	if report.HasUnattributedPurchases || amountByMember[memberA] != 660 || amountByMember[memberB] != 440 {
		t.Fatalf("historical spending changed after rule edit or correction used the wrong month: report=%#v", report)
	}
	outsiderContext := auth.WithPrincipal(t.Context(), auth.Principal{MemberID: outsider, Subject: "burden-integration:" + outsider, DisplayName: "Outsider"})
	if _, err := (&Resolver{DB: db}).Query().MonthlyMemberSpending(outsiderContext, household.ID, period, period); err == nil {
		t.Fatal("a member outside the household must not read its spending report")
	}
}
