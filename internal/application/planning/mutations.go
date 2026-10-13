package planning

import (
	"context"
	"database/sql"
	jsonv1 "encoding/json"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

var (
	ErrBadInput = apperror.ErrBadInput
	ErrConflict = apperror.ErrConflict
	ErrNotFound = apperror.ErrNotFound
)

const maxMoney int64 = 1_000_000_000_000

type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

type BudgetCommand struct {
	ActorID     string
	HouseholdID string
	BudgetID    *string
	Revision    *int
	CategoryID  *string
	Name        string
	AmountYen   int64
	StartsOn    time.Time
	EndsOn      *time.Time
}

type BudgetResult struct {
	ID          string
	HouseholdID string
	CategoryID  *string
	Name        string
	AmountYen   int64
	StartsOn    time.Time
	EndsOn      *time.Time
	Archived    bool
	Revision    int64
}

type RecurringExpenseCommand struct {
	ActorID          string
	HouseholdID      string
	ExpenseID        *string
	Revision         *int
	Name             string
	Merchant         string
	AmountYen        int64
	CategoryID       *string
	PaymentAccountID *string
	Cadence          string
	StartsOn         time.Time
	NextDueOn        time.Time
	EndsOn           *time.Time
}

type RecurringExpenseResult struct {
	ID               string
	HouseholdID      string
	Name             string
	Merchant         string
	AmountYen        int64
	CategoryID       *string
	PaymentAccountID *string
	Cadence          string
	StartsOn         time.Time
	NextDueOn        time.Time
	EndsOn           *time.Time
	Currency         string
	Archived         bool
	Revision         int64
}

func (s *Service) SetBudget(ctx context.Context, command BudgetCommand) (BudgetResult, error) {
	name := strings.TrimSpace(command.Name)
	if name == "" || len(name) > 120 || command.AmountYen <= 0 || command.AmountYen > maxMoney || command.StartsOn.IsZero() {
		return BudgetResult{}, fmt.Errorf("%w: invalid budget", ErrBadInput)
	}
	if command.EndsOn != nil && command.EndsOn.Before(command.StartsOn) {
		return BudgetResult{}, fmt.Errorf("%w: invalid budget end date", ErrBadInput)
	}
	householdID, err := parseUUID(command.HouseholdID)
	if err != nil {
		return BudgetResult{}, err
	}
	actorID, err := parseUUID(command.ActorID)
	if err != nil {
		return BudgetResult{}, err
	}
	var category sql.Null[gofrsuuid.UUID]
	if command.CategoryID != nil {
		id, err := parseUUID(*command.CategoryID)
		if err != nil {
			return BudgetResult{}, err
		}
		category = sql.Null[gofrsuuid.UUID]{V: id, Valid: true}
	}
	starts, ends := command.StartsOn, nullableTime(command.EndsOn)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return BudgetResult{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor); err != nil {
		return BudgetResult{}, err
	}
	var row *models.Budget
	if command.BudgetID == nil {
		id := newUUID()
		amount := command.AmountYen
		row, err = models.Budgets.Insert(&models.BudgetSetter{ID: &id, HouseholdID: &householdID, CategoryID: &category, Name: &name, AmountYen: &amount, StartsOn: &starts, EndsOn: &ends}).One(ctx, executor)
	} else {
		id, parseErr := parseUUID(*command.BudgetID)
		if parseErr != nil || command.Revision == nil {
			return BudgetResult{}, fmt.Errorf("%w: budget update requires id and revision", ErrBadInput)
		}
		row, err = models.Budgets.Query(sm.Where(psql.And(models.Budgets.Columns.ID.EQ(psql.Arg(id)), models.Budgets.Columns.HouseholdID.EQ(psql.Arg(householdID)))), sm.ForUpdate()).One(ctx, executor)
		if err == nil && int(row.Revision) != *command.Revision {
			return BudgetResult{}, ErrConflict
		}
		if err == nil {
			amount, revision := command.AmountYen, row.Revision+1
			_, err = models.Budgets.Update((&models.BudgetSetter{CategoryID: &category, Name: &name, AmountYen: &amount, StartsOn: &starts, EndsOn: &ends, Revision: &revision}).UpdateMod(), um.Where(models.Budgets.Columns.ID.EQ(psql.Arg(id))), um.SetCol("updated_at").To(psql.F("now"))).Exec(ctx, executor)
			if err == nil {
				row, err = models.Budgets.Query(sm.Where(models.Budgets.Columns.ID.EQ(psql.Arg(id)))).One(ctx, executor)
			}
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		return BudgetResult{}, ErrNotFound
	}
	if err != nil {
		return BudgetResult{}, err
	}
	if err := writeEvent(ctx, tx, householdID, row.ID, "budget.updated", row.Revision, []string{"name", "amountYen", "categoryId", "startsOn", "endsOn"}, actorID); err != nil {
		return BudgetResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return BudgetResult{}, err
	}
	return budgetResult(row), nil
}

func (s *Service) ArchiveBudget(ctx context.Context, actorID, householdID, budgetID string, revision int, archived bool) (BudgetResult, error) {
	hhID, err := parseUUID(householdID)
	if err != nil {
		return BudgetResult{}, err
	}
	id, err := parseUUID(budgetID)
	if err != nil {
		return BudgetResult{}, err
	}
	actor, err := parseUUID(actorID)
	if err != nil {
		return BudgetResult{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return BudgetResult{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	row, err := models.Budgets.Query(sm.Where(psql.And(models.Budgets.Columns.ID.EQ(psql.Arg(id)), models.Budgets.Columns.HouseholdID.EQ(psql.Arg(hhID)))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return BudgetResult{}, ErrNotFound
	}
	if err != nil {
		return BudgetResult{}, err
	}
	if int(row.Revision) != revision {
		return BudgetResult{}, ErrConflict
	}
	revisionValue := row.Revision + 1
	if _, err := models.Budgets.Update((&models.BudgetSetter{Archived: &archived, Revision: &revisionValue}).UpdateMod(), um.Where(models.Budgets.Columns.ID.EQ(psql.Arg(id)))).Exec(ctx, executor); err != nil {
		return BudgetResult{}, err
	}
	if err := writeEvent(ctx, tx, hhID, id, "budget.archived", revisionValue, []string{"archived"}, actor); err != nil {
		return BudgetResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return BudgetResult{}, err
	}
	row.Archived, row.Revision = archived, revisionValue
	return budgetResult(row), nil
}

func (s *Service) SetRecurringExpense(ctx context.Context, command RecurringExpenseCommand) (RecurringExpenseResult, error) {
	name, merchant := strings.TrimSpace(command.Name), strings.TrimSpace(command.Merchant)
	if name == "" || len(name) > 160 || len(merchant) > 240 || command.AmountYen <= 0 || command.AmountYen > maxMoney || command.StartsOn.IsZero() || command.NextDueOn.Before(command.StartsOn) {
		return RecurringExpenseResult{}, fmt.Errorf("%w: invalid recurring expense", ErrBadInput)
	}
	if command.Cadence != "WEEKLY" && command.Cadence != "MONTHLY" && command.Cadence != "YEARLY" {
		return RecurringExpenseResult{}, fmt.Errorf("%w: invalid recurring expense cadence", ErrBadInput)
	}
	if command.EndsOn != nil && command.EndsOn.Before(command.StartsOn) {
		return RecurringExpenseResult{}, fmt.Errorf("%w: invalid recurring expense end date", ErrBadInput)
	}
	householdID, err := parseUUID(command.HouseholdID)
	if err != nil {
		return RecurringExpenseResult{}, err
	}
	actorID, err := parseUUID(command.ActorID)
	if err != nil {
		return RecurringExpenseResult{}, err
	}
	category, account := nullableUUID(command.CategoryID), nullableUUID(command.PaymentAccountID)
	if command.CategoryID != nil && !category.Valid || command.PaymentAccountID != nil && !account.Valid {
		return RecurringExpenseResult{}, fmt.Errorf("%w: invalid recurring expense reference", ErrBadInput)
	}
	starts, due, ends := command.StartsOn, command.NextDueOn, nullableTime(command.EndsOn)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return RecurringExpenseResult{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor); err != nil {
		return RecurringExpenseResult{}, err
	}
	var row *models.RecurringExpense
	amount := command.AmountYen
	if command.ExpenseID == nil {
		id := newUUID()
		row, err = models.RecurringExpenses.Insert(&models.RecurringExpenseSetter{ID: &id, HouseholdID: &householdID, Name: &name, Merchant: &merchant, AmountYen: &amount, CategoryID: &category, PaymentAccountID: &account, Cadence: &command.Cadence, StartsOn: &starts, NextDueOn: &due, EndsOn: &ends}).One(ctx, executor)
	} else {
		id, parseErr := parseUUID(*command.ExpenseID)
		if parseErr != nil || command.Revision == nil {
			return RecurringExpenseResult{}, fmt.Errorf("%w: recurring expense update requires id and revision", ErrBadInput)
		}
		row, err = models.RecurringExpenses.Query(sm.Where(psql.And(models.RecurringExpenses.Columns.ID.EQ(psql.Arg(id)), models.RecurringExpenses.Columns.HouseholdID.EQ(psql.Arg(householdID)))), sm.ForUpdate()).One(ctx, executor)
		if err == nil && int(row.Revision) != *command.Revision {
			return RecurringExpenseResult{}, ErrConflict
		}
		if err == nil {
			revision := row.Revision + 1
			_, err = models.RecurringExpenses.Update((&models.RecurringExpenseSetter{Name: &name, Merchant: &merchant, AmountYen: &amount, CategoryID: &category, PaymentAccountID: &account, Cadence: &command.Cadence, StartsOn: &starts, NextDueOn: &due, EndsOn: &ends, Revision: &revision}).UpdateMod(), um.Where(models.RecurringExpenses.Columns.ID.EQ(psql.Arg(id))), um.SetCol("updated_at").To(psql.F("now"))).Exec(ctx, executor)
			if err == nil {
				row, err = models.RecurringExpenses.Query(sm.Where(models.RecurringExpenses.Columns.ID.EQ(psql.Arg(id)))).One(ctx, executor)
			}
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		return RecurringExpenseResult{}, ErrNotFound
	}
	if err != nil {
		return RecurringExpenseResult{}, err
	}
	if err := writeEvent(ctx, tx, householdID, row.ID, "recurring_expense.updated", row.Revision, []string{"name", "merchant", "amountYen", "categoryId", "paymentAccountId", "cadence", "startsOn", "nextDueOn", "endsOn"}, actorID); err != nil {
		return RecurringExpenseResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RecurringExpenseResult{}, err
	}
	return recurringExpenseResult(row), nil
}

func (s *Service) ArchiveRecurringExpense(ctx context.Context, actorID, householdID, expenseID string, revision int, archived bool) (RecurringExpenseResult, error) {
	hhID, err := parseUUID(householdID)
	if err != nil {
		return RecurringExpenseResult{}, err
	}
	id, err := parseUUID(expenseID)
	if err != nil {
		return RecurringExpenseResult{}, err
	}
	actor, err := parseUUID(actorID)
	if err != nil {
		return RecurringExpenseResult{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return RecurringExpenseResult{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	row, err := models.RecurringExpenses.Query(sm.Where(psql.And(models.RecurringExpenses.Columns.ID.EQ(psql.Arg(id)), models.RecurringExpenses.Columns.HouseholdID.EQ(psql.Arg(hhID)))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return RecurringExpenseResult{}, ErrNotFound
	}
	if err != nil {
		return RecurringExpenseResult{}, err
	}
	if int(row.Revision) != revision {
		return RecurringExpenseResult{}, ErrConflict
	}
	revisionValue := row.Revision + 1
	if _, err := models.RecurringExpenses.Update((&models.RecurringExpenseSetter{Archived: &archived, Revision: &revisionValue}).UpdateMod(), um.Where(models.RecurringExpenses.Columns.ID.EQ(psql.Arg(id)))).Exec(ctx, executor); err != nil {
		return RecurringExpenseResult{}, err
	}
	if err := writeEvent(ctx, tx, hhID, id, "recurring_expense.archived", revisionValue, []string{"archived"}, actor); err != nil {
		return RecurringExpenseResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RecurringExpenseResult{}, err
	}
	row.Archived, row.Revision = archived, revisionValue
	return recurringExpenseResult(row), nil
}

func budgetResult(row *models.Budget) BudgetResult {
	result := BudgetResult{ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), Name: row.Name, AmountYen: row.AmountYen, StartsOn: row.StartsOn, Archived: row.Archived, Revision: row.Revision}
	if row.CategoryID.Valid {
		id := row.CategoryID.V.String()
		result.CategoryID = &id
	}
	if row.EndsOn.Valid {
		end := row.EndsOn.V
		result.EndsOn = &end
	}
	return result
}

func recurringExpenseResult(row *models.RecurringExpense) RecurringExpenseResult {
	result := RecurringExpenseResult{ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), Name: row.Name, Merchant: row.Merchant, AmountYen: row.AmountYen, Cadence: row.Cadence, StartsOn: row.StartsOn, NextDueOn: row.NextDueOn, Currency: row.Currency, Archived: row.Archived, Revision: row.Revision}
	if row.CategoryID.Valid {
		id := row.CategoryID.V.String()
		result.CategoryID = &id
	}
	if row.PaymentAccountID.Valid {
		id := row.PaymentAccountID.V.String()
		result.PaymentAccountID = &id
	}
	if row.EndsOn.Valid {
		end := row.EndsOn.V
		result.EndsOn = &end
	}
	return result
}

func nullableUUID(value *string) sql.Null[gofrsuuid.UUID] {
	if value == nil {
		return sql.Null[gofrsuuid.UUID]{}
	}
	id, err := parseUUID(*value)
	if err != nil {
		return sql.Null[gofrsuuid.UUID]{}
	}
	return sql.Null[gofrsuuid.UUID]{V: id, Valid: true}
}

func nullableTime(value *time.Time) sql.Null[time.Time] {
	if value == nil {
		return sql.Null[time.Time]{}
	}
	return sql.Null[time.Time]{V: *value, Valid: true}
}

func parseUUID(value string) (gofrsuuid.UUID, error) {
	id, err := gofrsuuid.FromString(value)
	if err != nil {
		return gofrsuuid.Nil, fmt.Errorf("%w: invalid identifier", ErrBadInput)
	}
	return id, nil
}

func newUUID() gofrsuuid.UUID {
	return gofrsuuid.Must(gofrsuuid.NewV7())
}

func writeEvent(ctx context.Context, tx pgx.Tx, householdID, entityID gofrsuuid.UUID, eventType string, revision int64, fields []string, actorID gofrsuuid.UUID) error {
	encoded, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	changedFields := types.JSON[jsonv1.RawMessage]{Val: encoded}
	actor := sql.Null[gofrsuuid.UUID]{V: actorID, Valid: true}
	_, err = models.HouseholdEvents.Insert(&models.HouseholdEventSetter{HouseholdID: &householdID, Type: &eventType, EntityID: &entityID, Revision: &revision, ChangedFields: &changedFields, ActorID: &actor}).Exec(ctx, bobpgx.NewTx(tx, nil))
	return err
}
