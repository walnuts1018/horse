package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func fundingScheduleModel(row *models.FundingSchedule) *model.FundingSchedule {
	var endsOn *string
	if row.EndsOn.Valid {
		value := row.EndsOn.V.Format("2006-01-02")
		endsOn = &value
	}
	return &model.FundingSchedule{
		ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), MemberID: row.MemberID.String(), PoolID: row.PoolID.String(),
		AmountYen: graphLong(row.AmountYen), Cadence: model.FundingScheduleCadence(row.Cadence),
		StartsOn: row.StartsOn.Format("2006-01-02"), EndsOn: endsOn, Active: row.Active, CreatedAt: timeString(row.CreatedAt),
	}
}

func (r *queryResolver) fundingSchedules(ctx context.Context, householdID string, active *bool, after, before *string, first, last *int) (*model.FundingScheduleConnection, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	pageSize, backward, err := pageDirection(after, before, first, last)
	if err != nil {
		return nil, err
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, ErrNotFound
	}
	executor := queryExecutor(r.DB)
	filters := []bob.Expression{models.FundingSchedules.Columns.HouseholdID.EQ(psql.Arg(hhID))}
	if active != nil {
		filters = append(filters, models.FundingSchedules.Columns.Active.EQ(psql.Arg(*active)))
	}
	baseFilter := psql.And(filters...)
	count, err := models.FundingSchedules.Query(sm.Where(baseFilter)).Count(ctx, executor)
	if err != nil {
		return nil, err
	}
	for _, boundary := range []struct {
		cursor   *string
		isBefore bool
	}{{cursor: after}, {cursor: before, isBefore: true}} {
		id, err := cursorID(boundary.cursor, "FundingSchedule")
		if err != nil {
			return nil, err
		}
		if id == nil {
			continue
		}
		parsedID, err := gofrsuuid.FromString(*id)
		if err != nil {
			return nil, fmt.Errorf("parse funding schedule cursor: %w", err)
		}
		cursor, err := models.FundingSchedules.Query(sm.Where(psql.And(
			models.FundingSchedules.Columns.ID.EQ(psql.Arg(parsedID)),
			baseFilter,
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if boundary.isBefore {
			filters = append(filters, fundingScheduleCursorFilter(cursor, true))
		} else {
			filters = append(filters, fundingScheduleCursorFilter(cursor, false))
		}
	}
	var rows []*models.FundingSchedule
	if backward {
		rows, err = models.FundingSchedules.Query(
			sm.Where(psql.And(filters...)),
			sm.OrderBy(models.FundingSchedules.Columns.StartsOn).Desc(),
			sm.OrderBy(models.FundingSchedules.Columns.ID).Desc(),
			sm.Limit(pageSize+1),
		).All(ctx, executor)
	} else {
		rows, err = models.FundingSchedules.Query(
			sm.Where(psql.And(filters...)),
			sm.OrderBy(models.FundingSchedules.Columns.StartsOn),
			sm.OrderBy(models.FundingSchedules.Columns.ID),
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
	connection := &model.FundingScheduleConnection{
		Edges: make([]*model.FundingScheduleEdge, 0, len(rows)), TotalCount: int(count),
		PageInfo: &model.PageInfo{
			HasNextPage:     backward && before != nil && *before != "" || !backward && hasMore,
			HasPreviousPage: backward && hasMore || !backward && after != nil && *after != "",
		},
	}
	for _, row := range rows {
		connection.Edges = append(connection.Edges, &model.FundingScheduleEdge{
			Cursor: encodeCursor("FundingSchedule", row.ID.String()), Node: fundingScheduleModel(row),
		})
	}
	if len(rows) > 0 {
		start, end := encodeCursor("FundingSchedule", rows[0].ID.String()), encodeCursor("FundingSchedule", rows[len(rows)-1].ID.String())
		connection.PageInfo.StartCursor, connection.PageInfo.EndCursor = &start, &end
	}
	return connection, nil
}

func fundingScheduleCursorFilter(cursor *models.FundingSchedule, isBefore bool) bob.Expression {
	if isBefore {
		return psql.Or(
			models.FundingSchedules.Columns.StartsOn.LT(psql.Arg(cursor.StartsOn)),
			psql.And(
				models.FundingSchedules.Columns.StartsOn.EQ(psql.Arg(cursor.StartsOn)),
				models.FundingSchedules.Columns.ID.LT(psql.Arg(cursor.ID)),
			),
		)
	}
	return psql.Or(
		models.FundingSchedules.Columns.StartsOn.GT(psql.Arg(cursor.StartsOn)),
		psql.And(
			models.FundingSchedules.Columns.StartsOn.EQ(psql.Arg(cursor.StartsOn)),
			models.FundingSchedules.Columns.ID.GT(psql.Arg(cursor.ID)),
		),
	)
}
