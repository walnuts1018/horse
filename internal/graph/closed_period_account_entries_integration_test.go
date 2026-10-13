package graph

import (
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

func TestClosedPeriodRejectsAccountMovementsAndOpeningBalances(t *testing.T) {
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
	memberUUID, _ := gofrsuuid.FromString(memberID)
	subject, memberName := "closed-period-account-entries:"+memberID, "Closed period"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &memberName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: memberName})
	mutation := &mutationResolver{Resolver: (&Resolver{DB: db})}
	household, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "Closed period account entries"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(generatedUUID(household.ID))))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	from, err := mutation.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Source", Kind: model.AccountKindBank})
	if err != nil {
		t.Fatal(err)
	}
	to, err := mutation.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Destination", Kind: model.AccountKindBank})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.closePeriod(ctx, model.ClosePeriodInput{HouseholdID: household.ID, Period: testApril2026, ExpectedRevision: 0}); err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.recordAccountMovement(ctx, model.RecordAccountMovementInput{
		HouseholdID: household.ID, Kind: model.AccountMovementKindTransfer, FromAccountID: from.ID, ToAccountID: to.ID,
		AmountYen: 500, OccurredAt: "2026-04-15T12:00:00Z", IdempotencyKey: uuid.NewV7().String(),
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("closed-period account movement should fail with conflict: %v", err)
	}
	if _, err := mutation.createAccount(ctx, model.CreateAccountInput{
		HouseholdID: household.ID, Name: "Opening balance in closed period", Kind: model.AccountKindBank,
		OpeningBalance: &model.AccountOpeningBalanceInput{AmountYen: model.Long(1200), At: "2026-04-15T12:00:00Z"},
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("closed-period opening balance should fail with conflict: %v", err)
	}
	incomeKey := uuid.NewV7().String()
	if _, err := mutation.recordExternalIncome(ctx, model.RecordExternalIncomeInput{
		HouseholdID: household.ID, AccountID: to.ID, AmountYen: 800, OccurredAt: "2026-04-15T12:00:00Z",
		Description: "Closed-period income", IdempotencyKey: incomeKey,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("closed-period external income should fail with conflict: %v", err)
	}
	accounts, err := models.Accounts.Query(sm.Where(models.Accounts.Columns.HouseholdID.EQ(psql.Arg(generatedUUID(household.ID))))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	movements, err := models.AccountMovements.Query(sm.Where(models.AccountMovements.Columns.HouseholdID.EQ(psql.Arg(generatedUUID(household.ID))))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	householdUUID := generatedUUID(household.ID)
	incomes, err := models.AccountIncomes.Query(sm.Where(models.AccountIncomes.Columns.HouseholdID.EQ(psql.Arg(householdUUID)))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	journals, err := models.Journals.Query(sm.Where(psql.And(
		models.Journals.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.Journals.Columns.SourceKind.EQ(psql.Arg(sourceKindAccountIncome)),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	idempotencyKeys, err := models.IdempotencyKeys.Query(sm.Where(psql.And(
		models.IdempotencyKeys.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.IdempotencyKeys.Columns.Key.EQ(psql.Arg(incomeKey)),
		models.IdempotencyKeys.Columns.Operation.EQ(psql.Arg("recordExternalIncome")),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	events, err := models.HouseholdEvents.Query(sm.Where(psql.And(
		models.HouseholdEvents.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.HouseholdEvents.Columns.Type.EQ(psql.Arg("account.income.recorded")),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 || len(movements) != 0 || len(incomes) != 0 || len(journals) != 0 || len(idempotencyKeys) != 0 || len(events) != 0 {
		t.Fatalf("rejected entries must leave no account, movement, income, journal, idempotency key, or event behind: accounts=%d movements=%d incomes=%d journals=%d idempotency_keys=%d events=%d", len(accounts), len(movements), len(incomes), len(journals), len(idempotencyKeys), len(events))
	}
}
