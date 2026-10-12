package graph

import (
	"context"
	"os"
	"testing"

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

func TestAccountTransferFlowsThroughLedgerAndBalanceAnalysis(t *testing.T) {
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
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(context.Background(), bobpgx.NewPool(db))
	}()

	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: memberName})
	apiResolver := &Resolver{DB: db}
	mutation := &mutationResolver{Resolver: apiResolver}
	household, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "Account transfer end-to-end"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(generatedUUID(household.ID))))).Exec(context.Background(), bobpgx.NewPool(db))
	}()

	openingAt := "2026-05-01T00:00:00Z"
	fromAccount, err := mutation.createAccount(ctx, model.CreateAccountInput{
		HouseholdID: household.ID, Name: "Operating", Kind: model.AccountKindBank,
		OpeningBalanceYen: new(model.Long(1000)), OpeningBalanceAt: &openingAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	toAccount, err := mutation.createAccount(ctx, model.CreateAccountInput{
		HouseholdID: household.ID, Name: "Reserve", Kind: model.AccountKindBank,
		OpeningBalanceYen: new(model.Long(300)), OpeningBalanceAt: &openingAt,
	})
	if err != nil {
		t.Fatal(err)
	}

	movement, err := mutation.recordAccountMovement(ctx, model.RecordAccountMovementInput{
		HouseholdID: household.ID, Kind: model.AccountMovementKindTransfer,
		FromAccountID: fromAccount.ID, ToAccountID: toAccount.ID, AmountYen: 250,
		OccurredAt: "2026-06-10T12:00:00Z", Description: "Reserve transfer", IdempotencyKey: uuid.NewV7().String(),
	})
	if err != nil {
		t.Fatal(err)
	}

	ledger, err := apiResolver.Query().Ledger(ctx, household.ID, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var ledgerMovement *model.TransferLedgerEntry
	for _, edge := range ledger.Edges {
		if edge.Node.GetID() == movement.ID {
			ledgerMovement, _ = edge.Node.(*model.TransferLedgerEntry)
			break
		}
	}
	if ledgerMovement == nil || ledgerMovement.AmountYen != 250 || ledgerMovement.Description != "Reserve transfer" {
		t.Fatalf("account movement was not represented in the ledger: movement=%#v ledger=%#v", ledgerMovement, ledger)
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
		analysis, err := apiResolver.Query().AccountBalanceAnalysis(ctx, household.ID, expected.accountID, "2026-06", "2026-06")
		if err != nil {
			t.Fatal(err)
		}
		if analysis.OpeningBalanceYen != model.Long(expected.opening) || analysis.ClosingBalanceYen != model.Long(expected.closing) ||
			len(analysis.Changes) != 1 || analysis.Changes[0].ChangeYen != model.Long(expected.change) || analysis.Changes[0].SourceKind != "TRANSFER" {
			t.Fatalf("account analysis did not reflect its side of the transfer: account=%s analysis=%#v", expected.accountID, analysis)
		}
	}
}
