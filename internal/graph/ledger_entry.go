package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func ledgerEntryVariant(ctx context.Context, db *pgxpool.Pool, value *model.LedgerEntryData) (model.LedgerEntry, error) {
	baseID, occurredAt, description, amount, currency, createdAt := value.ID, value.OccurredAt, value.Description, value.AmountYen, value.Currency, value.CreatedAt
	switch value.Kind {
	case "PURCHASE", "CORRECTION":
		if value.SourcePurchaseID == nil {
			return nil, errors.New("ledger entry has no source purchase")
		}
		purchase, err := loadPurchase(ctx, db, *value.SourcePurchaseID)
		if err != nil {
			return nil, err
		}
		if value.Kind == "PURCHASE" {
			return &model.PurchaseLedgerEntry{ID: baseID, OccurredAt: occurredAt, Description: description, AmountYen: amount, Currency: currency, CreatedAt: createdAt, Purchase: purchase}, nil
		}
		return &model.CorrectionLedgerEntry{ID: baseID, OccurredAt: occurredAt, Description: description, AmountYen: amount, Currency: currency, CreatedAt: createdAt, Purchase: purchase}, nil
	case "FUNDING":
		if value.FromMemberID == nil || value.PoolID == nil {
			return nil, errors.New("funding ledger entry is missing a member or pool")
		}
		household, err := loadHousehold(ctx, db, value.HouseholdID)
		if err != nil {
			return nil, err
		}
		member := findValue(household.Members, func(member *model.Member) bool { return member.ID == *value.FromMemberID })
		pool := findValue(household.Pools, func(pool *model.Pool) bool { return pool.ID == *value.PoolID })
		if member == nil || pool == nil {
			return nil, ErrNotFound
		}
		return &model.FundingLedgerEntry{ID: baseID, OccurredAt: occurredAt, Description: description, AmountYen: amount, Currency: currency, CreatedAt: createdAt, Member: member, Pool: pool}, nil
	case "TRANSFER":
		if value.FromMemberID == nil || value.ToMemberID == nil || value.TransferID == nil {
			return nil, errors.New("transfer ledger entry is incomplete")
		}
		household, err := loadHousehold(ctx, db, value.HouseholdID)
		if err != nil {
			return nil, err
		}
		fromMember := findValue(household.Members, func(member *model.Member) bool { return member.ID == *value.FromMemberID })
		toMember := findValue(household.Members, func(member *model.Member) bool { return member.ID == *value.ToMemberID })
		if fromMember == nil || toMember == nil {
			return nil, ErrNotFound
		}
		transferID, err := uuid.FromString(*value.TransferID)
		if err != nil {
			return nil, err
		}
		transfer, err := models.Transfers.Query(sm.Where(models.Transfers.Columns.ID.EQ(psql.Arg(transferID)))).One(ctx, bobpgx.NewPool(db))
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		var loan *model.LoanBalance
		if transfer.LoanID.Valid {
			loanRow, err := models.Transfers.Query(sm.Where(models.Transfers.Columns.ID.EQ(psql.Arg(transfer.LoanID.V)))).One(ctx, bobpgx.NewPool(db))
			if err != nil {
				return nil, err
			}
			loanLedger, err := models.LedgerEntries.Query(sm.Where(models.LedgerEntries.Columns.ID.EQ(psql.Arg(loanRow.LedgerEntryID)))).One(ctx, bobpgx.NewPool(db))
			if err != nil {
				return nil, err
			}
			repayments, err := models.Transfers.Query(sm.Where(psql.And(models.Transfers.Columns.HouseholdID.EQ(psql.Arg(loanRow.HouseholdID)), models.Transfers.Columns.LoanID.EQ(psql.Arg(sql.Null[uuid.UUID]{V: loanRow.ID, Valid: true}))))).All(ctx, bobpgx.NewPool(db))
			if err != nil {
				return nil, err
			}
			var repaid int64
			for _, repayment := range repayments {
				repaid += repayment.AmountYen
			}
			loan = &model.LoanBalance{LoanID: loanRow.ID.String(), LenderMemberID: loanRow.FromMemberID.String(), BorrowerMemberID: loanRow.ToMemberID.String(), PrincipalYen: graphLong(loanRow.AmountYen), RepaidYen: graphLong(repaid), BalanceYen: graphLong(loanRow.AmountYen - repaid), CreatedAt: timeString(loanLedger.CreatedAt)}
		}
		return &model.TransferLedgerEntry{ID: baseID, OccurredAt: occurredAt, Description: description, AmountYen: amount, Currency: currency, CreatedAt: createdAt, FromMember: fromMember, ToMember: toMember, TransferKind: model.TransferKind(transfer.Kind), Loan: loan}, nil
	default:
		return nil, fmt.Errorf("unknown ledger entry kind %q", value.Kind)
	}
}
