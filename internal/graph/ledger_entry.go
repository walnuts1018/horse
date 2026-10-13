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
	baseID, description, amount, currency := value.ID, value.Description, value.AmountYen, value.Currency
	occurredAt, createdAt := model.DateTime(value.OccurredAt), model.DateTime(value.CreatedAt)
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
			loan, err = loadLoanBalance(ctx, bobpgx.NewPool(db), loanRow)
			if err != nil {
				return nil, err
			}
		}
		return &model.TransferLedgerEntry{ID: baseID, OccurredAt: occurredAt, Description: description, AmountYen: amount, Currency: currency, CreatedAt: createdAt, FromMember: fromMember, ToMember: toMember, TransferKind: model.TransferKind(transfer.Kind), Loan: loan}, nil
	default:
		return nil, fmt.Errorf("unknown ledger entry kind %q", value.Kind)
	}
}
