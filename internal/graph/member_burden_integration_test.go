package graph

import (
	"database/sql"
	stdjson "encoding/json"
	"errors"
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
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(outsider)))).Exec(t.Context(), bobpgx.NewPool(db))
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberA)))).Exec(t.Context(), bobpgx.NewPool(db))
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberB)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberA, Subject: "burden-integration:" + memberA, DisplayName: "A"})
	resolver := &mutationResolver{Resolver: (&Resolver{DB: db})}
	household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "burden-integration"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(household.ID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	householdID := generatedUUID(household.ID)
	memberBID := generatedUUID(memberB)
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdID, MemberID: &memberBID}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	pool, err := resolver.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: testSharedPoolName, Kind: model.PoolKindShared})
	if err != nil {
		t.Fatal(err)
	}
	initialPolicy := model.SharedPoolSettlementPolicyBalanceToStandard
	if _, err := resolver.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{HouseholdID: household.ID, PoolID: pool.ID, Configuration: sharedPoolFundingConfiguration([]*model.PoolFundingShareInput{{MemberID: memberA, Weight: 60}, {MemberID: memberB, Weight: 40}}, initialPolicy), ExpectedVersion: 1}); err != nil {
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
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: model.DateTime(occurredAt), Merchant: "Shop", PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: account.ID, AmountYen: 1000}},
		Items: []*model.PurchaseItemInput{{Name: testItemName, AmountYen: 1000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if purchase.Origin != model.PurchaseOriginManual || purchase.HasEvidence {
		t.Fatalf("manual purchase origin was conflated with evidence state: %#v", purchase)
	}
	updatedAmount := model.Long(1200)
	if _, err := resolver.updatePurchaseItem(ctx, model.UpdatePurchaseItemInput{ItemID: purchase.Items[0].ID, BaseRevision: purchase.Items[0].Revision, AmountYen: omittable(&updatedAmount)}); err != nil {
		t.Fatal(err)
	}
	purchase, err = loadPurchase(ctx, db, purchase.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.postPurchase(ctx, purchase.ID, purchase.Revision, uuid.NewV7().String()); err != nil {
		t.Fatal(err)
	}
	query := (&Resolver{DB: db}).Query()
	evidenceAbsent := false
	withoutEvidence, err := query.Purchases(ctx, model.PurchaseFilter{HouseholdID: household.ID, HasEvidence: &evidenceAbsent}, nil, nil, nil, nil)
	if err != nil || withoutEvidence.TotalCount != 1 || len(withoutEvidence.Edges) != 1 || withoutEvidence.Edges[0].Node.ID != purchase.ID {
		t.Fatalf("purchase without evidence was excluded: connection=%#v error=%v", withoutEvidence, err)
	}
	sourceDocumentID := generatedUUID(uuid.NewV7().String())
	memberAID := generatedUUID(memberA)
	hash, objectKey, mimeType, sourceKind := uuid.NewV7().String(), "household/"+household.ID+"/test", "image/jpeg", testReceiptSourceKind
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
	manualOrigin, evidencePresent := model.PurchaseOriginManual, true
	filtered, err := query.Purchases(ctx, model.PurchaseFilter{HouseholdID: household.ID, Origin: &manualOrigin, HasEvidence: &evidencePresent}, nil, nil, nil, nil)
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
	queryFrom := model.YearMonth(period)
	queryTo := model.YearMonth(period)
	kind, candidateState := model.SourceKindReceipt, model.UnmatchedObservationStateUnmatched
	observations, err := query.UnmatchedObservations(ctx, household.ID, &queryFrom, &queryTo, &kind, &candidateState, nil, nil, nil, nil)
	if err != nil || observations.TotalCount != 1 || len(observations.Edges) != 1 {
		t.Fatalf("unmatched receipt observation was not listed: connection=%#v error=%v", observations, err)
	}
	if _, ok := observations.Edges[0].Node.(*model.UnmatchedObservation); !ok {
		t.Fatalf("unmatched observation variant was not returned: type=%T", observations.Edges[0].Node)
	}
	emptyReference := ""
	if _, err := models.DocumentConfirmations.Insert(&models.DocumentConfirmationSetter{SourceDocumentID: &sourceDocumentID, HouseholdID: &householdID, PurchaseID: &purchaseUUID, CreatedBy: &memberAID, SourceReference: &emptyReference}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	candidateState = model.UnmatchedObservationStateConfirmed
	observations, err = query.UnmatchedObservations(ctx, household.ID, &queryFrom, &queryTo, &kind, &candidateState, nil, nil, nil, nil)
	if err != nil || observations.TotalCount != 1 || len(observations.Edges) != 1 {
		t.Fatalf("confirmed observation was not listed: connection=%#v error=%v", observations, err)
	}
	confirmedObservation, ok := observations.Edges[0].Node.(*model.ConfirmedObservation)
	if !ok {
		t.Fatalf("confirmed observation variant was not returned: type=%T", observations.Edges[0].Node)
	}
	confirmedPurchase, err := resolver.ConfirmedObservation().Purchase(ctx, confirmedObservation)
	if err != nil || confirmedPurchase.ID != purchase.ID {
		t.Fatalf("confirmed observation purchase was not returned: purchase=%#v error=%v", confirmedPurchase, err)
	}
	if _, err := resolver.closePeriod(ctx, model.ClosePeriodInput{HouseholdID: household.ID, Period: model.YearMonth(period), ExpectedRevision: 0}); err != nil {
		t.Fatal(err)
	}
	policy := model.SharedPoolSettlementPolicyBalanceToStandard
	if _, err := resolver.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{HouseholdID: household.ID, PoolID: pool.ID, Configuration: sharedPoolFundingConfiguration([]*model.PoolFundingShareInput{{MemberID: memberA, Weight: 40}, {MemberID: memberB, Weight: 60}}, policy), ExpectedVersion: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.correctPurchase(ctx, model.CorrectPurchaseInput{HouseholdID: household.ID, Target: &model.CorrectPurchaseTargetInput{Item: &model.PurchaseCorrectionItemTargetInput{PurchaseID: purchase.ID, ItemID: purchase.Items[0].ID}}, AmountDeltaYen: -100, Reason: "refund", IdempotencyKey: uuid.NewV7().String()}); err != nil {
		t.Fatal(err)
	}
	purchaseUUID = generatedUUID(purchase.ID)
	corrections, err := models.Corrections.Query(sm.Where(models.Corrections.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(corrections) != 1 || !corrections[0].EffectivePeriod.Valid || corrections[0].EffectivePeriod.V == period {
		t.Fatalf("closed-period correction must be booked in another effective month: corrections=%#v error=%v", corrections, err)
	}
	report, err := (&Resolver{DB: db}).Query().MonthlyMemberSpending(ctx, household.ID, model.YearMonth(period), model.YearMonth(period))
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
	categoryReport, err := query.MonthlyCategorySpending(ctx, household.ID, model.YearMonth(period), model.YearMonth(period))
	if err != nil {
		t.Fatal(err)
	}
	if len(categoryReport) != 1 || string(categoryReport[0].Period) != period || categoryReport[0].CategoryID != nil || categoryReport[0].CategoryName != nil || int64(categoryReport[0].AmountYen) != 1100 {
		t.Fatalf("monthly category spending changed the purchase month or correction amount: %#v", categoryReport)
	}
	secondPurchase, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: model.DateTime(now.UTC().Format(time.RFC3339)), Merchant: "Another shop",
		PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: account.ID, AmountYen: 300}},
		Items:              []*model.PurchaseItemInput{{Name: "another item", AmountYen: 300}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tag, err := resolver.createTag(ctx, model.CreateTagInput{HouseholdID: household.ID, Name: "Batch loading"})
	if err != nil {
		t.Fatal(err)
	}
	tagUUID := generatedUUID(tag.ID)
	itemUUID := generatedUUID(purchase.Items[0].ID)
	if _, err := models.PurchaseItemTags.Insert(&models.PurchaseItemTagSetter{HouseholdID: &householdID, PurchaseID: &purchaseUUID, ItemID: &itemUUID, TagID: &tagUUID}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	listed, err := (&Resolver{DB: db}).Query().Purchases(ctx, model.PurchaseFilter{HouseholdID: household.ID}, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if listed.TotalCount != 2 || len(listed.Edges) != 2 {
		t.Fatalf("purchase list returned %d purchases with %d edges, want 2", listed.TotalCount, len(listed.Edges))
	}
	listedByID := make(map[string]*model.Purchase, len(listed.Edges))
	for _, edge := range listed.Edges {
		listedByID[edge.Node.ID] = edge.Node
	}
	listedPurchase := listedByID[purchase.ID]
	if listedPurchase == nil {
		t.Fatalf("first purchase is missing from its list: %v", listedByID)
	}
	if !listedPurchase.HasEvidence || len(listedPurchase.SourceKinds) != 1 || listedPurchase.SourceKinds[0] != model.SourceKindReceipt {
		t.Fatalf("first purchase evidence relations are incorrect: %#v", listedPurchase)
	}
	if len(listedPurchase.Items) != 1 || listedPurchase.Items[0].Name != testItemName || int64(listedPurchase.Items[0].AmountYen) != 1200 {
		t.Fatalf("first purchase item relation is incorrect: %#v", listedPurchase.Items)
	}
	if len(listedPurchase.Items[0].Tags) != 1 || listedPurchase.Items[0].Tags[0].ID != tag.ID {
		t.Fatalf("first purchase tag relation is incorrect: %#v", listedPurchase.Items[0].Tags)
	}
	if len(listedPurchase.Items[0].Allocations) != 1 || listedPurchase.Items[0].Allocations[0].PoolID != pool.ID || int64(listedPurchase.Items[0].Allocations[0].AmountYen) != 1200 {
		t.Fatalf("first purchase allocation relation is incorrect: %#v", listedPurchase.Items[0].Allocations)
	}
	if len(listedPurchase.PaymentAllocations) != 1 || listedPurchase.PaymentAllocations[0].PaymentAccountID != account.ID || int64(listedPurchase.PaymentAllocations[0].AmountYen) != 1200 || len(listedPurchase.CorrectionsConnection.Edges) != 1 {
		t.Fatalf("first purchase relations were not loaded from their own records: %#v", listedPurchase)
	}
	listedSecondPurchase := listedByID[secondPurchase.ID]
	if listedSecondPurchase == nil {
		t.Fatalf("second purchase is missing from its list: %v", listedByID)
	}
	if listedSecondPurchase.HasEvidence || len(listedSecondPurchase.Items) != 1 || listedSecondPurchase.Items[0].Name != "another item" || int64(listedSecondPurchase.Items[0].AmountYen) != 300 {
		t.Fatalf("second purchase item or evidence relations crossed with another purchase: %#v", listedSecondPurchase)
	}
	if len(listedSecondPurchase.Items[0].Tags) != 0 {
		t.Fatalf("second purchase inherited another purchase's tag: %#v", listedSecondPurchase.Items[0].Tags)
	}
	if len(listedSecondPurchase.Items[0].Allocations) != 1 || listedSecondPurchase.Items[0].Allocations[0].PoolID != pool.ID || int64(listedSecondPurchase.Items[0].Allocations[0].AmountYen) != 300 {
		t.Fatalf("second purchase allocation relation crossed with another purchase: %#v", listedSecondPurchase.Items[0].Allocations)
	}
	if len(listedSecondPurchase.PaymentAllocations) != 1 || listedSecondPurchase.PaymentAllocations[0].PaymentAccountID != account.ID || int64(listedSecondPurchase.PaymentAllocations[0].AmountYen) != 300 || len(listedSecondPurchase.CorrectionsConnection.Edges) != 0 {
		t.Fatalf("second purchase relations crossed with another purchase: %#v", listedSecondPurchase)
	}
	outsiderContext := auth.WithPrincipal(t.Context(), auth.Principal{MemberID: outsider, Subject: "burden-integration:" + outsider, DisplayName: "Outsider"})
	if _, err := (&Resolver{DB: db}).Query().MonthlyMemberSpending(outsiderContext, household.ID, model.YearMonth(period), model.YearMonth(period)); err == nil {
		t.Fatal("a member outside the household must not read its spending report")
	}
}

func TestPostedPurchaseUpdatesPreserveExistingSnapshotsAndCreateCurrentSnapshotsForNewAllocations(t *testing.T) {
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
		memberID, err := gofrsuuid.FromString(id)
		if err != nil {
			t.Fatal(err)
		}
		subject := "purchase-snapshot-integration:" + id
		if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberID, OidcSubject: &subject, DisplayName: &name}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberA)))).Exec(t.Context(), bobpgx.NewPool(db))
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberB)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberA, Subject: "purchase-snapshot-integration:" + memberA, DisplayName: "A"})
	resolver := &mutationResolver{Resolver: (&Resolver{DB: db})}
	household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "purchase snapshot integration"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(household.ID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	householdID, memberBID := generatedUUID(household.ID), generatedUUID(memberB)
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdID, MemberID: &memberBID}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	poolOne, err := resolver.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "one", Kind: model.PoolKindShared})
	if err != nil {
		t.Fatal(err)
	}
	poolTwo, err := resolver.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "two", Kind: model.PoolKindShared})
	if err != nil {
		t.Fatal(err)
	}
	policyOne := model.SharedPoolSettlementPolicyBalanceToStandard
	policyTwo := model.SharedPoolSettlementPolicyBalanceToStandard
	for _, input := range []model.SetPoolFundingRuleInput{
		{HouseholdID: household.ID, PoolID: poolOne.ID, Configuration: sharedPoolFundingConfiguration([]*model.PoolFundingShareInput{{MemberID: memberA, Weight: 60}, {MemberID: memberB, Weight: 40}}, policyOne), ExpectedVersion: 1},
		{HouseholdID: household.ID, PoolID: poolTwo.ID, Configuration: sharedPoolFundingConfiguration([]*model.PoolFundingShareInput{{MemberID: memberA, Weight: 25}, {MemberID: memberB, Weight: 75}}, policyTwo), ExpectedVersion: 1},
	} {
		if _, err := resolver.setPoolFundingRule(ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	accountOne, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "one card", Kind: model.AccountKindCard, PoolID: &poolOne.ID})
	if err != nil {
		t.Fatal(err)
	}
	accountOneAlternate, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "one alternate card", Kind: model.AccountKindCard, PoolID: &poolOne.ID})
	if err != nil {
		t.Fatal(err)
	}
	accountOneNew, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "one new card", Kind: model.AccountKindCard, PoolID: &poolOne.ID})
	if err != nil {
		t.Fatal(err)
	}
	accountTwo, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "two card", Kind: model.AccountKindCard, PoolID: &poolTwo.ID})
	if err != nil {
		t.Fatal(err)
	}
	purchase, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: model.DateTime(time.Now().UTC().Format(time.RFC3339)), Merchant: "snapshot shop",
		PaymentAllocations: []*model.PurchasePaymentInput{
			{PaymentAccountID: accountOne.ID, AmountYen: 500}, {PaymentAccountID: accountOneAlternate.ID, AmountYen: 500},
		},
		Items: []*model.PurchaseItemInput{{Name: testItemName, AmountYen: 1000, Allocations: []*model.AllocationInput{{PoolID: poolOne.ID, Weight: 1}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	posted, err := resolver.postPurchase(ctx, purchase.ID, purchase.Revision, uuid.NewV7().String())
	if err != nil {
		t.Fatal(err)
	}
	policyOne = model.SharedPoolSettlementPolicyReimbursePersonal
	policyTwo = model.SharedPoolSettlementPolicyReimbursePersonal
	for _, input := range []model.SetPoolFundingRuleInput{
		{HouseholdID: household.ID, PoolID: poolOne.ID, Configuration: sharedPoolFundingConfiguration([]*model.PoolFundingShareInput{{MemberID: memberA, Weight: 10}, {MemberID: memberB, Weight: 90}}, policyOne), ExpectedVersion: 2},
		{HouseholdID: household.ID, PoolID: poolTwo.ID, Configuration: sharedPoolFundingConfiguration([]*model.PoolFundingShareInput{{MemberID: memberA, Weight: 80}, {MemberID: memberB, Weight: 20}}, policyTwo), ExpectedVersion: 2},
	} {
		if _, err := resolver.setPoolFundingRule(ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	updated, err := resolver.updatePurchase(ctx, model.UpdatePurchaseInput{
		PurchaseID: posted.ID, BaseRevision: posted.Revision,
		Patch: &model.PurchasePatch{PaymentAllocations: omittable([]*model.PurchasePaymentInput{
			{PaymentAccountID: accountOne.ID, AmountYen: 300}, {PaymentAccountID: accountOneNew.ID, AmountYen: 300}, {PaymentAccountID: accountTwo.ID, AmountYen: 400},
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	posted = updated
	item := posted.Items[0]
	_, err = resolver.updatePurchaseItem(ctx, model.UpdatePurchaseItemInput{
		ItemID: item.ID, BaseRevision: item.Revision,
		Allocations: omittable([]*model.AllocationInput{{PoolID: poolOne.ID, Weight: 1}, {PoolID: poolTwo.ID, Weight: 1}}),
	})
	if err != nil {
		t.Fatal(err)
	}

	purchaseUUID := generatedUUID(purchase.ID)
	paymentSnapshots, err := models.PurchasePaymentFundingSnapshots.Query(sm.Where(psql.And(
		models.PurchasePaymentFundingSnapshots.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.PurchasePaymentFundingSnapshots.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	assertSnapshots := func(got models.PurchasePaymentFundingSnapshotSlice, lineNo int32, poolID string, version int32, weights map[string]int64, settlement string) {
		members := make(map[string]int64)
		for _, snapshot := range got {
			if snapshot.PaymentLineNo != lineNo || snapshot.PoolID.String() != poolID {
				continue
			}
			if snapshot.FundingRuleVersion != version || snapshot.SnapshotSource != testPostedStatus || !snapshot.SettlementPolicy.Valid || snapshot.SettlementPolicy.V != settlement {
				t.Fatalf("payment snapshot metadata changed: %#v", snapshot)
			}
			members[snapshot.MemberID.String()] = snapshot.FundingWeight
		}
		if len(members) != len(weights) {
			t.Fatalf("payment snapshot members mismatch: got=%v want=%v", members, weights)
		}
		for memberID, weight := range weights {
			if members[memberID] != weight {
				t.Fatalf("payment snapshot weights mismatch: got=%v want=%v", members, weights)
			}
		}
	}
	assertSnapshots(paymentSnapshots, 1, poolOne.ID, 2, map[string]int64{memberA: 60, memberB: 40}, string(model.SharedPoolSettlementPolicyBalanceToStandard))
	assertSnapshots(paymentSnapshots, 2, poolOne.ID, 2, map[string]int64{memberA: 60, memberB: 40}, string(model.SharedPoolSettlementPolicyBalanceToStandard))
	assertSnapshots(paymentSnapshots, 3, poolTwo.ID, 3, map[string]int64{memberA: 80, memberB: 20}, string(policyTwo))

	itemUUID := generatedUUID(item.ID)
	burdens, err := models.PurchaseMemberBurdens.Query(sm.Where(psql.And(
		models.PurchaseMemberBurdens.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.PurchaseMemberBurdens.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)),
		models.PurchaseMemberBurdens.Columns.ItemID.EQ(psql.Arg(itemUUID)),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	weightsByPool := map[string]map[string]int64{}
	versionsByPool := map[string]int32{}
	for _, burden := range burdens {
		if weightsByPool[burden.PoolID.String()] == nil {
			weightsByPool[burden.PoolID.String()] = make(map[string]int64)
		}
		weightsByPool[burden.PoolID.String()][burden.MemberID.String()] = burden.FundingWeight
		versionsByPool[burden.PoolID.String()] = burden.FundingRuleVersion
		if burden.SnapshotSource != testPostedStatus {
			t.Fatalf("posted burden source changed: %#v", burden)
		}
	}
	if versionsByPool[poolOne.ID] != 2 || weightsByPool[poolOne.ID][memberA] != 60 || weightsByPool[poolOne.ID][memberB] != 40 {
		t.Fatalf("existing item/pool snapshot was replaced: versions=%v weights=%v", versionsByPool, weightsByPool)
	}
	if versionsByPool[poolTwo.ID] != 3 || weightsByPool[poolTwo.ID][memberA] != 80 || weightsByPool[poolTwo.ID][memberB] != 20 {
		t.Fatalf("new item/pool snapshot did not use the current rule: versions=%v weights=%v", versionsByPool, weightsByPool)
	}
	if _, err := calculatePeriodAccounting(ctx, bobpgx.NewPool(db), household.ID, accountingPeriod(time.Now())); err != nil {
		if errors.Is(err, ErrMissingPurchaseFundingSnapshot) {
			t.Fatalf("period accounting rejected an updated purchase because its snapshot was missing: %v", err)
		}
		t.Fatalf("period accounting failed after preserving and creating snapshots: %v", err)
	}
}
