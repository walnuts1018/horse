package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/99designs/gqlgen/graphql"
	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (r *queryResolver) ledger(ctx context.Context, householdID string, after, before *string, first, last *int) (*model.LedgerConnection, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	pageSize, backward, err := pageDirection(first, last)
	if err != nil {
		return nil, err
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	executor := queryExecutor(r.DB)
	filters := []bob.Expression{models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(hhID))}
	var afterCursorHasPreviousPage bool
	var count int64
	if ledgerTotalCountRequested(ctx) {
		count, err = models.LedgerEntries.Query(sm.Where(models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(hhID)))).Count(ctx, executor)
		if err != nil {
			return nil, err
		}
	}
	if cursorID, err := cursorID(after, nodeTypeLedgerEntry); err != nil {
		return nil, err
	} else if cursorID != nil {
		parsedCursorID, err := gofrsuuid.FromString(*cursorID)
		if err != nil {
			return nil, fmt.Errorf("parse ledger cursor: %w", err)
		}
		cursor, err := models.LedgerEntries.Query(sm.Where(psql.And(models.LedgerEntries.Columns.ID.EQ(psql.Arg(parsedCursorID)), models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(hhID))))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if !backward {
			afterCursorHasPreviousPage, err = ledgerCursorSideExists(ctx, executor, hhID, cursor, true)
			if err != nil {
				return nil, err
			}
		}
		filters = append(filters, psql.Or(models.LedgerEntries.Columns.OccurredAt.LT(psql.Arg(cursor.OccurredAt)), psql.And(models.LedgerEntries.Columns.OccurredAt.EQ(psql.Arg(cursor.OccurredAt)), models.LedgerEntries.Columns.ID.LT(psql.Arg(cursor.ID)))))
	}
	var beforeCursorHasNextPage bool
	if cursorID, err := cursorID(before, nodeTypeLedgerEntry); err != nil {
		return nil, err
	} else if cursorID != nil {
		parsedCursorID, err := gofrsuuid.FromString(*cursorID)
		if err != nil {
			return nil, fmt.Errorf("parse ledger cursor: %w", err)
		}
		cursor, err := models.LedgerEntries.Query(sm.Where(psql.And(models.LedgerEntries.Columns.ID.EQ(psql.Arg(parsedCursorID)), models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(hhID))))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if backward {
			beforeCursorHasNextPage, err = ledgerCursorSideExists(ctx, executor, hhID, cursor, false)
			if err != nil {
				return nil, err
			}
		}
		filters = append(filters, psql.Or(models.LedgerEntries.Columns.OccurredAt.GT(psql.Arg(cursor.OccurredAt)), psql.And(models.LedgerEntries.Columns.OccurredAt.EQ(psql.Arg(cursor.OccurredAt)), models.LedgerEntries.Columns.ID.GT(psql.Arg(cursor.ID)))))
	}
	occurredAtOrder := sm.OrderBy(models.LedgerEntries.Columns.OccurredAt).Desc()
	IDOrder := sm.OrderBy(models.LedgerEntries.Columns.ID).Desc()
	if backward {
		occurredAtOrder = sm.OrderBy(models.LedgerEntries.Columns.OccurredAt)
		IDOrder = sm.OrderBy(models.LedgerEntries.Columns.ID)
	}
	entries, err := models.LedgerEntries.Query(sm.Where(psql.And(filters...)), occurredAtOrder, IDOrder, sm.Limit(pageSize+1)).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	hasExtra := len(entries) > pageSize
	if hasExtra {
		entries = entries[:pageSize]
	}
	if backward {
		slices.Reverse(entries)
	}
	pageInfo := &model.PageInfo{HasPreviousPage: backward && hasExtra, HasNextPage: !backward && hasExtra}
	if !backward && after != nil && *after != "" {
		pageInfo.HasPreviousPage = afterCursorHasPreviousPage
	}
	if backward && before != nil && *before != "" {
		pageInfo.HasNextPage = beforeCursorHasNextPage
	}
	connection := &model.LedgerConnection{Edges: make([]*model.LedgerEdge, 0, len(entries)), TotalCount: int(count), PageInfo: pageInfo}
	for _, row := range entries {
		entry := &model.LedgerEntryData{ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), Kind: row.Kind, Currency: currencyJPY, OccurredAt: string(timeString(row.OccurredAt)), Description: row.Description, AmountYen: graphLong(row.AmountYen), CreatedAt: string(timeString(row.CreatedAt))}
		if row.FromMemberID.Valid {
			v := row.FromMemberID.V.String()
			entry.FromMemberID = &v
		}
		if row.ToMemberID.Valid {
			v := row.ToMemberID.V.String()
			entry.ToMemberID = &v
		}
		if row.PoolID.Valid {
			v := row.PoolID.V.String()
			entry.PoolID = &v
		}
		if row.SourcePurchaseID.Valid {
			v := row.SourcePurchaseID.V.String()
			entry.SourcePurchaseID = &v
		}
		variant, err := ledgerEntryVariant(ctx, r.DB, entry)
		if err != nil {
			return nil, err
		}
		connection.Edges = append(connection.Edges, &model.LedgerEdge{Cursor: encodeCursor(nodeTypeLedgerEntry, entry.ID), Node: variant})
	}
	if len(entries) > 0 {
		start := encodeCursor(nodeTypeLedgerEntry, entries[0].ID.String())
		end := encodeCursor(nodeTypeLedgerEntry, entries[len(entries)-1].ID.String())
		connection.PageInfo.StartCursor = &start
		connection.PageInfo.EndCursor = &end
	}
	return connection, nil
}

func ledgerTotalCountRequested(ctx context.Context) bool {
	return graphql.FieldRequested(ctx, "totalCount")
}

func ledgerCursorSideExists(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, cursor *models.LedgerEntry, newer bool) (bool, error) {
	var position bob.Expression
	if newer {
		position = psql.Or(
			models.LedgerEntries.Columns.OccurredAt.GT(psql.Arg(cursor.OccurredAt)),
			psql.And(
				models.LedgerEntries.Columns.OccurredAt.EQ(psql.Arg(cursor.OccurredAt)),
				models.LedgerEntries.Columns.ID.GT(psql.Arg(cursor.ID)),
			),
		)
	} else {
		position = psql.Or(
			models.LedgerEntries.Columns.OccurredAt.LT(psql.Arg(cursor.OccurredAt)),
			psql.And(
				models.LedgerEntries.Columns.OccurredAt.EQ(psql.Arg(cursor.OccurredAt)),
				models.LedgerEntries.Columns.ID.LT(psql.Arg(cursor.ID)),
			),
		)
	}
	return models.LedgerEntries.Query(sm.Where(psql.And(
		models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		position,
	))).Exists(ctx, executor)
}
