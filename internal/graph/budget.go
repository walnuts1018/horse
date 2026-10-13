package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/application/planning"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type budgetWriteInput struct {
	HouseholdID  string
	BudgetID     *string
	BaseRevision *int
	CategoryID   *string
	Name         string
	AmountYen    int64
	StartsOn     string
	EndsOn       *string
}

func (r *mutationResolver) setBudget(ctx context.Context, input budgetWriteInput) (*model.Budget, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	starts, err := parseDate(input.StartsOn)
	if err != nil {
		return nil, err
	}
	var ends *time.Time
	if input.EndsOn != nil {
		end, err := parseDate(*input.EndsOn)
		if err != nil {
			return nil, err
		}
		ends = &end
	}
	result, err := planning.NewService(r.DB).SetBudget(ctx, planning.BudgetCommand{
		ActorID: user.MemberID, HouseholdID: input.HouseholdID, BudgetID: input.BudgetID, Revision: input.BaseRevision,
		CategoryID: input.CategoryID, Name: input.Name, AmountYen: input.AmountYen, StartsOn: starts, EndsOn: ends,
	})
	if err != nil {
		return nil, err
	}
	return budgetResultModel(result), nil
}

func (r *mutationResolver) archiveBudget(ctx context.Context, budgetID string, baseRevision int, archived bool) (*model.Budget, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	id, err := gofrsuuid.FromString(budgetID)
	if err != nil {
		return nil, ErrNotFound
	}
	row, err := models.Budgets.Query(sm.Where(models.Budgets.Columns.ID.EQ(psql.Arg(id)))).One(ctx, queryExecutor(r.DB))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if _, err := requireMember(ctx, r.DB, row.HouseholdID.String()); err != nil {
		return nil, err
	}
	result, err := planning.NewService(r.DB).ArchiveBudget(ctx, user.MemberID, row.HouseholdID.String(), budgetID, baseRevision, archived)
	if err != nil {
		return nil, err
	}
	return budgetResultModel(result), nil
}

func budgetResultModel(result planning.BudgetResult) *model.Budget {
	budget := &model.Budget{ID: result.ID, HouseholdID: result.HouseholdID, Name: result.Name, AmountYen: graphLong(result.AmountYen), StartsOn: model.Date(result.StartsOn.Format(time.DateOnly)), Archived: result.Archived, Revision: int(result.Revision)}
	budget.CategoryID = result.CategoryID
	if result.EndsOn != nil {
		end := result.EndsOn.Format(time.DateOnly)
		budget.EndsOn = datePointer(&end)
	}
	return budget
}

func (r *queryResolver) budgets(ctx context.Context, householdID, period string, after, before *string, first, last *int) (*model.BudgetConnection, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	pageSize, backward, err := pageDirection(first, last)
	if err != nil {
		return nil, err
	}
	month, err := parseAccountingPeriod(period)
	if err != nil {
		return nil, fmt.Errorf("%w: period must use YYYY-MM", ErrBadInput)
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, ErrNotFound
	}
	baseFilter := budgetPeriodFilter(hhID, month)
	executor := queryExecutor(r.DB)
	count, err := models.Budgets.Query(sm.Where(baseFilter)).Count(ctx, executor)
	if err != nil {
		return nil, err
	}
	filters := []bob.Expression{baseFilter}
	for _, boundary := range []struct {
		cursor   *string
		isBefore bool
	}{{cursor: after}, {cursor: before, isBefore: true}} {
		id, err := cursorID(boundary.cursor, nodeTypeBudget)
		if err != nil {
			return nil, err
		}
		if id == nil {
			continue
		}
		parsedID, err := gofrsuuid.FromString(*id)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid budget cursor", ErrBadInput)
		}
		cursor, err := models.Budgets.Query(sm.Where(psql.And(
			models.Budgets.Columns.ID.EQ(psql.Arg(parsedID)),
			baseFilter,
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		filters = append(filters, budgetCursorFilter(cursor, boundary.isBefore))
	}
	var rows []*models.Budget
	if backward {
		rows, err = models.Budgets.Query(
			sm.Where(psql.And(filters...)),
			sm.OrderBy(models.Budgets.Columns.Name).Desc(),
			sm.OrderBy(models.Budgets.Columns.ID).Desc(),
			sm.Limit(pageSize+1),
		).All(ctx, executor)
	} else {
		rows, err = models.Budgets.Query(
			sm.Where(psql.And(filters...)),
			sm.OrderBy(models.Budgets.Columns.Name),
			sm.OrderBy(models.Budgets.Columns.ID),
			sm.Limit(pageSize+1),
		).All(ctx, executor)
	}
	if err != nil {
		return nil, err
	}
	hasMore := len(rows) > pageSize
	if hasMore {
		rows = rows[:pageSize]
	}
	if backward {
		slices.Reverse(rows)
	}
	result := &model.BudgetConnection{
		Edges: make([]*model.BudgetEdge, 0, len(rows)), TotalCount: int(count),
		PageInfo: &model.PageInfo{
			HasNextPage:     backward && before != nil && *before != "" || !backward && hasMore,
			HasPreviousPage: backward && hasMore || !backward && after != nil && *after != "",
		},
	}
	for _, row := range rows {
		budget, err := budgetModel(ctx, r.DB, row, &month)
		if err != nil {
			return nil, err
		}
		result.Edges = append(result.Edges, &model.BudgetEdge{Cursor: encodeCursor(nodeTypeBudget, row.ID.String()), Node: budget})
	}
	if len(rows) > 0 {
		start := encodeCursor(nodeTypeBudget, rows[0].ID.String())
		end := encodeCursor(nodeTypeBudget, rows[len(rows)-1].ID.String())
		result.PageInfo.StartCursor, result.PageInfo.EndCursor = &start, &end
	}
	return result, nil
}

func budgetPeriodFilter(householdID gofrsuuid.UUID, month time.Time) bob.Expression {
	return psql.And(
		models.Budgets.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Budgets.Columns.Archived.EQ(psql.Arg(false)),
		models.Budgets.Columns.StartsOn.LT(psql.Arg(month.AddDate(0, 1, 0))),
		psql.Or(models.Budgets.Columns.EndsOn.IsNull(), models.Budgets.Columns.EndsOn.GTE(psql.Arg(month))),
	)
}

func budgetCursorFilter(cursor *models.Budget, isBefore bool) bob.Expression {
	if isBefore {
		return psql.Or(
			models.Budgets.Columns.Name.LT(psql.Arg(cursor.Name)),
			psql.And(
				models.Budgets.Columns.Name.EQ(psql.Arg(cursor.Name)),
				models.Budgets.Columns.ID.LT(psql.Arg(cursor.ID)),
			),
		)
	}
	return psql.Or(
		models.Budgets.Columns.Name.GT(psql.Arg(cursor.Name)),
		psql.And(
			models.Budgets.Columns.Name.EQ(psql.Arg(cursor.Name)),
			models.Budgets.Columns.ID.GT(psql.Arg(cursor.ID)),
		),
	)
}

func budgetModel(ctx context.Context, db *pgxpool.Pool, row *models.Budget, month *time.Time) (*model.Budget, error) {
	result := &model.Budget{ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), Name: row.Name, AmountYen: graphLong(row.AmountYen), StartsOn: model.Date(row.StartsOn.Format(time.DateOnly)), Archived: row.Archived, Revision: int(row.Revision)}
	if row.CategoryID.Valid {
		id := row.CategoryID.V.String()
		result.CategoryID = &id
	}
	if row.EndsOn.Valid {
		end := row.EndsOn.V.Format(time.DateOnly)
		result.EndsOn = datePointer(&end)
	}
	if month == nil {
		return result, nil
	}
	start, end := month.UTC(), month.AddDate(0, 1, 0).UTC()
	purchases, err := models.Purchases.Query(sm.Where(psql.And(models.Purchases.Columns.HouseholdID.EQ(psql.Arg(row.HouseholdID)), models.Purchases.Columns.OccurredAt.GTE(psql.Arg(start)), models.Purchases.Columns.OccurredAt.LT(psql.Arg(end)), psql.Or(models.Purchases.Columns.Status.EQ(psql.Arg(statusPosted)), models.Purchases.Columns.Status.EQ(psql.Arg(statusClosed)))))).All(ctx, queryExecutor(db))
	if err != nil {
		return nil, err
	}
	var actual int64
	for _, purchase := range purchases {
		items, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchase.ID)))).All(ctx, queryExecutor(db))
		if err != nil {
			return nil, err
		}
		itemsByID := make(map[gofrsuuid.UUID]*models.PurchaseItem, len(items))
		for _, item := range items {
			itemsByID[item.ID] = item
			if row.CategoryID.Valid && (!item.CategoryID.Valid || row.CategoryID.V != item.CategoryID.V) {
				continue
			}
			var ok bool
			actual, ok = checkedAdd(actual, item.AmountYen)
			if !ok {
				return nil, fmt.Errorf("%w: budget actual exceeds int64", ErrBadInput)
			}
		}
		corrections, err := models.Corrections.Query(sm.Where(models.Corrections.Columns.PurchaseID.EQ(psql.Arg(purchase.ID)))).All(ctx, queryExecutor(db))
		if err != nil {
			return nil, err
		}
		for _, correction := range corrections {
			var categoryID sql.Null[gofrsuuid.UUID]
			if correction.ItemID.Valid {
				if item, ok := itemsByID[correction.ItemID.V]; ok {
					categoryID = item.CategoryID
				}
			}
			if row.CategoryID.Valid && (!categoryID.Valid || row.CategoryID.V != categoryID.V) {
				continue
			}
			var ok bool
			actual, ok = checkedAdd(actual, correction.AmountDeltaYen)
			if !ok {
				return nil, fmt.Errorf("%w: budget actual exceeds int64", ErrBadInput)
			}
		}
	}
	result.ActualYen = graphLong(actual)
	result.RemainingYen = graphLong(row.AmountYen - actual)
	return result, nil
}
