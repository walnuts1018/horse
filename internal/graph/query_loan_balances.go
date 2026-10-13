package graph

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (r *queryResolver) loanBalances(ctx context.Context, householdID string, after, before *string, first, last *int) (*model.LoanBalanceConnection, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	pageSize, backward, err := pageDirection(after, before, first, last)
	if err != nil {
		return nil, err
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	executor := queryExecutor(r.DB)
	transfers, err := models.Transfers.Query(sm.Where(models.Transfers.Columns.HouseholdID.EQ(psql.Arg(hhID)))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	repaidByLoan := make(map[gofrsuuid.UUID]int64)
	loans := make([]*models.Transfer, 0)
	for _, transfer := range transfers {
		if transfer.Kind == "REPAYMENT" && transfer.LoanID.Valid {
			total, ok := checkedAdd(repaidByLoan[transfer.LoanID.V], transfer.AmountYen)
			if !ok {
				return nil, fmt.Errorf("loan %s repayment total overflow", transfer.LoanID.V)
			}
			repaidByLoan[transfer.LoanID.V] = total
		}
		if transfer.Kind == "LOAN" {
			loans = append(loans, transfer)
		}
	}
	ledgerEntries, err := models.LedgerEntries.Query(sm.Where(models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(hhID)))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	ledgerByID := make(map[gofrsuuid.UUID]*models.LedgerEntry, len(ledgerEntries))
	for _, entry := range ledgerEntries {
		ledgerByID[entry.ID] = entry
	}
	slices.SortFunc(loans, func(a, b *models.Transfer) int {
		left, right := ledgerByID[a.LedgerEntryID], ledgerByID[b.LedgerEntryID]
		if left != nil && right != nil {
			if n := right.CreatedAt.Compare(left.CreatedAt); n != 0 {
				return n
			}
		}
		return cmp.Compare(a.ID.String(), b.ID.String())
	})
	loanCount := len(loans)
	start, end := 0, len(loans)
	if id, err := cursorID(after, "LoanBalance"); err != nil {
		return nil, err
	} else if id != nil {
		cursorIndex := slices.IndexFunc(loans, func(loan *models.Transfer) bool { return loan.ID.String() == *id })
		if cursorIndex < 0 {
			return nil, ErrNotFound
		}
		start = cursorIndex + 1
	}
	if id, err := cursorID(before, "LoanBalance"); err != nil {
		return nil, err
	} else if id != nil {
		cursorIndex := slices.IndexFunc(loans, func(loan *models.Transfer) bool { return loan.ID.String() == *id })
		if cursorIndex < 0 {
			return nil, ErrNotFound
		}
		end = cursorIndex
	}
	if start > end {
		return nil, fmt.Errorf("%w: cursor range is empty", ErrBadInput)
	}
	pageStart, pageEnd := start, end
	if backward {
		pageStart = max(start, end-pageSize)
	} else {
		pageEnd = min(end, start+pageSize)
	}
	pageLoans := loans[pageStart:pageEnd]
	connection := &model.LoanBalanceConnection{Edges: make([]*model.LoanBalanceEdge, 0, len(pageLoans)), TotalCount: loanCount, PageInfo: &model.PageInfo{HasNextPage: pageEnd < loanCount, HasPreviousPage: pageStart > 0}}
	for _, loan := range pageLoans {
		ledger := ledgerByID[loan.LedgerEntryID]
		if ledger == nil {
			return nil, fmt.Errorf("loan transfer %s has no ledger entry", loan.ID)
		}
		globalID, err := encodeNodeID("LoanBalance", loan.ID.String())
		if err != nil {
			return nil, err
		}
		balance := &model.LoanBalance{ID: globalID, LenderMemberID: loan.FromMemberID.String(), BorrowerMemberID: loan.ToMemberID.String(), PrincipalYen: graphLong(loan.AmountYen), RepaidYen: graphLong(repaidByLoan[loan.ID]), BalanceYen: graphLong(loan.AmountYen - repaidByLoan[loan.ID]), CreatedAt: timeString(ledger.CreatedAt)}
		connection.Edges = append(connection.Edges, &model.LoanBalanceEdge{Cursor: encodeCursor("LoanBalance", loan.ID.String()), Node: balance})
	}
	if len(pageLoans) > 0 {
		startCursor, endCursor := encodeCursor("LoanBalance", pageLoans[0].ID.String()), encodeCursor("LoanBalance", pageLoans[len(pageLoans)-1].ID.String())
		connection.PageInfo.StartCursor, connection.PageInfo.EndCursor = &startCursor, &endCursor
	}
	return connection, nil
}
