package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type recurringExpenseWriteInput struct {
	HouseholdID      string
	ExpenseID        *string
	BaseRevision     *int
	Name             string
	Merchant         string
	AmountYen        int64
	CategoryID       *string
	PaymentAccountID *string
	Cadence          string
	StartsOn         string
	NextDueOn        string
	EndsOn           *string
}

func (r *mutationResolver) setRecurringExpense(ctx context.Context, input recurringExpenseWriteInput) (*model.RecurringExpense, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	name, merchant := strings.TrimSpace(input.Name), strings.TrimSpace(input.Merchant)
	if name == "" || len(name) > 160 || len(merchant) > 240 || input.AmountYen <= 0 || input.AmountYen > maxMoney || !slices.Contains([]string{"WEEKLY", "MONTHLY", "YEARLY"}, input.Cadence) {
		return nil, fmt.Errorf("%w: invalid recurring expense", ErrBadInput)
	}
	starts, err := parseDate(input.StartsOn)
	if err != nil {
		return nil, err
	}
	due, err := parseDate(input.NextDueOn)
	if err != nil || due.Before(starts) {
		return nil, fmt.Errorf("%w: next due date precedes start date", ErrBadInput)
	}
	ends := sql.Null[time.Time]{}
	if input.EndsOn != nil {
		end, err := parseDate(*input.EndsOn)
		if err != nil || end.Before(starts) {
			return nil, fmt.Errorf("%w: invalid recurring expense end date", ErrBadInput)
		}
		ends = sql.Null[time.Time]{V: end, Valid: true}
	}
	hhID, err := gofrsuuid.FromString(input.HouseholdID)
	if err != nil {
		return nil, ErrNotFound
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(hhID))), sm.ForUpdate()).One(ctx, executor); err != nil {
		return nil, err
	}
	var row *models.RecurringExpense
	var category, account sql.Null[gofrsuuid.UUID]
	if input.CategoryID != nil {
		category = sql.Null[gofrsuuid.UUID]{V: generatedUUID(*input.CategoryID), Valid: true}
	}
	if input.PaymentAccountID != nil {
		account = sql.Null[gofrsuuid.UUID]{V: generatedUUID(*input.PaymentAccountID), Valid: true}
	}
	amount := input.AmountYen
	if input.ExpenseID == nil {
		id := generatedUUID(newID())
		row, err = models.RecurringExpenses.Insert(&models.RecurringExpenseSetter{ID: &id, HouseholdID: &hhID, Name: &name, Merchant: &merchant, AmountYen: &amount, CategoryID: &category, PaymentAccountID: &account, Cadence: &input.Cadence, StartsOn: &starts, NextDueOn: &due, EndsOn: &ends}).One(ctx, executor)
	} else {
		id, parseErr := gofrsuuid.FromString(*input.ExpenseID)
		if parseErr != nil || input.BaseRevision == nil {
			return nil, fmt.Errorf("%w: recurring expense update requires id and revision", ErrBadInput)
		}
		row, err = models.RecurringExpenses.Query(sm.Where(psql.And(models.RecurringExpenses.Columns.ID.EQ(psql.Arg(id)), models.RecurringExpenses.Columns.HouseholdID.EQ(psql.Arg(hhID)))), sm.ForUpdate()).One(ctx, executor)
		if err == nil && int(row.Revision) != *input.BaseRevision {
			return nil, ErrConflict
		}
		if err == nil {
			revision := row.Revision + 1
			_, err = models.RecurringExpenses.Update((&models.RecurringExpenseSetter{Name: &name, Merchant: &merchant, AmountYen: &amount, CategoryID: &category, PaymentAccountID: &account, Cadence: &input.Cadence, StartsOn: &starts, NextDueOn: &due, EndsOn: &ends, Revision: &revision}).UpdateMod(), um.Where(models.RecurringExpenses.Columns.ID.EQ(psql.Arg(id))), um.SetCol("updated_at").To(psql.F("now"))).Exec(ctx, executor)
			if err == nil {
				row, err = models.RecurringExpenses.Query(sm.Where(models.RecurringExpenses.Columns.ID.EQ(psql.Arg(id)))).One(ctx, executor)
			}
		}
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "recurring_expense.updated", row.ID.String(), row.Revision, []string{fieldName, fieldMerchant, fieldAmountYen, fieldCategoryID, fieldPaymentAccountID, fieldCadence, fieldStartsOn, fieldNextDueOn, fieldEndsOn}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return recurringExpenseModel(row), nil
}

func (r *mutationResolver) archiveRecurringExpense(ctx context.Context, expenseID string, baseRevision int, archived bool) (*model.RecurringExpense, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	id, err := gofrsuuid.FromString(expenseID)
	if err != nil {
		return nil, ErrNotFound
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	row, err := models.RecurringExpenses.Query(sm.Where(models.RecurringExpenses.Columns.ID.EQ(psql.Arg(id))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if _, err := requireMember(ctx, r.DB, row.HouseholdID.String()); err != nil {
		return nil, err
	}
	if int(row.Revision) != baseRevision {
		return nil, ErrConflict
	}
	revision := row.Revision + 1
	if _, err := models.RecurringExpenses.Update((&models.RecurringExpenseSetter{Archived: &archived, Revision: &revision}).UpdateMod(), um.Where(models.RecurringExpenses.Columns.ID.EQ(psql.Arg(id)))).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, row.HouseholdID.String(), "recurring_expense.archived", expenseID, revision, []string{fieldArchived}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	row.Archived, row.Revision = archived, revision
	return recurringExpenseModel(row), nil
}

func recurringExpenseModel(row *models.RecurringExpense) *model.RecurringExpense {
	result := &model.RecurringExpense{ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), Name: row.Name, Merchant: row.Merchant, AmountYen: graphLong(row.AmountYen), Currency: row.Currency, Cadence: model.RecurringExpenseCadence(row.Cadence), StartsOn: model.Date(row.StartsOn.Format(time.DateOnly)), NextDueOn: model.Date(row.NextDueOn.Format(time.DateOnly)), Archived: row.Archived, Revision: int(row.Revision)}
	if row.CategoryID.Valid {
		id := row.CategoryID.V.String()
		result.CategoryID = &id
	}
	if row.PaymentAccountID.Valid {
		id := row.PaymentAccountID.V.String()
		result.PaymentAccountID = &id
	}
	if row.EndsOn.Valid {
		date := row.EndsOn.V.Format(time.DateOnly)
		result.EndsOn = datePointer(&date)
	}
	return result
}

func (r *queryResolver) recurringExpenses(ctx context.Context, householdID string, includeArchived bool) ([]*model.RecurringExpense, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, ErrNotFound
	}
	var rows models.RecurringExpenseSlice
	if includeArchived {
		rows, err = models.RecurringExpenses.Query(sm.Where(models.RecurringExpenses.Columns.HouseholdID.EQ(psql.Arg(hhID))), sm.OrderBy(models.RecurringExpenses.Columns.NextDueOn)).All(ctx, queryExecutor(r.DB))
	} else {
		rows, err = models.RecurringExpenses.Query(sm.Where(psql.And(models.RecurringExpenses.Columns.HouseholdID.EQ(psql.Arg(hhID)), models.RecurringExpenses.Columns.Archived.EQ(psql.Arg(false)))), sm.OrderBy(models.RecurringExpenses.Columns.NextDueOn)).All(ctx, queryExecutor(r.DB))
	}
	if err != nil {
		return nil, err
	}
	result := make([]*model.RecurringExpense, 0, len(rows))
	for _, row := range rows {
		result = append(result, recurringExpenseModel(row))
	}
	return result, nil
}

func (r *queryResolver) recurringExpenseConnection(ctx context.Context, householdID string, includeArchived bool, after, before *string, first, last *int) (*model.RecurringExpenseConnection, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	pageSize, backward, err := pageDirection(first, last)
	if err != nil {
		return nil, err
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, ErrNotFound
	}
	executor := queryExecutor(r.DB)
	filters := []bob.Expression{models.RecurringExpenses.Columns.HouseholdID.EQ(psql.Arg(hhID))}
	if !includeArchived {
		filters = append(filters, models.RecurringExpenses.Columns.Archived.EQ(psql.Arg(false)))
	}
	baseFilter := psql.And(filters...)
	count, err := models.RecurringExpenses.Query(sm.Where(baseFilter)).Count(ctx, executor)
	if err != nil {
		return nil, err
	}
	for _, boundary := range []struct {
		cursor   *string
		isBefore bool
	}{{cursor: after}, {cursor: before, isBefore: true}} {
		afterID, err := cursorID(boundary.cursor, nodeTypeRecurringExpense)
		if err != nil {
			return nil, err
		}
		if afterID == nil {
			continue
		}
		cursorID, err := gofrsuuid.FromString(*afterID)
		if err != nil {
			return nil, fmt.Errorf("parse recurring expense cursor: %w", err)
		}
		cursor, err := models.RecurringExpenses.Query(sm.Where(psql.And(
			models.RecurringExpenses.Columns.ID.EQ(psql.Arg(cursorID)),
			baseFilter,
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		filters = append(filters, recurringExpenseCursorFilter(cursor, boundary.isBefore))
	}
	var rows []*models.RecurringExpense
	if backward {
		rows, err = models.RecurringExpenses.Query(
			sm.Where(psql.And(filters...)),
			sm.OrderBy(models.RecurringExpenses.Columns.NextDueOn).Desc(),
			sm.OrderBy(models.RecurringExpenses.Columns.ID).Desc(),
			sm.Limit(pageSize+1),
		).All(ctx, executor)
	} else {
		rows, err = models.RecurringExpenses.Query(
			sm.Where(psql.And(filters...)),
			sm.OrderBy(models.RecurringExpenses.Columns.NextDueOn),
			sm.OrderBy(models.RecurringExpenses.Columns.ID),
			sm.Limit(pageSize+1),
		).All(ctx, executor)
	}
	if err != nil {
		return nil, err
	}
	hasNextPage := len(rows) > pageSize
	if hasNextPage {
		rows = rows[:pageSize]
	}
	if backward {
		slices.Reverse(rows)
	}
	connection := &model.RecurringExpenseConnection{
		Edges:      make([]*model.RecurringExpenseEdge, 0, len(rows)),
		TotalCount: int(count),
		PageInfo:   &model.PageInfo{HasNextPage: backward && before != nil && *before != "" || !backward && hasNextPage, HasPreviousPage: backward && hasNextPage || !backward && after != nil && *after != ""},
	}
	for _, row := range rows {
		connection.Edges = append(connection.Edges, &model.RecurringExpenseEdge{
			Cursor: encodeCursor(nodeTypeRecurringExpense, row.ID.String()),
			Node:   recurringExpenseModel(row),
		})
	}
	if len(rows) > 0 {
		start, end := encodeCursor(nodeTypeRecurringExpense, rows[0].ID.String()), encodeCursor(nodeTypeRecurringExpense, rows[len(rows)-1].ID.String())
		connection.PageInfo.StartCursor, connection.PageInfo.EndCursor = &start, &end
	}
	return connection, nil
}

func recurringExpenseCursorFilter(cursor *models.RecurringExpense, isBefore bool) bob.Expression {
	if isBefore {
		return psql.Or(
			models.RecurringExpenses.Columns.NextDueOn.LT(psql.Arg(cursor.NextDueOn)),
			psql.And(
				models.RecurringExpenses.Columns.NextDueOn.EQ(psql.Arg(cursor.NextDueOn)),
				models.RecurringExpenses.Columns.ID.LT(psql.Arg(cursor.ID)),
			),
		)
	}
	return psql.Or(
		models.RecurringExpenses.Columns.NextDueOn.GT(psql.Arg(cursor.NextDueOn)),
		psql.And(
			models.RecurringExpenses.Columns.NextDueOn.EQ(psql.Arg(cursor.NextDueOn)),
			models.RecurringExpenses.Columns.ID.GT(psql.Arg(cursor.ID)),
		),
	)
}
