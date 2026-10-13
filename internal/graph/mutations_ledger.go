package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (r *mutationResolver) recordFunding(ctx context.Context, input model.RecordFundingInput) (*model.LedgerEntryData, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	if input.AmountYen <= 0 || int64(input.AmountYen) > maxMoney || input.IdempotencyKey == "" {
		return nil, fmt.Errorf("%w: invalid funding amount or idempotency key", ErrBadInput)
	}
	occurredAt, err := parseTime(input.OccurredAt)
	if err != nil {
		return nil, err
	}
	if err := verifyHouseholdMember(ctx, r.DB, input.HouseholdID, input.MemberID); err != nil {
		return nil, err
	}
	householdID, err := gofrsuuid.FromString(input.HouseholdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	memberID, err := gofrsuuid.FromString(input.MemberID)
	if err != nil {
		return nil, fmt.Errorf("parse member id: %w", err)
	}
	poolID, err := gofrsuuid.FromString(input.PoolID)
	if err != nil {
		return nil, fmt.Errorf("parse pool id: %w", err)
	}
	executor := bobpgx.NewPool(r.DB)
	poolExists, err := models.Pools.Query(sm.Where(psql.And(
		models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Pools.Columns.ID.EQ(psql.Arg(poolID)),
	))).Exists(ctx, executor)
	if err != nil {
		return nil, err
	}
	if !poolExists {
		return nil, ErrNotFound
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	transactionExecutor := bobpgx.NewTx(tx, nil)
	operation := "recordFunding"
	fingerprint, err := mutationRequestFingerprint(input)
	if err != nil {
		return nil, err
	}
	existing, err := models.IdempotencyKeys.Query(sm.Where(psql.And(
		models.IdempotencyKeys.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.IdempotencyKeys.Columns.Key.EQ(psql.Arg(input.IdempotencyKey)),
		models.IdempotencyKeys.Columns.Operation.EQ(psql.Arg(operation)),
	))).One(ctx, transactionExecutor)
	if err == nil && existing.ResourceID.Valid {
		if err := validateIdempotencyFingerprint(existing, fingerprint); err != nil {
			return nil, err
		}
		if err := tx.Rollback(ctx); err != nil {
			return nil, err
		}
		return loadLedgerEntry(ctx, r.DB, existing.ResourceID.V.String())
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	period := accountingPeriod(occurredAt)
	if _, err := models.AccountingPeriods.Insert(&models.AccountingPeriodSetter{
		HouseholdID: &householdID, Period: &period, Status: new("OPEN"), Revision: new(int64(0)),
	}, im.OnConflict("household_id", "period").DoNothing()).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	periodRow, err := models.AccountingPeriods.Query(sm.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(period)),
	)), sm.ForUpdate()).One(ctx, transactionExecutor)
	if err != nil {
		return nil, err
	}
	if periodRow.Status == "CLOSED" {
		return nil, fmt.Errorf("%w: accounting period is closed", ErrConflict)
	}
	var sourceAccountID, destinationAccountID *gofrsuuid.UUID
	if input.SourceAccountID != nil {
		id, parseErr := gofrsuuid.FromString(*input.SourceAccountID)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: invalid source account id", ErrBadInput)
		}
		account, accountErr := models.Accounts.Query(sm.Where(psql.And(
			models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Accounts.Columns.ID.EQ(psql.Arg(id)),
		))).One(ctx, transactionExecutor)
		if errors.Is(accountErr, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if accountErr != nil {
			return nil, accountErr
		}
		if account.Kind == string(model.AccountKindCard) {
			return nil, fmt.Errorf("%w: funding source must be a non-card account", ErrBadInput)
		}
		if account.PoolID.Valid {
			pool, poolErr := models.Pools.Query(sm.Where(psql.And(
				models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdID)),
				models.Pools.Columns.ID.EQ(psql.Arg(account.PoolID.V)),
			))).One(ctx, transactionExecutor)
			if poolErr != nil {
				return nil, poolErr
			}
			if pool.Kind == string(model.PoolKindPersonal) {
				fundingRule, ruleErr := models.PoolFundingRules.Query(sm.Where(psql.And(
					models.PoolFundingRules.Columns.HouseholdID.EQ(psql.Arg(householdID)),
					models.PoolFundingRules.Columns.PoolID.EQ(psql.Arg(pool.ID)),
					models.PoolFundingRules.Columns.Version.EQ(psql.Arg(pool.FundingRuleVersion)),
				))).One(ctx, transactionExecutor)
				if ruleErr != nil {
					return nil, ruleErr
				}
				if !fundingRule.OwnerMemberID.Valid || fundingRule.OwnerMemberID.V != memberID {
					return nil, fmt.Errorf("%w: personal source account must belong to the funding member", ErrBadInput)
				}
			}
		}
		sourceAccountID = &id
	}
	if input.DestinationAccountID != nil {
		id, parseErr := gofrsuuid.FromString(*input.DestinationAccountID)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: invalid destination account id", ErrBadInput)
		}
		account, accountErr := models.Accounts.Query(sm.Where(psql.And(
			models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Accounts.Columns.ID.EQ(psql.Arg(id)),
		))).One(ctx, transactionExecutor)
		if errors.Is(accountErr, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if accountErr != nil {
			return nil, accountErr
		}
		if !account.PoolID.Valid || account.PoolID.V != poolID || account.Kind == string(model.AccountKindCard) {
			return nil, fmt.Errorf("%w: funding destination must be a non-card account in the funded pool", ErrBadInput)
		}
		destinationAccountID = &id
	}
	if sourceAccountID != nil && destinationAccountID != nil && *sourceAccountID == *destinationAccountID {
		return nil, fmt.Errorf("%w: funding source and destination accounts must differ", ErrBadInput)
	}
	schedules, err := models.FundingSchedules.Query(sm.Where(psql.And(
		models.FundingSchedules.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.FundingSchedules.Columns.MemberID.EQ(psql.Arg(memberID)),
		models.FundingSchedules.Columns.PoolID.EQ(psql.Arg(poolID)),
		models.FundingSchedules.Columns.Active.EQ(psql.Arg(true)),
	)), sm.OrderBy(models.FundingSchedules.Columns.CreatedAt).Desc(), sm.Limit(1)).All(ctx, transactionExecutor)
	if err != nil {
		return nil, err
	}
	var scheduleID sql.Null[gofrsuuid.UUID]
	if len(schedules) > 0 {
		scheduleID = sql.Null[gofrsuuid.UUID]{V: schedules[0].ID, Valid: true}
	}
	obligationUUID := generatedUUID(newID())
	amount := int64(input.AmountYen)
	if _, err := models.FundingObligations.Insert(&models.FundingObligationSetter{
		ID: &obligationUUID, HouseholdID: &householdID, ScheduleID: &scheduleID, MemberID: &memberID,
		PoolID: &poolID, Period: &period, AmountYen: new(int64(0)), PaidYen: &amount,
	}, im.OnConflict("household_id", "member_id", "pool_id", "period").DoUpdate(
		im.SetCol("paid_yen").To(psql.Quote("funding_obligations", "paid_yen").Plus(im.Excluded("paid_yen"))),
	)).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	journalUUID, ledgerUUID := generatedUUID(newID()), generatedUUID(newID())
	sourceKind := string("FUNDING")
	if _, err := models.Journals.Insert(&models.JournalSetter{
		ID: &journalUUID, HouseholdID: &householdID, SourceKind: &sourceKind, SourceID: &ledgerUUID,
		IdempotencyKey: &input.IdempotencyKey, OccurredAt: &occurredAt, CreatedBy: &memberID,
	}).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	poolReference := sql.Null[gofrsuuid.UUID]{V: poolID, Valid: true}
	if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
		JournalID: &journalUUID, LineNo: new(int32(1)), PoolID: &poolReference, DebitYen: &amount, Memo: new("Pool funding"),
	}).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
		JournalID: &journalUUID, LineNo: new(int32(2)), PoolID: &poolReference, DebitYen: new(int64(0)), CreditYen: &amount, Memo: new("Member contribution"),
	}).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	if sourceAccountID != nil || destinationAccountID != nil {
		noAccount, noPool, noMember := sql.Null[gofrsuuid.UUID]{}, sql.Null[gofrsuuid.UUID]{}, sql.Null[gofrsuuid.UUID]{}
		lineThree, lineFour := int32(3), int32(4)
		sourceDebit, sourceCredit := int64(0), amount
		destinationDebit, destinationCredit := amount, int64(0)
		sourceAccount := noAccount
		destinationAccount := noAccount
		if sourceAccountID == nil {
			sourceCredit = 0
		} else {
			sourceAccount = sql.Null[gofrsuuid.UUID]{V: *sourceAccountID, Valid: true}
		}
		if destinationAccountID != nil {
			destinationAccount = sql.Null[gofrsuuid.UUID]{V: *destinationAccountID, Valid: true}
		}
		if sourceAccountID == nil && destinationAccountID != nil {
			sourceDebit = 0
			sourceCredit = amount
		}
		if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
			JournalID: &journalUUID, LineNo: &lineThree, AccountID: &destinationAccount, PoolID: &noPool, MemberID: &noMember,
			DebitYen: &destinationDebit, CreditYen: &destinationCredit, Memo: new("Funding destination"),
		}).Exec(ctx, transactionExecutor); err != nil {
			return nil, err
		}
		if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
			JournalID: &journalUUID, LineNo: &lineFour, AccountID: &sourceAccount, PoolID: &noPool, MemberID: &noMember,
			DebitYen: &sourceDebit, CreditYen: &sourceCredit, Memo: new("Funding source"),
		}).Exec(ctx, transactionExecutor); err != nil {
			return nil, err
		}
	}
	memberReference := sql.Null[gofrsuuid.UUID]{V: memberID, Valid: true}
	if _, err := models.LedgerEntries.Insert(&models.LedgerEntrySetter{
		ID: &ledgerUUID, HouseholdID: &householdID, JournalID: &journalUUID, Kind: new(string("FUNDING")),
		OccurredAt: &occurredAt, Description: new("Pool funding"), AmountYen: &amount,
		FromMemberID: &memberReference, PoolID: &poolReference,
	}).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	resourceID := sql.Null[gofrsuuid.UUID]{V: ledgerUUID, Valid: true}
	requestFingerprint := sql.Null[[]byte]{V: fingerprint[:], Valid: true}
	if _, err := models.IdempotencyKeys.Insert(&models.IdempotencyKeySetter{
		HouseholdID: &householdID, Key: &input.IdempotencyKey, Operation: &operation, ResourceID: &resourceID,
		RequestFingerprint: &requestFingerprint,
	}).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "funding.recorded", ledgerUUID.String(), 1, []string{"amountYen"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadLedgerEntry(ctx, r.DB, ledgerUUID.String())
}

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
		Description: row.Description, AmountYen: graphLong(row.AmountYen), Currency: "JPY", CreatedAt: string(timeString(row.CreatedAt)),
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
	if result.LoanID == nil && result.Kind == "TRANSFER" && result.TransferID != nil {
		if transferErr != nil {
			return nil, transferErr
		}
		if transfer.Kind == string(model.TransferKindLoan) {
			result.LoanID = result.TransferID
		}
	}
	return result, nil
}

type transferCommand struct {
	HouseholdID          string             `json:"householdId"`
	FromMemberID         string             `json:"fromMemberId"`
	ToMemberID           string             `json:"toMemberId"`
	Kind                 model.TransferKind `json:"kind"`
	LoanID               *string            `json:"loanId,omitempty"`
	PaymentAccountID     *string            `json:"paymentAccountId,omitempty"`
	DestinationAccountID *string            `json:"destinationAccountId,omitempty"`
	AmountYen            model.Long         `json:"amountYen"`
	OccurredAt           string             `json:"occurredAt"`
	Description          string             `json:"description"`
	IdempotencyKey       string             `json:"idempotencyKey"`
}

func (r *mutationResolver) recordTransfer(ctx context.Context, input model.RecordTransferInput) (*model.LedgerEntryData, error) {
	return r.recordTransferCommand(ctx, transferCommand{
		HouseholdID: input.HouseholdID, FromMemberID: input.FromMemberID, ToMemberID: input.ToMemberID,
		Kind: model.TransferKind(input.Kind), PaymentAccountID: input.PaymentAccountID,
		DestinationAccountID: input.DestinationAccountID, AmountYen: input.AmountYen,
		OccurredAt: string(input.OccurredAt), Description: input.Description, IdempotencyKey: input.IdempotencyKey,
	})
}

func (r *mutationResolver) recordRepayment(ctx context.Context, input model.RecordRepaymentInput) (*model.LedgerEntryData, error) {
	return r.recordTransferCommand(ctx, transferCommand{
		HouseholdID: input.HouseholdID, FromMemberID: input.FromMemberID, ToMemberID: input.ToMemberID,
		Kind: model.TransferKindRepayment, LoanID: &input.LoanID, PaymentAccountID: input.PaymentAccountID,
		DestinationAccountID: input.DestinationAccountID, AmountYen: input.AmountYen,
		OccurredAt: string(input.OccurredAt), Description: input.Description, IdempotencyKey: input.IdempotencyKey,
	})
}

func (r *mutationResolver) recordTransferCommand(ctx context.Context, input transferCommand) (*model.LedgerEntryData, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	if input.AmountYen <= 0 || int64(input.AmountYen) > maxMoney || input.IdempotencyKey == "" || !input.Kind.IsValid() {
		return nil, fmt.Errorf("%w: invalid transfer", ErrBadInput)
	}
	if input.FromMemberID == input.ToMemberID {
		return nil, fmt.Errorf("%w: transfer participants must differ", ErrBadInput)
	}
	if err := verifyHouseholdMember(ctx, r.DB, input.HouseholdID, input.FromMemberID); err != nil {
		return nil, err
	}
	if err := verifyHouseholdMember(ctx, r.DB, input.HouseholdID, input.ToMemberID); err != nil {
		return nil, err
	}
	occurredAt, err := parseTime(input.OccurredAt)
	if err != nil {
		return nil, err
	}
	if input.Kind == model.TransferKindRepayment && input.LoanID == nil {
		return nil, fmt.Errorf("%w: repayment requires a loan", ErrBadInput)
	}
	if input.Kind != model.TransferKindRepayment && input.LoanID != nil {
		return nil, fmt.Errorf("%w: loanId is only valid for a repayment", ErrBadInput)
	}
	var paymentPoolID *gofrsuuid.UUID
	var paymentPoolKind string
	var paymentAccountUUID *gofrsuuid.UUID
	var destinationAccountUUID *gofrsuuid.UUID
	if input.PaymentAccountID != nil {
		accountID, err := gofrsuuid.FromString(*input.PaymentAccountID)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid payment account id", ErrBadInput)
		}
		householdID, err := gofrsuuid.FromString(input.HouseholdID)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid household id", ErrBadInput)
		}
		poolExecutor := bobpgx.NewPool(r.DB)
		paymentAccountUUID = &accountID
		account, err := models.Accounts.Query(sm.Where(psql.And(
			models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Accounts.Columns.ID.EQ(psql.Arg(accountID)),
		))).One(ctx, poolExecutor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if account.Kind == string(model.AccountKindCard) {
			return nil, fmt.Errorf("%w: transfer source must be a non-card account", ErrBadInput)
		}
		if !account.PoolID.Valid {
			return nil, fmt.Errorf("%w: payment account is not assigned to a pool", ErrBadInput)
		}
		pool, err := models.Pools.Query(sm.Where(psql.And(
			models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Pools.Columns.ID.EQ(psql.Arg(account.PoolID.V)),
		))).One(ctx, poolExecutor)
		if err != nil {
			return nil, err
		}
		paymentPoolID = &account.PoolID.V
		paymentPoolKind = pool.Kind
		if pool.Kind == string(model.PoolKindPersonal) {
			fundingRule, err := models.PoolFundingRules.Query(sm.Where(psql.And(
				models.PoolFundingRules.Columns.HouseholdID.EQ(psql.Arg(householdID)),
				models.PoolFundingRules.Columns.PoolID.EQ(psql.Arg(pool.ID)),
				models.PoolFundingRules.Columns.Version.EQ(psql.Arg(pool.FundingRuleVersion)),
			))).One(ctx, poolExecutor)
			if err != nil {
				return nil, err
			}
			fromMemberID, err := gofrsuuid.FromString(input.FromMemberID)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid source member id", ErrBadInput)
			}
			if !fundingRule.OwnerMemberID.Valid || fundingRule.OwnerMemberID.V != fromMemberID {
				return nil, fmt.Errorf("%w: personal payment account must belong to the source member", ErrBadInput)
			}
		}
	}
	if input.DestinationAccountID != nil {
		destinationID, parseErr := gofrsuuid.FromString(*input.DestinationAccountID)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: invalid destination account id", ErrBadInput)
		}
		householdID, parseErr := gofrsuuid.FromString(input.HouseholdID)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: invalid household id", ErrBadInput)
		}
		executor := bobpgx.NewPool(r.DB)
		account, accountErr := models.Accounts.Query(sm.Where(psql.And(
			models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Accounts.Columns.ID.EQ(psql.Arg(destinationID)),
		))).One(ctx, executor)
		if errors.Is(accountErr, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if accountErr != nil {
			return nil, accountErr
		}
		if !account.PoolID.Valid || account.Kind == string(model.AccountKindCard) {
			return nil, fmt.Errorf("%w: transfer destination must be a non-card account assigned to a pool", ErrBadInput)
		}
		pool, poolErr := models.Pools.Query(sm.Where(psql.And(
			models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Pools.Columns.ID.EQ(psql.Arg(account.PoolID.V)),
		))).One(ctx, executor)
		if poolErr != nil {
			return nil, poolErr
		}
		if pool.Kind == string(model.PoolKindPersonal) {
			fundingRule, ruleErr := models.PoolFundingRules.Query(sm.Where(psql.And(
				models.PoolFundingRules.Columns.HouseholdID.EQ(psql.Arg(householdID)),
				models.PoolFundingRules.Columns.PoolID.EQ(psql.Arg(pool.ID)),
				models.PoolFundingRules.Columns.Version.EQ(psql.Arg(pool.FundingRuleVersion)),
			))).One(ctx, executor)
			if ruleErr != nil {
				return nil, ruleErr
			}
			toMemberID, parseErr := gofrsuuid.FromString(input.ToMemberID)
			if parseErr != nil || !fundingRule.OwnerMemberID.Valid || fundingRule.OwnerMemberID.V != toMemberID {
				return nil, fmt.Errorf("%w: personal destination account must belong to the receiving member", ErrBadInput)
			}
		}
		destinationAccountUUID = &destinationID
	}
	if paymentAccountUUID != nil && destinationAccountUUID != nil && *paymentAccountUUID == *destinationAccountUUID {
		return nil, fmt.Errorf("%w: transfer source and destination accounts must differ", ErrBadInput)
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	transactionExecutor := bobpgx.NewTx(tx, nil)
	householdID, err := gofrsuuid.FromString(input.HouseholdID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid household id", ErrBadInput)
	}
	key := input.IdempotencyKey
	operation := "recordTransfer"
	fingerprint, err := mutationRequestFingerprint(input)
	if err != nil {
		return nil, err
	}
	existing, err := models.IdempotencyKeys.Query(sm.Where(psql.And(
		models.IdempotencyKeys.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.IdempotencyKeys.Columns.Key.EQ(psql.Arg(key)),
		models.IdempotencyKeys.Columns.Operation.EQ(psql.Arg(operation)),
	))).One(ctx, transactionExecutor)
	if err == nil {
		if err := validateIdempotencyFingerprint(existing, fingerprint); err != nil {
			return nil, err
		}
		if err := tx.Rollback(ctx); err != nil {
			return nil, err
		}
		return loadLedgerEntry(ctx, r.DB, existing.ResourceID.V.String())
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	period := accountingPeriod(occurredAt)
	periodStatusValue := "OPEN"
	periodRevision := int64(0)
	if _, err := models.AccountingPeriods.Insert(&models.AccountingPeriodSetter{
		HouseholdID: &householdID, Period: &period, Status: &periodStatusValue, Revision: &periodRevision,
	}, im.OnConflict("household_id", "period").DoNothing()).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	periodRow, err := models.AccountingPeriods.Query(sm.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(period)),
	)), sm.ForUpdate()).One(ctx, transactionExecutor)
	if err != nil {
		return nil, err
	}
	if periodRow.Status == "CLOSED" {
		return nil, fmt.Errorf("%w: accounting period is closed", ErrConflict)
	}
	var loanID *gofrsuuid.UUID
	if input.Kind == model.TransferKindRepayment {
		loanUUID, parseErr := gofrsuuid.FromString(*input.LoanID)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: invalid loan id", ErrBadInput)
		}
		loan, err := models.Transfers.Query(sm.Where(psql.And(
			models.Transfers.Columns.ID.EQ(psql.Arg(loanUUID)),
			models.Transfers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Transfers.Columns.Kind.EQ(psql.Arg("LOAN")),
		)), sm.ForUpdate()).One(ctx, transactionExecutor)
		if err != nil {
			return nil, ErrNotFound
		}
		fromMemberID, fromErr := gofrsuuid.FromString(input.FromMemberID)
		toMemberID, toErr := gofrsuuid.FromString(input.ToMemberID)
		if fromErr != nil || toErr != nil {
			return nil, fmt.Errorf("%w: invalid transfer member id", ErrBadInput)
		}
		if fromMemberID != loan.ToMemberID || toMemberID != loan.FromMemberID {
			return nil, fmt.Errorf("%w: repayment direction does not match the loan", ErrBadInput)
		}
		repayments, err := models.Transfers.Query(sm.Where(psql.And(
			models.Transfers.Columns.LoanID.EQ(psql.Arg(loanUUID)),
			models.Transfers.Columns.Kind.EQ(psql.Arg("REPAYMENT")),
		))).All(ctx, transactionExecutor)
		if err != nil {
			return nil, err
		}
		var repaid int64
		for _, repayment := range repayments {
			var ok bool
			repaid, ok = checkedAdd(repaid, repayment.AmountYen)
			if !ok {
				return nil, fmt.Errorf("%w: repayment total is too large", ErrBadInput)
			}
		}
		if int64(input.AmountYen) > loan.AmountYen-repaid {
			return nil, fmt.Errorf("%w: repayment exceeds the outstanding loan", ErrBadInput)
		}
		loanID = &loanUUID
	}
	transferID, journalID, ledgerID := newID(), newID(), newID()
	transferUUID, _ := gofrsuuid.FromString(transferID)
	journalUUID, _ := gofrsuuid.FromString(journalID)
	ledgerUUID, _ := gofrsuuid.FromString(ledgerID)
	actorUUID, err := gofrsuuid.FromString(user.MemberID)
	if err != nil {
		return nil, err
	}
	sourceKind := "TRANSFER"
	if _, err := models.Journals.Insert(&models.JournalSetter{
		ID: &journalUUID, HouseholdID: &householdID, SourceKind: &sourceKind, SourceID: &transferUUID,
		IdempotencyKey: &key, OccurredAt: &occurredAt, CreatedBy: &actorUUID,
	}).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	amount := int64(input.AmountYen)
	fromMemberUUID, err := gofrsuuid.FromString(input.FromMemberID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid source member id", ErrBadInput)
	}
	toMemberUUID, err := gofrsuuid.FromString(input.ToMemberID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid destination member id", ErrBadInput)
	}
	lineOne, lineTwo := int32(1), int32(2)
	kindMemo := string(input.Kind)
	noAccount, noPool := sql.Null[gofrsuuid.UUID]{}, sql.Null[gofrsuuid.UUID]{}
	fromMember, toMember := sql.Null[gofrsuuid.UUID]{V: fromMemberUUID, Valid: true}, sql.Null[gofrsuuid.UUID]{V: toMemberUUID, Valid: true}
	zero := int64(0)
	if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
		JournalID: &journalUUID, LineNo: &lineOne, AccountID: &noAccount, PoolID: &noPool, MemberID: &toMember,
		DebitYen: &amount, CreditYen: &zero, Memo: &kindMemo,
	}).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
		JournalID: &journalUUID, LineNo: &lineTwo, AccountID: &noAccount, PoolID: &noPool, MemberID: &fromMember,
		DebitYen: &zero, CreditYen: &amount, Memo: &kindMemo,
	}).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	if input.PaymentAccountID != nil || input.DestinationAccountID != nil {
		if input.PaymentAccountID != nil && paymentPoolID == nil {
			return nil, fmt.Errorf("%w: invalid payment account", ErrBadInput)
		}
		line := int32(3)
		if input.PaymentAccountID != nil {
			poolValue := sql.Null[gofrsuuid.UUID]{V: *paymentPoolID, Valid: true}
			debitMemo := "Transfer settlement offset"
			if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
				JournalID: &journalUUID, LineNo: &line, AccountID: &noAccount, PoolID: &poolValue, MemberID: &noPool,
				DebitYen: &zero, CreditYen: &amount, Memo: new("Transfer settlement"),
			}).Exec(ctx, transactionExecutor); err != nil {
				return nil, err
			}
			line++
			if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
				JournalID: &journalUUID, LineNo: &line, AccountID: &noAccount, PoolID: &noPool, MemberID: &noPool,
				DebitYen: &amount, CreditYen: &zero, Memo: &debitMemo,
			}).Exec(ctx, transactionExecutor); err != nil {
				return nil, err
			}
			line++
		}
		if paymentAccountUUID != nil {
			accountValue := sql.Null[gofrsuuid.UUID]{V: *paymentAccountUUID, Valid: true}
			if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
				JournalID: &journalUUID, LineNo: &line, AccountID: &accountValue, PoolID: &noPool, MemberID: &noPool,
				DebitYen: &zero, CreditYen: &amount, Memo: new("Transfer source account"),
			}).Exec(ctx, transactionExecutor); err != nil {
				return nil, err
			}
			line++
			if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
				JournalID: &journalUUID, LineNo: &line, AccountID: &noAccount, PoolID: &noPool, MemberID: &noPool,
				DebitYen: &amount, CreditYen: &zero, Memo: new("Transfer source account offset"),
			}).Exec(ctx, transactionExecutor); err != nil {
				return nil, err
			}
			line++
		}
		if destinationAccountUUID != nil {
			accountValue := sql.Null[gofrsuuid.UUID]{V: *destinationAccountUUID, Valid: true}
			if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
				JournalID: &journalUUID, LineNo: &line, AccountID: &accountValue, PoolID: &noPool, MemberID: &noPool,
				DebitYen: &amount, CreditYen: &zero, Memo: new("Transfer destination account"),
			}).Exec(ctx, transactionExecutor); err != nil {
				return nil, err
			}
			line++
			if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
				JournalID: &journalUUID, LineNo: &line, AccountID: &noAccount, PoolID: &noPool, MemberID: &noPool,
				DebitYen: &zero, CreditYen: &amount, Memo: new("Transfer destination account offset"),
			}).Exec(ctx, transactionExecutor); err != nil {
				return nil, err
			}
		}
	}
	description := strings.TrimSpace(input.Description)
	if description == "" {
		description = string(input.Kind)
	}
	transferLedgerKind := "TRANSFER"
	if _, err := models.LedgerEntries.Insert(&models.LedgerEntrySetter{
		ID: &ledgerUUID, HouseholdID: &householdID, JournalID: &journalUUID, Kind: &transferLedgerKind,
		OccurredAt: &occurredAt, Description: &description, AmountYen: &amount,
		FromMemberID: &fromMember, ToMemberID: &toMember,
	}).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	transferKind := string(input.Kind)
	loanValue := sql.Null[gofrsuuid.UUID]{}
	if loanID != nil {
		loanValue = sql.Null[gofrsuuid.UUID]{V: *loanID, Valid: true}
	}
	paymentAccountValue := sql.Null[gofrsuuid.UUID]{}
	if input.PaymentAccountID != nil {
		paymentAccountValue = sql.Null[gofrsuuid.UUID]{V: *paymentAccountUUID, Valid: true}
	}
	if _, err := models.Transfers.Insert(&models.TransferSetter{
		ID: &transferUUID, HouseholdID: &householdID, LedgerEntryID: &ledgerUUID,
		FromMemberID: &fromMemberUUID, ToMemberID: &toMemberUUID, Kind: &transferKind,
		AmountYen: &amount, Description: &description, LoanID: &loanValue, PaymentAccountID: &paymentAccountValue,
	}).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	if input.Kind == model.TransferKindAdvance || input.Kind == model.TransferKindLoan || input.Kind == model.TransferKindRepayment {
		adjustments := []struct {
			memberID string
			amount   int64
		}{{memberID: input.FromMemberID, amount: -amount}, {memberID: input.ToMemberID, amount: amount}}
		if input.Kind == model.TransferKindRepayment && paymentPoolKind == string(model.PoolKindShared) {
			adjustments = []struct {
				memberID string
				amount   int64
			}{{memberID: input.ToMemberID, amount: amount}}
		}
		for _, adjustment := range adjustments {
			adjustmentID, _ := gofrsuuid.FromString(newID())
			memberUUID, parseErr := gofrsuuid.FromString(adjustment.memberID)
			if parseErr != nil {
				return nil, parseErr
			}
			if _, err := models.SettlementAdjustments.Insert(&models.SettlementAdjustmentSetter{
				ID: &adjustmentID, HouseholdID: &householdID, Period: &period, MemberID: &memberUUID,
				SourceKind: &transferKind, SourceID: &transferUUID, AmountYen: &adjustment.amount,
			}).Exec(ctx, transactionExecutor); err != nil {
				return nil, err
			}
		}
	}
	ledgerResource := sql.Null[gofrsuuid.UUID]{V: ledgerUUID, Valid: true}
	requestFingerprint := sql.Null[[]byte]{V: fingerprint[:], Valid: true}
	if _, err := models.IdempotencyKeys.Insert(&models.IdempotencyKeySetter{
		HouseholdID: &householdID, Key: &key, Operation: &operation, ResourceID: &ledgerResource,
		RequestFingerprint: &requestFingerprint,
	}).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "transfer.recorded", ledgerID, 1, []string{"amountYen", "kind"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadLedgerEntry(ctx, r.DB, ledgerID)
}
