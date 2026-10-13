package graph

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"uuid"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestAccountTransferFlowsThroughAccountMovementsAndBalanceAnalysis(t *testing.T) {
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

	openingAt := model.DateTime("2026-05-01T00:00:00Z")
	fromAccount, err := mutation.createAccount(ctx, model.CreateAccountInput{
		HouseholdID: household.ID, Name: "Operating", Kind: model.AccountKindBank,
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
		OccurredAt: "2026-06-10T12:00:00Z", Description: "Reserve transfer", IdempotencyKey: uuid.NewV7().String(),
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
	if queriedMovement == nil || queriedMovement.AmountYen != 250 || queriedMovement.Description != "Reserve transfer" {
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
		analysis, err := apiResolver.Query().AccountBalanceAnalysis(ctx, household.ID, expected.accountID, "2026-06", "2026-06")
		if err != nil {
			t.Fatal(err)
		}
		if analysis.OpeningBalanceYen != model.Long(expected.opening) || analysis.ClosingBalanceYen != model.Long(expected.closing) ||
			len(analysis.Changes) != 1 || analysis.Changes[0].ChangeYen != model.Long(expected.change) || accountBalanceSourceType(analysis.Changes[0].Source) != "AccountMovement" {
			t.Fatalf("account analysis did not reflect its side of the transfer: account=%s analysis=%#v", expected.accountID, analysis)
		}
	}
}

func TestConcurrentLoanRepaymentsCannotExceedPrincipal(t *testing.T) {
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
	lenderName, borrowerName := "Lender", "Borrower"
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
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(lenderUUID)))).Exec(context.Background(), executor)
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(borrowerUUID)))).Exec(context.Background(), executor)
	}()

	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: lenderID, Subject: lenderSubject, DisplayName: lenderName})
	mutation := &mutationResolver{Resolver: &Resolver{DB: db}}
	household, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "Loan repayment concurrency"})
	if err != nil {
		t.Fatal(err)
	}
	householdUUID := generatedUUID(household.ID)
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(context.Background(), executor)
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
	results := make(chan error, 2)
	for _, period := range []string{"2026-09", "2026-10"} {
		go func() {
			ready <- struct{}{}
			<-start
			_, err := mutation.recordRepayment(ctx, model.RecordRepaymentInput{
				HouseholdID: household.ID, FromMemberID: borrowerID, ToMemberID: lenderID, LoanID: *loan.TransferID,
				AmountYen: 60, OccurredAt: model.DateTime(period + "-15T12:00:00Z"), Description: "Repayment", IdempotencyKey: uuid.NewV7().String(),
			})
			results <- err
		}()
	}
	for range 2 {
		<-ready
	}
	close(start)
	var succeeded, rejected int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrBadInput):
			rejected++
		default:
			t.Fatalf("unexpected concurrent repayment result: %v", err)
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
}
