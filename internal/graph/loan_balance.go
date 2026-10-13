package graph

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func loadLoanBalance(ctx context.Context, db bob.Executor, loan *models.Transfer) (*model.LoanBalance, error) {
	ledger, err := models.LedgerEntries.Query(sm.Where(models.LedgerEntries.Columns.ID.EQ(psql.Arg(loan.LedgerEntryID)))).One(ctx, db)
	if err != nil {
		return nil, err
	}
	loanID := sql.Null[uuid.UUID]{V: loan.ID, Valid: true}
	repayments, err := models.Transfers.Query(sm.Where(psql.And(
		models.Transfers.Columns.HouseholdID.EQ(psql.Arg(loan.HouseholdID)),
		models.Transfers.Columns.LoanID.EQ(psql.Arg(loanID)),
		models.Transfers.Columns.Kind.EQ(psql.Arg("REPAYMENT")),
	))).All(ctx, db)
	if err != nil {
		return nil, err
	}
	var repaid int64
	for _, repayment := range repayments {
		var ok bool
		repaid, ok = checkedAdd(repaid, repayment.AmountYen)
		if !ok {
			return nil, fmt.Errorf("loan %s repayment total overflow", loan.ID)
		}
	}
	globalID, err := encodeNodeID(nodeTypeLoanBalance, loan.ID.String())
	if err != nil {
		return nil, err
	}
	return &model.LoanBalance{
		ID: globalID, LenderMemberID: loan.FromMemberID.String(), BorrowerMemberID: loan.ToMemberID.String(),
		PrincipalYen: graphLong(loan.AmountYen), RepaidYen: graphLong(repaid),
		BalanceYen: graphLong(loan.AmountYen - repaid), CreatedAt: timeString(ledger.CreatedAt),
	}, nil
}
