package graph

import (
	"context"
	"database/sql"
	"fmt"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
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
	accounts, err := models.Accounts.Query(
		sm.Where(models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID))),
		sm.OrderBy(models.Accounts.Columns.Name),
		sm.OrderBy(models.Accounts.Columns.ID),
	).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	result := make([]*model.AccountBalance, 0, len(accounts))
	for _, account := range accounts {
		entries, err := models.JournalEntries.Query(sm.Where(models.JournalEntries.Columns.AccountID.EQ(psql.Arg(sql.Null[gofrsuuid.UUID]{V: account.ID, Valid: true})))).All(ctx, executor)
		if err != nil {
			return nil, err
		}
		if err := entries.LoadJournal(ctx, executor); err != nil {
			return nil, err
		}
		var openingBalance int64
		var openingAtPresent bool
		var openingAt = account.CreatedAt
		for _, entry := range entries {
			journal := entry.R.Journal
			if journal == nil || journal.SourceKind != "ACCOUNT_OPENING_BALANCE" || journal.SourceID != account.ID {
				continue
			}
			openingAtPresent = true
			openingAt = journal.OccurredAt
			openingBalance = accountEntryBalance(account.Kind, entry.DebitYen, entry.CreditYen)
		}
		balance := openingBalance
		for _, entry := range entries {
			journal := entry.R.Journal
			if journal == nil || journal.SourceKind == "ACCOUNT_OPENING_BALANCE" && journal.SourceID == account.ID {
				continue
			}
			if openingAtPresent && journal.OccurredAt.Before(openingAt) {
				continue
			}
			delta := accountEntryBalance(account.Kind, entry.DebitYen, entry.CreditYen)
			updatedBalance, ok := checkedAdd(balance, delta)
			if !ok {
				return nil, fmt.Errorf("%w: account balance exceeds int64", ErrBadInput)
			}
			balance = updatedBalance
		}
		result = append(result, &model.AccountBalance{AccountID: account.ID.String(), BalanceYen: graphLong(balance), Currency: "JPY"})
	}
	return result, nil
}

func accountEntryBalance(kind string, debitYen, creditYen int64) int64 {
	if kind == string(model.AccountKindCard) {
		return creditYen - debitYen
	}
	return debitYen - creditYen
}
