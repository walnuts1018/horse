package graph

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"uuid"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const testAccountTransferDescription = "Reserve transfer"

func TestAccountTransferFlowsThroughAccountMovementsAndBalanceAnalysis(t *testing.T) {
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

	memberID := uuid.NewV7().String()
	memberUUID := generatedUUID(memberID)
	subject, memberName := "accounting-e2e:"+memberID, "Accounting integration"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &memberName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()

	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: memberName})
	apiResolver := &Resolver{DB: db}
	mutation := &mutationResolver{Resolver: apiResolver}
	household, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "Account transfer end-to-end"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(generatedUUID(household.ID))))).Exec(t.Context(), bobpgx.NewPool(db))
	}()

	openingAt := model.DateTime("2026-05-01T00:00:00Z")
	fromAccount, err := mutation.createAccount(ctx, model.CreateAccountInput{
		HouseholdID: household.ID, Name: testOperatingAccountName, Kind: model.AccountKindBank,
		OpeningBalance: &model.AccountOpeningBalanceInput{AmountYen: model.Long(1000), At: openingAt},
	})
	if err != nil {
		t.Fatal(err)
	}
	toAccount, err := mutation.createAccount(ctx, model.CreateAccountInput{
		HouseholdID: household.ID, Name: "Reserve", Kind: model.AccountKindBank,
		OpeningBalance: &model.AccountOpeningBalanceInput{AmountYen: model.Long(300), At: openingAt},
	})
	if err != nil {
		t.Fatal(err)
	}

	movement, err := mutation.recordAccountMovement(ctx, model.RecordAccountMovementInput{
		HouseholdID: household.ID, Kind: model.AccountMovementKindTransfer,
		FromAccountID: fromAccount.ID, ToAccountID: toAccount.ID, AmountYen: 250,
		OccurredAt: "2026-06-10T12:00:00Z", Description: testAccountTransferDescription, IdempotencyKey: uuid.NewV7().String(),
	})
	if err != nil {
		t.Fatal(err)
	}

	movements, err := apiResolver.Query().AccountMovements(ctx, household.ID, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var queriedMovement *model.AccountMovement
	for _, edge := range movements.Edges {
		if edge.Node.ID == movement.ID {
			queriedMovement = edge.Node
			break
		}
	}
	if queriedMovement == nil || queriedMovement.AmountYen != 250 || queriedMovement.Description != testAccountTransferDescription {
		t.Fatalf("account movement query did not return the transfer: movement=%#v connection=%#v", queriedMovement, movements)
	}

	storedMovement, err := models.AccountMovements.Query(sm.Where(models.AccountMovements.Columns.ID.EQ(psql.Arg(generatedUUID(movement.ID))))).One(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	journalEntries, err := models.JournalEntries.Query(
		sm.Where(models.JournalEntries.Columns.JournalID.EQ(psql.Arg(storedMovement.JournalID))),
		sm.OrderBy(models.JournalEntries.Columns.LineNo),
	).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	var debits, credits int64
	for _, entry := range journalEntries {
		debits += entry.DebitYen
		credits += entry.CreditYen
	}
	if len(journalEntries) != 2 || debits != 250 || credits != 250 {
		t.Fatalf("transfer journal must balance at the movement amount: entries=%#v debits=%d credits=%d", journalEntries, debits, credits)
	}
	if !journalEntries[0].AccountID.Valid || journalEntries[0].AccountID.V != generatedUUID(toAccount.ID) || journalEntries[0].DebitYen != 250 ||
		!journalEntries[1].AccountID.Valid || journalEntries[1].AccountID.V != generatedUUID(fromAccount.ID) || journalEntries[1].CreditYen != 250 {
		t.Fatalf("transfer journal must debit the destination and credit the source: entries=%#v", journalEntries)
	}

	for _, expected := range []struct {
		accountID string
		opening   int64
		change    int64
		closing   int64
	}{
		{accountID: fromAccount.ID, opening: 1000, change: -250, closing: 750},
		{accountID: toAccount.ID, opening: 300, change: 250, closing: 550},
	} {
		analysis, err := apiResolver.Query().AccountBalanceAnalysis(ctx, household.ID, expected.accountID, "2026-06", "2026-06", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if analysis.OpeningBalanceYen != model.Long(expected.opening) || analysis.ClosingBalanceYen != model.Long(expected.closing) ||
			len(analysis.Changes.Edges) != 1 || analysis.Changes.Edges[0].Node.ChangeYen != model.Long(expected.change) || accountBalanceSourceType(analysis.Changes.Edges[0].Node.Source) != graphQLAccountMovementType {
			t.Fatalf("account analysis did not reflect its side of the transfer: account=%s analysis=%#v", expected.accountID, analysis)
		}
	}
	assertHouseholdJournalsBalance(t, ctx, db, generatedUUID(household.ID))
}

func assertHouseholdJournalsBalance(t *testing.T, ctx context.Context, db *pgxpool.Pool, householdID gofrsuuid.UUID) {
	t.Helper()
	executor := bobpgx.NewPool(db)
	journals, err := models.Journals.Query(sm.Where(models.Journals.Columns.HouseholdID.EQ(psql.Arg(householdID)))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if len(journals) == 0 {
		t.Fatal("household has no journals to verify")
	}
	type journalBalance struct {
		debit, credit int64
		entries       int
	}
	journalIDs := make([]bob.Expression, 0, len(journals))
	balances := make(map[gofrsuuid.UUID]*journalBalance, len(journals))
	for _, journal := range journals {
		journalIDs = append(journalIDs, psql.Arg(journal.ID))
		balances[journal.ID] = &journalBalance{}
	}
	entries, err := models.JournalEntries.Query(sm.Where(models.JournalEntries.Columns.JournalID.In(journalIDs...))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		balance := balances[entry.JournalID]
		balance.debit += entry.DebitYen
		balance.credit += entry.CreditYen
		balance.entries++
	}
	for _, journal := range journals {
		balance := balances[journal.ID]
		if balance.debit != balance.credit {
			t.Errorf("journal %s (%s) is unbalanced: debit=%d credit=%d", journal.ID, journal.SourceKind, balance.debit, balance.credit)
		}
		if balance.entries == 0 {
			t.Errorf("journal %s (%s) has no entries", journal.ID, journal.SourceKind)
		}
	}
	items, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.HouseholdID.EQ(psql.Arg(householdID)))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	allocations, err := models.ItemAllocations.Query(sm.Where(models.ItemAllocations.Columns.HouseholdID.EQ(psql.Arg(householdID)))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	type itemAllocationTotal struct {
		amount int64
		count  int
	}
	totals := make(map[gofrsuuid.UUID]itemAllocationTotal, len(items))
	for _, allocation := range allocations {
		total := totals[allocation.ItemID]
		total.amount += allocation.AmountYen
		total.count++
		totals[allocation.ItemID] = total
	}
	for _, item := range items {
		total := totals[item.ID]
		if total.count == 0 || total.amount != item.AmountYen {
			t.Errorf("purchase item %s allocation total does not match its amount: item=%d allocated=%d rows=%d", item.ID, item.AmountYen, total.amount, total.count)
		}
	}
}

func TestAccountOpeningBalancesCreateBalancedAssetAndLiabilityJournals(t *testing.T) {
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

	memberID := uuid.NewV7().String()
	memberUUID := generatedUUID(memberID)
	subject, memberName := "account-opening-integration:"+memberID, "Account opening integration"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &memberName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: memberName})
	resolver := &mutationResolver{Resolver: (&Resolver{DB: db})}
	household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "Account opening integration"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(generatedUUID(household.ID))))).Exec(t.Context(), bobpgx.NewPool(db))
	}()

	openingAt := model.DateTime("2026-07-15T12:00:00Z")
	for _, expected := range []struct {
		name          string
		kind          model.AccountKind
		amount        model.Long
		accountDebit  bool
		accountCredit bool
	}{
		{name: "Bank", kind: model.AccountKindBank, amount: 1200, accountDebit: true},
		{name: "Card", kind: model.AccountKindCard, amount: 850, accountCredit: true},
	} {
		account, err := resolver.createAccount(ctx, model.CreateAccountInput{
			HouseholdID: household.ID, Name: expected.name, Kind: expected.kind,
			OpeningBalance: &model.AccountOpeningBalanceInput{AmountYen: expected.amount, At: openingAt},
		})
		if err != nil {
			t.Fatal(err)
		}
		accountUUID := generatedUUID(account.ID)
		journal, err := models.Journals.Query(sm.Where(psql.And(
			models.Journals.Columns.HouseholdID.EQ(psql.Arg(generatedUUID(household.ID))),
			models.Journals.Columns.SourceKind.EQ(psql.Arg(sourceKindAccountOpeningBalance)),
			models.Journals.Columns.SourceID.EQ(psql.Arg(accountUUID)),
		))).One(ctx, bobpgx.NewPool(db))
		if err != nil {
			t.Fatal(err)
		}
		entries, err := models.JournalEntries.Query(
			sm.Where(models.JournalEntries.Columns.JournalID.EQ(psql.Arg(journal.ID))),
			sm.OrderBy(models.JournalEntries.Columns.LineNo),
		).All(ctx, bobpgx.NewPool(db))
		if err != nil {
			t.Fatal(err)
		}
		var debits, credits int64
		accountLines := 0
		for _, entry := range entries {
			debits += entry.DebitYen
			credits += entry.CreditYen
			if entry.AccountID.Valid {
				accountLines++
				if entry.AccountID.V != accountUUID || entry.DebitYen != int64(expected.amount) && entry.CreditYen != int64(expected.amount) {
					t.Fatalf("opening journal account line must match the account and opening amount: account=%#v entries=%#v", account, entries)
				}
				if expected.accountDebit && (entry.DebitYen != int64(expected.amount) || entry.CreditYen != 0) ||
					expected.accountCredit && (entry.CreditYen != int64(expected.amount) || entry.DebitYen != 0) {
					t.Fatalf("opening journal must post the account on its normal balance side: kind=%s entries=%#v", expected.kind, entries)
				}
			}
		}
		if len(entries) != 2 || accountLines != 1 || debits != int64(expected.amount) || credits != int64(expected.amount) {
			t.Fatalf("opening journal must balance exactly at the opening amount: kind=%s entries=%#v debits=%d credits=%d", expected.kind, entries, debits, credits)
		}

		analysis, err := (&Resolver{DB: db}).Query().AccountBalanceAnalysis(ctx, household.ID, account.ID, "2026-07", "2026-07", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if analysis.OpeningBalanceYen != 0 || analysis.ClosingBalanceYen != expected.amount || len(analysis.Changes.Edges) != 1 ||
			analysis.Changes.Edges[0].Node.ChangeYen != expected.amount || accountBalanceSourceType(analysis.Changes.Edges[0].Node.Source) != "AccountOpeningBalance" {
			t.Fatalf("account balance must reflect its opening journal once: kind=%s analysis=%#v", expected.kind, analysis)
		}
	}

	assertHouseholdJournalsBalance(t, ctx, db, generatedUUID(household.ID))
	balanceRows, err := accountBalances(ctx, db, generatedUUID(household.ID))
	if err != nil {
		t.Fatal(err)
	}
	if len(balanceRows) != 2 || balanceRows[0].BalanceYen != 1200 || balanceRows[1].BalanceYen != 850 {
		t.Fatalf("account balances must preserve positive asset and liability magnitudes: %#v", balanceRows)
	}
}

func TestPostedPurchaseCorrectionAffectsNextSettlementWithoutRewritingClosedPeriod(t *testing.T) {
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
		subject := "purchase-correction-e2e:" + id
		if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &name}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(generatedUUID(memberA))))).Exec(t.Context(), bobpgx.NewPool(db))
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(generatedUUID(memberB))))).Exec(t.Context(), bobpgx.NewPool(db))
	}()

	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberA, Subject: "purchase-correction-e2e:" + memberA, DisplayName: "A"})
	apiResolver := &Resolver{DB: db}
	mutation := &mutationResolver{Resolver: apiResolver}
	household, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "Purchase correction settlement"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(generatedUUID(household.ID))))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	householdUUID, memberBUUID := generatedUUID(household.ID), generatedUUID(memberB)
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdUUID, MemberID: &memberBUUID}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}

	personalPool, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "A wallet", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	sharedPool, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: testSharedPoolName, Kind: model.PoolKindShared})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{HouseholdID: household.ID, PoolID: personalPool.ID, Configuration: personalPoolFundingConfiguration(memberA), ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	policy := model.SharedPoolSettlementPolicyBalanceToStandard
	if _, err := mutation.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{
		HouseholdID: household.ID, PoolID: sharedPool.ID,
		Configuration:   sharedPoolFundingConfiguration([]*model.PoolFundingShareInput{{MemberID: memberA, Weight: 60}, {MemberID: memberB, Weight: 40}}, policy),
		ExpectedVersion: 1,
	}); err != nil {
		t.Fatal(err)
	}
	account, err := mutation.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "A card", Kind: model.AccountKindCard, PoolID: &personalPool.ID})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(accountingLocation)
	currentMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, accountingLocation)
	originalMonth := currentMonth.AddDate(0, -1, 0)
	purchaseAt := time.Date(originalMonth.Year(), originalMonth.Month(), 10, 12, 0, 0, 0, accountingLocation)
	originalPeriod, correctionPeriod := accountingPeriod(purchaseAt), accountingPeriod(now)
	purchase, err := mutation.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: model.DateTime(purchaseAt.UTC().Format(time.RFC3339)), Merchant: testMerchant,
		PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: account.ID, AmountYen: 5000}},
		Items:              []*model.PurchaseItemInput{{Name: "Shared item", AmountYen: 5000, Allocations: []*model.AllocationInput{{PoolID: sharedPool.ID, Weight: 1}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	posted, err := mutation.postPurchase(ctx, purchase.ID, purchase.Revision, uuid.NewV7().String())
	if err != nil {
		t.Fatal(err)
	}
	closed, err := mutation.closePeriod(ctx, model.ClosePeriodInput{HouseholdID: household.ID, Period: model.YearMonth(originalPeriod), ExpectedRevision: 0})
	if err != nil {
		t.Fatal(err)
	}
	closedLines := settlementByMember(t, settlementData(t, closed).Lines)
	if closedLines[memberA].AdvanceCreditYen != 2000 || closedLines[memberA].AmountDueYen != -2000 || closedLines[memberB].MonthlyAdjustmentYen != 2000 || closedLines[memberB].AmountDueYen != 2000 {
		t.Fatalf("closed settlement did not capture the original purchase allocation: %#v", closedLines)
	}
	purchaseNodeID, err := encodeNodeID("Purchase", purchase.ID)
	if err != nil {
		t.Fatal(err)
	}
	var purchaseEvidenceFound bool
	for _, line := range settlementData(t, closed).Lines {
		for _, evidence := range line.Evidence {
			if evidence.Kind == "PURCHASE" && evidence.PurchaseID != nil && *evidence.PurchaseID == purchaseNodeID {
				purchaseEvidenceFound = true
			}
		}
	}
	if !purchaseEvidenceFound {
		t.Fatalf("closed settlement did not expose a navigable purchase source: %#v", closed)
	}
	changedAmount := model.Long(4500)
	if _, err := mutation.updatePurchaseItem(ctx, model.UpdatePurchaseItemInput{
		ItemID: posted.Items[0].ID, BaseRevision: posted.Items[0].Revision, AmountYen: omittable(&changedAmount),
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("editing a purchase after closing its period returned %v; want ErrConflict", err)
	}

	if _, err := mutation.correctPurchase(ctx, model.CorrectPurchaseInput{
		HouseholdID: household.ID, Target: &model.CorrectPurchaseTargetInput{Item: &model.PurchaseCorrectionItemTargetInput{PurchaseID: purchase.ID, ItemID: posted.Items[0].ID}},
		AmountDeltaYen: -2000, Reason: testCorrectionRefundReason, IdempotencyKey: uuid.NewV7().String(),
	}); err != nil {
		t.Fatal(err)
	}
	corrections, err := models.Corrections.Query(sm.Where(models.Corrections.Columns.PurchaseID.EQ(psql.Arg(generatedUUID(purchase.ID))))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(corrections) != 1 || !corrections[0].EffectivePeriod.Valid || corrections[0].EffectivePeriod.V != correctionPeriod || correctionPeriod == originalPeriod {
		t.Fatalf("correction was not assigned to the open period after closing the purchase month: corrections=%#v error=%v", corrections, err)
	}
	correctionEntries, err := models.JournalEntries.Query(sm.Where(models.JournalEntries.Columns.JournalID.EQ(psql.Arg(corrections[0].JournalID)))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	var correctionDebits, correctionCredits int64
	for _, entry := range correctionEntries {
		correctionDebits += entry.DebitYen
		correctionCredits += entry.CreditYen
	}
	if len(correctionEntries) != 2 || correctionDebits != 2000 || correctionCredits != 2000 {
		t.Fatalf("correction journal must balance at the refund amount: entries=%#v debits=%d credits=%d", correctionEntries, correctionDebits, correctionCredits)
	}
	assertHouseholdJournalsBalance(t, ctx, db, generatedUUID(household.ID))
	ledgerEntries, err := models.LedgerEntries.Query(sm.Where(psql.And(
		models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.LedgerEntries.Columns.Kind.EQ(psql.Arg(sourceKindCorrection)),
		models.LedgerEntries.Columns.SourcePurchaseID.EQ(psql.Arg(generatedUUID(purchase.ID))),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(ledgerEntries) != 1 || ledgerEntries[0].JournalID != corrections[0].JournalID || ledgerEntries[0].AmountYen != 2000 {
		t.Fatalf("correction ledger entry did not capture the refund: entries=%#v error=%v", ledgerEntries, err)
	}

	closedAgain, err := apiResolver.Query().Settlement(ctx, household.ID, model.YearMonth(originalPeriod))
	if err != nil {
		t.Fatal(err)
	}
	closedData := settlementData(t, closed)
	closedAgainData := settlementData(t, closedAgain)
	if _, ok := closedAgain.(*model.ClosedSettlement); !ok || closedAgainData.Revision != closedData.Revision {
		t.Fatalf("correction changed the closed settlement revision: before=%#v after=%#v", closed, closedAgain)
	}
	closedAgainLines := settlementByMember(t, closedAgainData.Lines)
	for memberID, before := range closedLines {
		after := closedAgainLines[memberID]
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("correction rewrote the closed settlement for member %s: before=%#v after=%#v", memberID, before, after)
		}
	}

	query := apiResolver.Query()
	memberSpending, err := query.MonthlyMemberSpending(ctx, household.ID, model.YearMonth(originalPeriod), model.YearMonth(originalPeriod))
	if err != nil {
		t.Fatal(err)
	}
	memberAmounts := make(map[string]int64, len(memberSpending.Items))
	for _, item := range memberSpending.Items {
		if string(item.Period) != originalPeriod {
			t.Fatalf("correction moved member spending out of the purchase month: %#v", memberSpending.Items)
		}
		memberAmounts[item.MemberID] = int64(item.AmountYen)
	}
	if memberAmounts[memberA] != 1800 || memberAmounts[memberB] != 1200 {
		t.Fatalf("purchase-month member analytics did not retain the corrected historical burden: %#v", memberSpending.Items)
	}
	categorySpending, err := query.MonthlyCategorySpending(ctx, household.ID, model.YearMonth(originalPeriod), model.YearMonth(originalPeriod))
	if err != nil {
		t.Fatal(err)
	}
	if len(categorySpending) != 1 || string(categorySpending[0].Period) != originalPeriod || int64(categorySpending[0].AmountYen) != 3000 {
		t.Fatalf("purchase-month category analytics did not retain the correction: %#v", categorySpending)
	}

	nextSettlement, err := query.Settlement(ctx, household.ID, model.YearMonth(correctionPeriod))
	if err != nil {
		t.Fatal(err)
	}
	nextLines := settlementByMember(t, settlementData(t, nextSettlement).Lines)
	if nextLines[memberA].MonthlyAdjustmentYen != 800 || nextLines[memberA].PriorBalanceYen != -2000 || nextLines[memberA].AmountDueYen != -1200 ||
		nextLines[memberB].MonthlyAdjustmentYen != -800 || nextLines[memberB].PriorBalanceYen != 2000 || nextLines[memberB].AmountDueYen != 1200 {
		t.Fatalf("next settlement did not apply the correction while carrying the closed balance: %#v", nextLines)
	}
	accountAnalysis, err := query.AccountBalanceAnalysis(ctx, household.ID, account.ID, model.YearMonth(correctionPeriod), model.YearMonth(correctionPeriod), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(accountAnalysis.Changes.Edges) != 1 || accountAnalysis.Changes.Edges[0].Node.ChangeYen != -2000 || accountAnalysis.ClosingBalanceYen != accountAnalysis.OpeningBalanceYen-2000 ||
		accountBalanceSourceType(accountAnalysis.Changes.Edges[0].Node.Source) != "PurchaseItemCorrection" {
		t.Fatalf("correction period card balance analysis did not reflect the refund: %#v", accountAnalysis)
	}
}

func TestConcurrentLoanRepaymentsCannotExceedPrincipal(t *testing.T) {
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

	lenderID, borrowerID := uuid.NewV7().String(), uuid.NewV7().String()
	lenderUUID, borrowerUUID := generatedUUID(lenderID), generatedUUID(borrowerID)
	lenderSubject, borrowerSubject := "loan-concurrency:"+lenderID, "loan-concurrency:"+borrowerID
	lenderName, borrowerName := testLenderName, testBorrowerName
	executor := bobpgx.NewPool(db)
	for _, member := range []struct {
		id      *gofrsuuid.UUID
		subject *string
		name    *string
	}{{&lenderUUID, &lenderSubject, &lenderName}, {&borrowerUUID, &borrowerSubject, &borrowerName}} {
		if _, err := models.Members.Insert(&models.MemberSetter{ID: member.id, OidcSubject: member.subject, DisplayName: member.name}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(lenderUUID)))).Exec(t.Context(), executor)
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(borrowerUUID)))).Exec(t.Context(), executor)
	}()

	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: lenderID, Subject: lenderSubject, DisplayName: lenderName})
	mutation := &mutationResolver{Resolver: &Resolver{DB: db}}
	household, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "Loan repayment concurrency"})
	if err != nil {
		t.Fatal(err)
	}
	householdUUID := generatedUUID(household.ID)
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(t.Context(), executor)
	}()
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdUUID, MemberID: &borrowerUUID}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	loan, err := mutation.recordTransfer(ctx, model.RecordTransferInput{
		HouseholdID: household.ID, FromMemberID: lenderID, ToMemberID: borrowerID, Kind: model.TransferInputKindLoan,
		AmountYen: 100, OccurredAt: "2026-08-01T12:00:00Z", Description: "Loan", IdempotencyKey: uuid.NewV7().String(),
	})
	if err != nil || loan.TransferID == nil {
		t.Fatalf("loan creation failed: transfer=%#v error=%v", loan, err)
	}

	start := make(chan struct{})
	ready := make(chan struct{}, 2)
	type repaymentResult struct {
		result model.RecordRepaymentResult
		err    error
	}
	results := make(chan repaymentResult, 2)
	for _, period := range []string{"2026-09", testOctober2026} {
		go func() {
			ready <- struct{}{}
			<-start
			result, err := mutation.RecordRepayment(ctx, model.RecordRepaymentInput{
				HouseholdID: household.ID, FromMemberID: borrowerID, ToMemberID: lenderID, LoanID: *loan.TransferID,
				AmountYen: 60, OccurredAt: model.DateTime(period + "-15T12:00:00Z"), Description: "Repayment", IdempotencyKey: uuid.NewV7().String(),
			})
			results <- repaymentResult{result: result, err: err}
		}()
	}
	for range 2 {
		<-ready
	}
	close(start)
	var succeeded, rejected int
	for range 2 {
		outcome := <-results
		if outcome.err != nil {
			t.Fatalf("repayment resolver returned an unexpected error: %v", outcome.err)
		}
		switch result := outcome.result.(type) {
		case *model.RecordRepaymentSuccess:
			succeeded++
		case *model.RecordRepaymentUserError:
			if result.Code != model.MutationErrorCodeBadInput {
				t.Fatalf("unexpected repayment error result: %#v", result)
			}
			rejected++
		default:
			t.Fatalf("unexpected concurrent repayment result: %#v", outcome.result)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("expected one repayment to succeed and the other to exceed the remaining balance: succeeded=%d rejected=%d", succeeded, rejected)
	}

	loanUUID := generatedUUID(*loan.TransferID)
	repayments, err := models.Transfers.Query(sm.Where(psql.And(
		models.Transfers.Columns.LoanID.EQ(psql.Arg(sql.Null[gofrsuuid.UUID]{V: loanUUID, Valid: true})),
		models.Transfers.Columns.Kind.EQ(psql.Arg("REPAYMENT")),
	))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	var repaid int64
	for _, repayment := range repayments {
		repaid += repayment.AmountYen
	}
	if len(repayments) != 1 || repaid != 60 || repaid > 100 {
		t.Fatalf("repayment rows must remain within the loan principal: rows=%#v total=%d", repayments, repaid)
	}
	assertHouseholdJournalsBalance(t, ctx, db, householdUUID)
}
