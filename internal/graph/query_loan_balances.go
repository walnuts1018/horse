package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/scan"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type loanBalanceCursor struct {
	ID        gofrsuuid.UUID `db:"id"`
	CreatedAt time.Time      `db:"created_at"`
}

type loanBalanceRow struct {
	ID            gofrsuuid.UUID `db:"id"`
	LedgerEntryID gofrsuuid.UUID `db:"ledger_entry_id"`
	FromMemberID  gofrsuuid.UUID `db:"from_member_id"`
	ToMemberID    gofrsuuid.UUID `db:"to_member_id"`
	AmountYen     int64          `db:"amount_yen"`
	CreatedAt     time.Time      `db:"created_at"`
}

type loanRepaymentTotal struct {
	LoanID    gofrsuuid.UUID `db:"loan_id"`
	AmountYen string         `db:"amount_yen"`
}

func (r *queryResolver) loanBalances(ctx context.Context, householdID string, after, before *string, first, last *int) (*model.LoanBalanceConnection, error) {
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
	loanFilter := psql.And(
		models.Transfers.Columns.HouseholdID.EQ(psql.Arg(hhID)),
		models.Transfers.Columns.Kind.EQ(psql.Arg(transferKindLoan)),
	)
	count, err := models.Transfers.Query(sm.Where(loanFilter)).Count(ctx, executor)
	if err != nil {
		return nil, err
	}

	filters := []bob.Expression{loanFilter}
	var afterCursorHasPreviousPage, beforeCursorHasNextPage bool
	for _, boundary := range []struct {
		cursor   *string
		isBefore bool
	}{{cursor: after}, {cursor: before, isBefore: true}} {
		id, err := cursorID(boundary.cursor, nodeTypeLoanBalance)
		if err != nil {
			return nil, err
		}
		if id == nil {
			continue
		}
		parsedID, err := gofrsuuid.FromString(*id)
		if err != nil {
			return nil, ErrNotFound
		}
		loan, err := models.Transfers.Query(sm.Where(psql.And(
			models.Transfers.Columns.ID.EQ(psql.Arg(parsedID)),
			loanFilter,
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		ledger, err := models.LedgerEntries.Query(sm.Where(psql.And(
			models.LedgerEntries.Columns.ID.EQ(psql.Arg(loan.LedgerEntryID)),
			models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(hhID)),
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		cursor := &loanBalanceCursor{ID: loan.ID, CreatedAt: ledger.CreatedAt}
		if boundary.isBefore {
			beforeCursorHasNextPage, err = loanBalanceCursorSideExists(ctx, executor, hhID, loanBalanceAfterFilter(cursor))
			if err != nil {
				return nil, err
			}
			filters = append(filters, loanBalanceBeforeFilter(cursor))
		} else {
			afterCursorHasPreviousPage, err = loanBalanceCursorSideExists(ctx, executor, hhID, loanBalanceBeforeFilter(cursor))
			if err != nil {
				return nil, err
			}
			filters = append(filters, loanBalanceAfterFilter(cursor))
		}
	}
	joins := sm.From(models.Transfers.NameExpr(), sm.InnerJoin(models.LedgerEntries.NameExpr()).On(
		models.LedgerEntries.Columns.ID.EQ(models.Transfers.Columns.LedgerEntryID),
	))
	pageFilters := append(filters, models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(hhID)))
	columns := sm.Columns(
		models.Transfers.Columns.ID.As("id"),
		models.Transfers.Columns.LedgerEntryID.As("ledger_entry_id"),
		models.Transfers.Columns.FromMemberID.As("from_member_id"),
		models.Transfers.Columns.ToMemberID.As("to_member_id"),
		models.Transfers.Columns.AmountYen.As("amount_yen"),
		models.LedgerEntries.Columns.CreatedAt.As("created_at"),
	)
	var rows []loanBalanceRow
	if backward {
		rows, err = bob.All(ctx, executor, psql.Select(
			columns,
			joins,
			sm.Where(psql.And(pageFilters...)),
			sm.OrderBy(models.LedgerEntries.Columns.CreatedAt),
			sm.OrderBy(models.Transfers.Columns.ID).Desc(),
			sm.Limit(pageSize+1),
		), scan.StructMapper[loanBalanceRow]())
	} else {
		rows, err = bob.All(ctx, executor, psql.Select(
			columns,
			joins,
			sm.Where(psql.And(pageFilters...)),
			sm.OrderBy(models.LedgerEntries.Columns.CreatedAt).Desc(),
			sm.OrderBy(models.Transfers.Columns.ID),
			sm.Limit(pageSize+1),
		), scan.StructMapper[loanBalanceRow]())
	}
	if err != nil {
		return nil, err
	}
	hasExtra := len(rows) > pageSize
	if hasExtra {
		rows = rows[:pageSize]
	}
	if backward {
		slices.Reverse(rows)
	}

	repaidByLoan := make(map[gofrsuuid.UUID]int64, len(rows))
	if len(rows) > 0 {
		loanIDs := make([]bob.Expression, 0, len(rows))
		for _, row := range rows {
			loanIDs = append(loanIDs, psql.Arg(row.ID))
		}
		totals, err := bob.All(ctx, executor, psql.Select(
			sm.Columns(
				models.Transfers.Columns.LoanID.As("loan_id"),
				psql.F("sum", models.Transfers.Columns.AmountYen)().As("amount_yen"),
			),
			sm.From(models.Transfers.NameExpr()),
			sm.Where(psql.And(
				models.Transfers.Columns.HouseholdID.EQ(psql.Arg(hhID)),
				models.Transfers.Columns.Kind.EQ(psql.Arg("REPAYMENT")),
				models.Transfers.Columns.LoanID.In(loanIDs...),
			)),
			sm.GroupBy(models.Transfers.Columns.LoanID),
		), scan.StructMapper[loanRepaymentTotal]())
		if err != nil {
			return nil, err
		}
		for _, total := range totals {
			amount, err := strconv.ParseInt(total.AmountYen, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("loan %s repayment total overflow: %w", total.LoanID, err)
			}
			repaidByLoan[total.LoanID] = amount
		}
	}

	pageInfo := &model.PageInfo{
		HasPreviousPage: backward && (hasExtra || afterCursorHasPreviousPage) || !backward && afterCursorHasPreviousPage,
		HasNextPage:     backward && beforeCursorHasNextPage || !backward && (hasExtra || beforeCursorHasNextPage),
	}
	connection := &model.LoanBalanceConnection{Edges: make([]*model.LoanBalanceEdge, 0, len(rows)), TotalCount: int(count), PageInfo: pageInfo}
	for _, row := range rows {
		globalID, err := encodeNodeID(nodeTypeLoanBalance, row.ID.String())
		if err != nil {
			return nil, err
		}
		repaid := repaidByLoan[row.ID]
		balance := &model.LoanBalance{
			ID: globalID, LenderMemberID: row.FromMemberID.String(), BorrowerMemberID: row.ToMemberID.String(),
			PrincipalYen: graphLong(row.AmountYen), RepaidYen: graphLong(repaid), BalanceYen: graphLong(row.AmountYen - repaid),
			CreatedAt: timeString(row.CreatedAt),
		}
		connection.Edges = append(connection.Edges, &model.LoanBalanceEdge{Cursor: encodeCursor(nodeTypeLoanBalance, row.ID.String()), Node: balance})
	}
	if len(rows) > 0 {
		startCursor := encodeCursor(nodeTypeLoanBalance, rows[0].ID.String())
		endCursor := encodeCursor(nodeTypeLoanBalance, rows[len(rows)-1].ID.String())
		connection.PageInfo.StartCursor, connection.PageInfo.EndCursor = &startCursor, &endCursor
	}
	return connection, nil
}

func loanBalanceAfterFilter(cursor *loanBalanceCursor) bob.Expression {
	return psql.Or(
		models.LedgerEntries.Columns.CreatedAt.LT(psql.Arg(cursor.CreatedAt)),
		psql.And(
			models.LedgerEntries.Columns.CreatedAt.EQ(psql.Arg(cursor.CreatedAt)),
			models.Transfers.Columns.ID.GT(psql.Arg(cursor.ID)),
		),
	)
}

func loanBalanceBeforeFilter(cursor *loanBalanceCursor) bob.Expression {
	return psql.Or(
		models.LedgerEntries.Columns.CreatedAt.GT(psql.Arg(cursor.CreatedAt)),
		psql.And(
			models.LedgerEntries.Columns.CreatedAt.EQ(psql.Arg(cursor.CreatedAt)),
			models.Transfers.Columns.ID.LT(psql.Arg(cursor.ID)),
		),
	)
}

func loanBalanceCursorSideExists(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, position bob.Expression) (bool, error) {
	return models.Transfers.Query(
		sm.InnerJoin(models.LedgerEntries.NameExpr()).On(models.LedgerEntries.Columns.ID.EQ(models.Transfers.Columns.LedgerEntryID)),
		sm.Where(psql.And(
			models.Transfers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Transfers.Columns.Kind.EQ(psql.Arg(transferKindLoan)),
			models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			position,
		)),
	).Exists(ctx, executor)
}
