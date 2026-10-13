package funding

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

var (
	ErrBadInput = errors.New("invalid input")
	ErrConflict = errors.New("resource conflict")
	ErrNotFound = errors.New("resource not found")
)

const maxMoney int64 = 1_000_000_000_000

type Command struct {
	ActorID              string
	HouseholdID          string
	MemberID             string
	PoolID               string
	AmountYen            int64
	OccurredAt           time.Time
	IdempotencyKey       string
	SourceAccountID      *string
	DestinationAccountID *string
	RequestFingerprint   [sha256.Size]byte
}

type Result struct {
	LedgerEntryID string
}

type Workflow struct {
	db *pgxpool.Pool
}

func New(db *pgxpool.Pool) *Workflow {
	return &Workflow{db: db}
}

func (w *Workflow) Do(ctx context.Context, command Command) (Result, error) {
	if command.AmountYen <= 0 || command.AmountYen > maxMoney || command.IdempotencyKey == "" {
		return Result{}, fmt.Errorf("%w: invalid funding amount or idempotency key", ErrBadInput)
	}
	householdID, err := gofrsuuid.FromString(command.HouseholdID)
	if err != nil {
		return Result{}, fmt.Errorf("%w: invalid household id", ErrBadInput)
	}
	actorID, err := gofrsuuid.FromString(command.ActorID)
	if err != nil {
		return Result{}, fmt.Errorf("%w: invalid actor id", ErrBadInput)
	}
	memberID, err := gofrsuuid.FromString(command.MemberID)
	if err != nil {
		return Result{}, fmt.Errorf("%w: invalid member id", ErrBadInput)
	}
	poolID, err := gofrsuuid.FromString(command.PoolID)
	if err != nil {
		return Result{}, fmt.Errorf("%w: invalid pool id", ErrBadInput)
	}
	if command.OccurredAt.IsZero() {
		return Result{}, fmt.Errorf("%w: occurred at is required", ErrBadInput)
	}

	tx, err := w.db.Begin(ctx)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	if err := requireHouseholdMember(ctx, executor, householdID, actorID); err != nil {
		return Result{}, err
	}
	if err := requireHouseholdMember(ctx, executor, householdID, memberID); err != nil {
		return Result{}, err
	}
	_, err = models.Pools.Query(sm.Where(psql.And(
		models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Pools.Columns.ID.EQ(psql.Arg(poolID)),
	))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return Result{}, ErrNotFound
	}
	if err != nil {
		return Result{}, err
	}

	operation := "recordFunding"
	existing, err := models.IdempotencyKeys.Query(sm.Where(psql.And(
		models.IdempotencyKeys.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.IdempotencyKeys.Columns.Key.EQ(psql.Arg(command.IdempotencyKey)),
		models.IdempotencyKeys.Columns.Operation.EQ(psql.Arg(operation)),
	))).One(ctx, executor)
	if err == nil && existing.ResourceID.Valid {
		if !existing.RequestFingerprint.Valid || !slices.Equal(existing.RequestFingerprint.V, command.RequestFingerprint[:]) {
			return Result{}, fmt.Errorf("%w: idempotency key was used with a different request", ErrConflict)
		}
		if err := tx.Rollback(ctx); err != nil {
			return Result{}, err
		}
		return Result{LedgerEntryID: existing.ResourceID.V.String()}, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Result{}, err
	}

	period := accounting.PeriodFor(command.OccurredAt)
	if _, err := models.AccountingPeriods.Insert(&models.AccountingPeriodSetter{
		HouseholdID: &householdID, Period: &period, Status: new("OPEN"), Revision: new(int64(0)),
	}, im.OnConflict("household_id", "period").DoNothing()).Exec(ctx, executor); err != nil {
		return Result{}, err
	}
	periodRow, err := models.AccountingPeriods.Query(sm.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(period)),
	)), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return Result{}, err
	}
	if periodRow.Status == "CLOSED" {
		return Result{}, fmt.Errorf("%w: accounting period is closed", ErrConflict)
	}

	var sourceAccountID, destinationAccountID *gofrsuuid.UUID
	if command.SourceAccountID != nil {
		id, parseErr := gofrsuuid.FromString(*command.SourceAccountID)
		if parseErr != nil {
			return Result{}, fmt.Errorf("%w: invalid source account id", ErrBadInput)
		}
		account, accountErr := models.Accounts.Query(sm.Where(psql.And(
			models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Accounts.Columns.ID.EQ(psql.Arg(id)),
		))).One(ctx, executor)
		if errors.Is(accountErr, sql.ErrNoRows) {
			return Result{}, ErrNotFound
		}
		if accountErr != nil {
			return Result{}, accountErr
		}
		if account.Kind == "CARD" {
			return Result{}, fmt.Errorf("%w: funding source must be a non-card account", ErrBadInput)
		}
		if account.PoolID.Valid {
			pool, poolErr := models.Pools.Query(sm.Where(psql.And(
				models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdID)),
				models.Pools.Columns.ID.EQ(psql.Arg(account.PoolID.V)),
			))).One(ctx, executor)
			if poolErr != nil {
				return Result{}, poolErr
			}
			if pool.Kind == "PERSONAL" {
				fundingRule, ruleErr := models.PoolFundingRules.Query(sm.Where(psql.And(
					models.PoolFundingRules.Columns.HouseholdID.EQ(psql.Arg(householdID)),
					models.PoolFundingRules.Columns.PoolID.EQ(psql.Arg(pool.ID)),
					models.PoolFundingRules.Columns.Version.EQ(psql.Arg(pool.FundingRuleVersion)),
				))).One(ctx, executor)
				if ruleErr != nil {
					return Result{}, ruleErr
				}
				if !fundingRule.OwnerMemberID.Valid || fundingRule.OwnerMemberID.V != memberID {
					return Result{}, fmt.Errorf("%w: personal source account must belong to the funding member", ErrBadInput)
				}
			}
		}
		sourceAccountID = &id
	}
	if command.DestinationAccountID != nil {
		id, parseErr := gofrsuuid.FromString(*command.DestinationAccountID)
		if parseErr != nil {
			return Result{}, fmt.Errorf("%w: invalid destination account id", ErrBadInput)
		}
		account, accountErr := models.Accounts.Query(sm.Where(psql.And(
			models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Accounts.Columns.ID.EQ(psql.Arg(id)),
		))).One(ctx, executor)
		if errors.Is(accountErr, sql.ErrNoRows) {
			return Result{}, ErrNotFound
		}
		if accountErr != nil {
			return Result{}, accountErr
		}
		if !account.PoolID.Valid || account.PoolID.V != poolID || account.Kind == "CARD" {
			return Result{}, fmt.Errorf("%w: funding destination must be a non-card account in the funded pool", ErrBadInput)
		}
		destinationAccountID = &id
	}
	if sourceAccountID != nil && destinationAccountID != nil && *sourceAccountID == *destinationAccountID {
		return Result{}, fmt.Errorf("%w: funding source and destination accounts must differ", ErrBadInput)
	}

	schedules, err := models.FundingSchedules.Query(sm.Where(psql.And(
		models.FundingSchedules.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.FundingSchedules.Columns.MemberID.EQ(psql.Arg(memberID)),
		models.FundingSchedules.Columns.PoolID.EQ(psql.Arg(poolID)),
		models.FundingSchedules.Columns.Active.EQ(psql.Arg(true)),
	)), sm.OrderBy(models.FundingSchedules.Columns.CreatedAt).Desc(), sm.Limit(1)).All(ctx, executor)
	if err != nil {
		return Result{}, err
	}
	var scheduleID sql.Null[gofrsuuid.UUID]
	if len(schedules) > 0 {
		scheduleID = sql.Null[gofrsuuid.UUID]{V: schedules[0].ID, Valid: true}
	}
	amount := command.AmountYen
	obligationID := gofrsuuid.Must(gofrsuuid.NewV7())
	if _, err := models.FundingObligations.Insert(&models.FundingObligationSetter{
		ID: &obligationID, HouseholdID: &householdID, ScheduleID: &scheduleID, MemberID: &memberID,
		PoolID: &poolID, Period: &period, AmountYen: new(int64(0)), PaidYen: &amount,
	}, im.OnConflict("household_id", "member_id", "pool_id", "period").DoUpdate(
		im.SetCol("paid_yen").To(psql.Quote("funding_obligations", "paid_yen").Plus(im.Excluded("paid_yen"))),
	)).Exec(ctx, executor); err != nil {
		return Result{}, err
	}

	journalID, ledgerID := gofrsuuid.Must(gofrsuuid.NewV7()), gofrsuuid.Must(gofrsuuid.NewV7())
	sourceKind := "FUNDING"
	occurredAt := command.OccurredAt.UTC()
	if _, err := models.Journals.Insert(&models.JournalSetter{
		ID: &journalID, HouseholdID: &householdID, SourceKind: &sourceKind, SourceID: &ledgerID,
		IdempotencyKey: &command.IdempotencyKey, OccurredAt: &occurredAt, CreatedBy: &memberID,
	}).Exec(ctx, executor); err != nil {
		return Result{}, err
	}
	poolReference := sql.Null[gofrsuuid.UUID]{V: poolID, Valid: true}
	if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
		JournalID: &journalID, LineNo: new(int32(1)), PoolID: &poolReference, DebitYen: &amount, Memo: new("Pool funding"),
	}).Exec(ctx, executor); err != nil {
		return Result{}, err
	}
	if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
		JournalID: &journalID, LineNo: new(int32(2)), PoolID: &poolReference, DebitYen: new(int64(0)), CreditYen: &amount, Memo: new("Member contribution"),
	}).Exec(ctx, executor); err != nil {
		return Result{}, err
	}
	if sourceAccountID != nil || destinationAccountID != nil {
		empty := sql.Null[gofrsuuid.UUID]{}
		lineThree, lineFour := int32(3), int32(4)
		sourceDebit, sourceCredit := int64(0), amount
		destinationDebit, destinationCredit := amount, int64(0)
		sourceAccount, destinationAccount := empty, empty
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
			JournalID: &journalID, LineNo: &lineThree, AccountID: &destinationAccount, PoolID: &empty, MemberID: &empty,
			DebitYen: &destinationDebit, CreditYen: &destinationCredit, Memo: new("Funding destination"),
		}).Exec(ctx, executor); err != nil {
			return Result{}, err
		}
		if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
			JournalID: &journalID, LineNo: &lineFour, AccountID: &sourceAccount, PoolID: &empty, MemberID: &empty,
			DebitYen: &sourceDebit, CreditYen: &sourceCredit, Memo: new("Funding source"),
		}).Exec(ctx, executor); err != nil {
			return Result{}, err
		}
	}
	memberReference := sql.Null[gofrsuuid.UUID]{V: memberID, Valid: true}
	if _, err := models.LedgerEntries.Insert(&models.LedgerEntrySetter{
		ID: &ledgerID, HouseholdID: &householdID, JournalID: &journalID, Kind: new("FUNDING"),
		OccurredAt: &occurredAt, Description: new("Pool funding"), AmountYen: &amount,
		FromMemberID: &memberReference, PoolID: &poolReference,
	}).Exec(ctx, executor); err != nil {
		return Result{}, err
	}
	resourceID := sql.Null[gofrsuuid.UUID]{V: ledgerID, Valid: true}
	requestFingerprint := sql.Null[[]byte]{V: command.RequestFingerprint[:], Valid: true}
	if _, err := models.IdempotencyKeys.Insert(&models.IdempotencyKeySetter{
		HouseholdID: &householdID, Key: &command.IdempotencyKey, Operation: &operation, ResourceID: &resourceID,
		RequestFingerprint: &requestFingerprint,
	}).Exec(ctx, executor); err != nil {
		return Result{}, err
	}
	if err := writeEvent(ctx, tx, householdID, ledgerID, "funding.recorded", 1, []string{"amountYen"}, actorID); err != nil {
		return Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	return Result{LedgerEntryID: ledgerID.String()}, nil
}

func requireHouseholdMember(ctx context.Context, executor bob.Executor, householdID, memberID gofrsuuid.UUID) error {
	member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberID)),
	))).Exists(ctx, executor)
	if err != nil {
		return err
	}
	if !member {
		return ErrNotFound
	}
	return nil
}

func writeEvent(ctx context.Context, tx pgx.Tx, householdID, entityID gofrsuuid.UUID, eventType string, revision int64, fields []string, actorID gofrsuuid.UUID) error {
	return accounting.RecordEvent(ctx, bobpgx.NewTx(tx, nil), householdID, entityID, eventType, revision, fields, actorID)
}
