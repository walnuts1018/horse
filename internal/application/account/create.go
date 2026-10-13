package account

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const maxOpeningBalanceYen int64 = 1_000_000_000_000

var accountingLocation = time.FixedZone("JST", 9*60*60)

type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

type OpeningBalance struct {
	AmountYen int64
	At        time.Time
}

type CreateCommand struct {
	ActorID     string
	HouseholdID string
	Name        string
	Kind        string
	PoolID      *string
	Opening     *OpeningBalance
}

type CreatedAccount struct {
	ID     string
	Name   string
	Kind   string
	PoolID *string
}

func (s *Service) Create(ctx context.Context, command CreateCommand) (CreatedAccount, error) {
	name := strings.TrimSpace(command.Name)
	if name == "" || len(name) > 120 || !validKind(command.Kind) {
		return CreatedAccount{}, apperror.Wrap(apperror.KindBadInput, errors.New("invalid account"))
	}
	if command.Opening != nil && (command.Opening.AmountYen < 1 || command.Opening.AmountYen > maxOpeningBalanceYen) {
		return CreatedAccount{}, apperror.Wrap(apperror.KindBadInput, errors.New("invalid opening balance"))
	}

	householdID, err := parseID(command.HouseholdID, "household")
	if err != nil {
		return CreatedAccount{}, err
	}
	actorID, err := parseID(command.ActorID, "actor")
	if err != nil {
		return CreatedAccount{}, err
	}
	var poolID sql.Null[gofrsuuid.UUID]
	if command.PoolID != nil {
		poolUUID, err := parseID(*command.PoolID, "pool")
		if err != nil {
			return CreatedAccount{}, err
		}
		poolID = sql.Null[gofrsuuid.UUID]{V: poolUUID, Valid: true}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CreatedAccount{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return CreatedAccount{}, apperror.ErrNotFound
	} else if err != nil {
		return CreatedAccount{}, err
	}
	if poolID.Valid {
		if _, err := models.Pools.Query(sm.Where(psql.And(
			models.Pools.Columns.ID.EQ(psql.Arg(poolID.V)),
			models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		))).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
			return CreatedAccount{}, apperror.ErrNotFound
		} else if err != nil {
			return CreatedAccount{}, err
		}
	}

	accountID, err := gofrsuuid.FromString(uuid.NewV7().String())
	if err != nil {
		return CreatedAccount{}, fmt.Errorf("parse generated account id: %w", err)
	}
	if command.Opening != nil {
		if err := lockAccountingPeriod(ctx, executor, householdID, command.Opening.At); err != nil {
			return CreatedAccount{}, err
		}
	}
	kind := command.Kind
	if _, err := models.Accounts.Insert(&models.AccountSetter{
		ID: &accountID, HouseholdID: &householdID, PoolID: &poolID, Name: &name, Kind: &kind,
	}).Exec(ctx, executor); err != nil {
		return CreatedAccount{}, err
	}
	if command.Opening != nil {
		if err := createOpeningBalance(ctx, executor, householdID, actorID, accountID, command.Kind, *command.Opening); err != nil {
			return CreatedAccount{}, err
		}
	}
	if err := accounting.RecordEvent(ctx, executor, householdID, accountID, "account.created", 1, []string{"name", "kind", "poolId"}, actorID); err != nil {
		return CreatedAccount{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CreatedAccount{}, err
	}
	return CreatedAccount{ID: accountID.String(), Name: name, Kind: kind, PoolID: command.PoolID}, nil
}

func createOpeningBalance(ctx context.Context, executor bob.Executor, householdID, actorID, accountID gofrsuuid.UUID, kind string, opening OpeningBalance) error {
	journalID, err := gofrsuuid.FromString(uuid.NewV7().String())
	if err != nil {
		return fmt.Errorf("parse generated journal id: %w", err)
	}
	sourceKind := "ACCOUNT_OPENING_BALANCE"
	idempotencyKey := "account-opening:" + accountID.String()
	if _, err := models.Journals.Insert(&models.JournalSetter{
		ID: &journalID, HouseholdID: &householdID, SourceKind: &sourceKind, SourceID: &accountID,
		IdempotencyKey: &idempotencyKey, OccurredAt: &opening.At, CreatedBy: &actorID,
	}).Exec(ctx, executor); err != nil {
		return err
	}
	accountSide, offsetSide := accounting.Debit, accounting.Credit
	if kind == "CARD" {
		accountSide, offsetSide = accounting.Credit, accounting.Debit
	}
	entries, err := accounting.BuildEntries([]accounting.Posting{
		{Side: accountSide, AccountID: &accountID, AmountYen: opening.AmountYen, Memo: "Opening balance"},
		{Side: offsetSide, AmountYen: opening.AmountYen, Memo: "Opening balance offset"},
	})
	if err != nil {
		return err
	}
	return accounting.InsertEntries(ctx, executor, journalID, entries)
}

func lockAccountingPeriod(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, at time.Time) error {
	period := at.In(accountingLocation).Format("2006-01")
	status := "OPEN"
	revision := int64(0)
	if _, err := models.AccountingPeriods.Insert(&models.AccountingPeriodSetter{
		HouseholdID: &householdID, Period: &period, Status: &status, Revision: &revision,
	}, im.OnConflict("household_id", "period").DoNothing()).Exec(ctx, executor); err != nil {
		return err
	}
	accountingPeriod, err := models.AccountingPeriods.Query(sm.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(period)),
	)), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return err
	}
	if accountingPeriod.Status == "CLOSED" {
		return apperror.Wrap(apperror.KindConflict, errors.New("accounting period is closed"))
	}
	return nil
}

func validKind(kind string) bool {
	switch kind {
	case "BANK", "CASH", "CARD", "OTHER":
		return true
	default:
		return false
	}
}

func parseID(value, resource string) (gofrsuuid.UUID, error) {
	id, err := gofrsuuid.FromString(value)
	if err != nil {
		return gofrsuuid.UUID{}, apperror.Wrap(apperror.KindBadInput, fmt.Errorf("invalid %s id", resource))
	}
	return id, nil
}
