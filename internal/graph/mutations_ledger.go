package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func loadLedgerEntry(ctx context.Context, db *pgxpool.Pool, entryID string) (*model.LedgerEntryData, error) {
	entryUUID, err := gofrsuuid.FromString(entryID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid ledger entry id", ErrBadInput)
	}
	row, err := models.LedgerEntries.Query(sm.Where(models.LedgerEntries.Columns.ID.EQ(psql.Arg(entryUUID)))).One(ctx, bobpgx.NewPool(db))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	result := &model.LedgerEntryData{
		ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), Kind: row.Kind, OccurredAt: string(timeString(row.OccurredAt)),
		Description: row.Description, AmountYen: graphLong(row.AmountYen), Currency: currencyJPY, CreatedAt: string(timeString(row.CreatedAt)),
	}
	if row.FromMemberID.Valid {
		value := row.FromMemberID.V.String()
		result.FromMemberID = &value
	}
	if row.ToMemberID.Valid {
		value := row.ToMemberID.V.String()
		result.ToMemberID = &value
	}
	if row.PoolID.Valid {
		value := row.PoolID.V.String()
		result.PoolID = &value
	}
	if row.SourcePurchaseID.Valid {
		value := row.SourcePurchaseID.V.String()
		result.SourcePurchaseID = &value
	}
	transfer, transferErr := models.Transfers.Query(sm.Where(models.Transfers.Columns.LedgerEntryID.EQ(psql.Arg(row.ID)))).One(ctx, bobpgx.NewPool(db))
	if transferErr == nil {
		transferID := transfer.ID.String()
		result.TransferID = &transferID
		if transfer.LoanID.Valid {
			loanID := transfer.LoanID.V.String()
			result.LoanID = &loanID
		}
	} else if !errors.Is(transferErr, sql.ErrNoRows) {
		return nil, transferErr
	}
	if result.LoanID == nil && result.Kind == sourceKindTransfer && result.TransferID != nil {
		if transferErr != nil {
			return nil, transferErr
		}
		if transfer.Kind == transferKindLoan {
			result.LoanID = result.TransferID
		}
	}
	return result, nil
}

type transferCommand struct {
	HouseholdID          string     `json:"householdId"`
	FromMemberID         string     `json:"fromMemberId"`
	ToMemberID           string     `json:"toMemberId"`
	Kind                 string     `json:"kind"`
	LoanID               *string    `json:"loanId,omitempty"`
	PaymentAccountID     *string    `json:"paymentAccountId,omitempty"`
	DestinationAccountID *string    `json:"destinationAccountId,omitempty"`
	AmountYen            model.Long `json:"amountYen"`
	OccurredAt           string     `json:"occurredAt"`
	Description          string     `json:"description"`
	IdempotencyKey       string     `json:"idempotencyKey"`
}

func (r *mutationResolver) recordTransfer(ctx context.Context, input model.RecordTransferInput) (*model.LedgerEntryData, error) {
	return r.recordTransferCommand(ctx, transferCommand{
		HouseholdID: input.HouseholdID, FromMemberID: input.FromMemberID, ToMemberID: input.ToMemberID,
		Kind: string(input.Kind), PaymentAccountID: input.PaymentAccountID,
		DestinationAccountID: input.DestinationAccountID, AmountYen: input.AmountYen,
		OccurredAt: string(input.OccurredAt), Description: input.Description, IdempotencyKey: input.IdempotencyKey,
	})
}

func (r *mutationResolver) recordRepayment(ctx context.Context, input model.RecordRepaymentInput) (*model.LedgerEntryData, error) {
	return r.recordTransferCommand(ctx, transferCommand{
		HouseholdID: input.HouseholdID, FromMemberID: input.FromMemberID, ToMemberID: input.ToMemberID,
		Kind: transferKindRepayment, LoanID: &input.LoanID, PaymentAccountID: input.PaymentAccountID,
		DestinationAccountID: input.DestinationAccountID, AmountYen: input.AmountYen,
		OccurredAt: string(input.OccurredAt), Description: input.Description, IdempotencyKey: input.IdempotencyKey,
	})
}
