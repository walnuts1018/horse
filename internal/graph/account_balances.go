package graph

import (
	"context"
	"fmt"
	"strconv"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/scan"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (r *queryResolver) accountBalances(ctx context.Context, householdID string) ([]*model.AccountBalance, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, ErrNotFound
	}
	return accountBalances(ctx, r.DB, hhID)
}

func accountBalances(ctx context.Context, db *pgxpool.Pool, householdID gofrsuuid.UUID) ([]*model.AccountBalance, error) {
	executor := bobpgx.NewPool(db)
	cardBalance := models.JournalEntries.Columns.CreditYen.Minus(models.JournalEntries.Columns.DebitYen)
	otherBalance := models.JournalEntries.Columns.DebitYen.Minus(models.JournalEntries.Columns.CreditYen)
	signedBalance := psql.Case().
		When(models.Accounts.Columns.Kind.EQ(psql.Arg(string(model.AccountKindCard))), cardBalance).
		Else(otherBalance)
	openingQuery := psql.Select(
		sm.Columns(
			models.Journals.Columns.SourceID.As("account_id"),
			psql.F("max", models.Journals.Columns.OccurredAt)().As("occurred_at"),
			psql.F("sum", signedBalance)().As("balance_yen"),
		),
		sm.From(models.JournalEntries.NameExpr(),
			sm.InnerJoin(models.Journals.NameExpr()).On(models.JournalEntries.Columns.JournalID.EQ(models.Journals.Columns.ID)),
			sm.InnerJoin(models.Accounts.NameExpr()).On(models.JournalEntries.Columns.AccountID.EQ(models.Accounts.Columns.ID)),
		),
		sm.Where(psql.And(
			models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Journals.Columns.SourceKind.EQ(psql.Arg("ACCOUNT_OPENING_BALANCE")),
			models.Journals.Columns.SourceID.EQ(models.Accounts.Columns.ID),
		)),
		sm.GroupBy(models.Journals.Columns.SourceID),
	)
	isOpeningEntry := psql.And(
		models.Journals.Columns.SourceKind.EQ(psql.Arg("ACCOUNT_OPENING_BALANCE")),
		models.Journals.Columns.SourceID.EQ(models.Accounts.Columns.ID),
	)
	afterOpening := psql.Or(
		psql.Quote("opening", "occurred_at").IsNull(),
		models.Journals.Columns.OccurredAt.GTE(psql.Quote("opening", "occurred_at")),
	)
	includedMovement := psql.And(psql.Not(isOpeningEntry), afterOpening)
	openingBalance := psql.F("coalesce", psql.F("max", psql.Quote("opening", "balance_yen"))(), psql.Arg(int64(0)))()
	movementBalance := psql.F("coalesce", psql.F("sum",
		psql.Case().When(includedMovement, signedBalance).Else(psql.Arg(int64(0))),
	)(), psql.Arg(int64(0)))()
	rows, err := bob.All(ctx, executor, psql.Select(
		sm.With("opening", "account_id", "occurred_at", "balance_yen").As(openingQuery),
		sm.Columns(
			models.Accounts.Columns.ID.As("account_id"),
			openingBalance.As("opening_balance"),
			movementBalance.As("movement_balance"),
		),
		sm.From(models.Accounts.NameExpr(),
			sm.LeftJoin(models.JournalEntries.NameExpr()).On(models.JournalEntries.Columns.AccountID.EQ(models.Accounts.Columns.ID)),
			sm.LeftJoin(models.Journals.NameExpr()).On(models.JournalEntries.Columns.JournalID.EQ(models.Journals.Columns.ID)),
			sm.LeftJoin("opening").On(psql.Quote("opening", "account_id").EQ(models.Accounts.Columns.ID)),
		),
		sm.Where(models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID))),
		sm.GroupBy(models.Accounts.Columns.ID),
		sm.OrderBy(models.Accounts.Columns.Name),
		sm.OrderBy(models.Accounts.Columns.ID),
	), scan.StructMapper[accountBalanceAggregate]())
	if err != nil {
		return nil, err
	}
	result := make([]*model.AccountBalance, 0, len(rows))
	for _, row := range rows {
		opening, err := strconv.ParseInt(row.OpeningBalance, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: account balance exceeds int64", ErrBadInput)
		}
		movement, err := strconv.ParseInt(row.MovementBalance, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: account balance exceeds int64", ErrBadInput)
		}
		balance, ok := checkedAdd(opening, movement)
		if !ok {
			return nil, fmt.Errorf("%w: account balance exceeds int64", ErrBadInput)
		}
		result = append(result, &model.AccountBalance{AccountID: row.AccountID.String(), BalanceYen: graphLong(balance), Currency: "JPY"})
	}
	return result, nil
}

type accountBalanceAggregate struct {
	AccountID       gofrsuuid.UUID `db:"account_id"`
	OpeningBalance  string         `db:"opening_balance"`
	MovementBalance string         `db:"movement_balance"`
}

func accountEntryBalance(kind string, debitYen, creditYen int64) int64 {
	if kind == string(model.AccountKindCard) {
		return creditYen - debitYen
	}
	return debitYen - creditYen
}
