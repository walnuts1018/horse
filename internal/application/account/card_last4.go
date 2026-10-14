package account

import (
	"context"
	"database/sql"
	"errors"

	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/application/txcleanup"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type SetCardLast4Command struct {
	ActorID   string
	AccountID string
	CardLast4 *string
}

func (s *Service) SetCardLast4(ctx context.Context, command SetCardLast4Command) (CreatedAccount, error) {
	if err := validateCardLast4(cardAccountKind, command.CardLast4); err != nil {
		return CreatedAccount{}, err
	}
	actorID, err := parseID(command.ActorID, "actor")
	if err != nil {
		return CreatedAccount{}, err
	}
	accountID, err := parseID(command.AccountID, "account")
	if err != nil {
		return CreatedAccount{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CreatedAccount{}, err
	}
	defer txcleanup.Rollback(ctx, tx)
	executor := bobpgx.NewTx(tx, nil)
	account, err := models.Accounts.Query(sm.Where(models.Accounts.Columns.ID.EQ(psql.Arg(accountID))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return CreatedAccount{}, apperror.ErrNotFound
	} else if err != nil {
		return CreatedAccount{}, err
	}
	member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(account.HouseholdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(actorID)),
	))).Exists(ctx, executor)
	if err != nil {
		return CreatedAccount{}, err
	}
	if !member {
		return CreatedAccount{}, apperror.ErrNotFound
	}
	if account.Kind != cardAccountKind {
		return CreatedAccount{}, apperror.Wrap(apperror.KindBadInput, errors.New("card last four digits require a card account"))
	}
	if !sameLast4(account.CardLast4, command.CardLast4) {
		var cardLast4 sql.Null[string]
		if command.CardLast4 != nil {
			cardLast4 = sql.Null[string]{V: *command.CardLast4, Valid: true}
		}
		if _, err := models.Accounts.Update((&models.AccountSetter{CardLast4: &cardLast4}).UpdateMod(), um.Where(psql.And(
			models.Accounts.Columns.ID.EQ(psql.Arg(accountID)),
			models.Accounts.Columns.HouseholdID.EQ(psql.Arg(account.HouseholdID)),
		))).Exec(ctx, executor); err != nil {
			return CreatedAccount{}, err
		}
		if err := accounting.RecordEvent(ctx, executor, account.HouseholdID, accountID, "account.card_last4_updated", 1, []string{"cardLast4"}, actorID); err != nil {
			return CreatedAccount{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return CreatedAccount{}, err
	}
	var cardLast4 *string
	if command.CardLast4 != nil {
		value := *command.CardLast4
		cardLast4 = &value
	}
	var poolID *string
	if account.PoolID.Valid {
		value := account.PoolID.V.String()
		poolID = &value
	}

	return CreatedAccount{
		ID: account.ID.String(), Name: account.Name, Kind: account.Kind, CardLast4: cardLast4, PoolID: poolID,
	}, nil
}

func validateCardLast4(kind string, value *string) error {
	if value == nil {
		return nil
	}
	if kind != cardAccountKind || len(*value) != 4 {
		return apperror.Wrap(apperror.KindBadInput, errors.New("card last four digits require a card account and four digits"))
	}
	for _, digit := range *value {
		if digit < '0' || digit > '9' {
			return apperror.Wrap(apperror.KindBadInput, errors.New("card last four digits must contain only digits"))
		}
	}

	return nil
}

func sameLast4(existing sql.Null[string], next *string) bool {
	if !existing.Valid || next == nil {
		return !existing.Valid && next == nil
	}

	return existing.V == *next
}
