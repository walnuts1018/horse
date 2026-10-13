package accountmovement

import (
	"context"
	"crypto/sha256"
	"database/sql"
	stdjson "encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const maxMoney int64 = 1_000_000_000_000

const (
	movementOperation = "recordAccountMovement"
	incomeOperation   = "recordExternalIncome"
)

type Service struct {
	db *pgxpool.Pool
}

const cardAccountKind = "CARD"

func New(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

type MovementCommand struct {
	ActorID            string
	HouseholdID        string
	Kind               string
	OccurredAt         time.Time
	FromAccountID      string
	ToAccountID        string
	AmountYen          int64
	Description        string
	IdempotencyKey     string
	RequestFingerprint [sha256.Size]byte
}

type IncomeCommand struct {
	ActorID            string
	HouseholdID        string
	AccountID          string
	OccurredAt         time.Time
	AmountYen          int64
	Description        string
	IdempotencyKey     string
	RequestFingerprint [sha256.Size]byte
}

type MovementResult struct {
	ID, HouseholdID, Kind, OccurredAt, FromAccountID, ToAccountID, Description, CreatedAt string
	AmountYen                                                                             int64
}

type IncomeResult struct {
	ID, HouseholdID, AccountID, OccurredAt, Description, CreatedAt string
	AmountYen                                                      int64
}

func (s *Service) RecordMovement(ctx context.Context, command MovementCommand) (MovementResult, error) {
	if command.AmountYen <= 0 || command.AmountYen > maxMoney || strings.TrimSpace(command.IdempotencyKey) == "" || len(command.IdempotencyKey) > 200 || command.OccurredAt.IsZero() {
		return MovementResult{}, apperror.Wrap(apperror.KindBadInput, errors.New("invalid account movement"))
	}
	if command.Kind != "TRANSFER" && command.Kind != "CARD_BILL_PAYMENT" {
		return MovementResult{}, apperror.Wrap(apperror.KindBadInput, errors.New("invalid account movement kind"))
	}
	if len(command.Description) > 240 || command.FromAccountID == command.ToAccountID {
		return MovementResult{}, apperror.Wrap(apperror.KindBadInput, errors.New("invalid account movement"))
	}
	householdID, err := parseID(command.HouseholdID, "household")
	if err != nil {
		return MovementResult{}, err
	}
	actorID, err := parseID(command.ActorID, "actor")
	if err != nil {
		return MovementResult{}, err
	}
	fromID, err := parseID(command.FromAccountID, "source account")
	if err != nil {
		return MovementResult{}, err
	}
	toID, err := parseID(command.ToAccountID, "destination account")
	if err != nil {
		return MovementResult{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return MovementResult{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return MovementResult{}, apperror.ErrNotFound
	} else if err != nil {
		return MovementResult{}, err
	}
	existing, err := findIdempotent(ctx, executor, householdID, command.IdempotencyKey, movementOperation)
	if err == nil {
		if err := validateFingerprint(existing, command.RequestFingerprint); err != nil {
			return MovementResult{}, err
		}
		if !existing.ResourceID.Valid {
			return MovementResult{}, apperror.ErrNotFound
		}
		if err := tx.Rollback(ctx); err != nil {
			return MovementResult{}, err
		}
		row, err := models.AccountMovements.Query(sm.Where(models.AccountMovements.Columns.ID.EQ(psql.Arg(existing.ResourceID.V)))).One(ctx, bobpgx.NewPool(s.db))
		if errors.Is(err, sql.ErrNoRows) {
			return MovementResult{}, apperror.ErrNotFound
		}
		if err != nil {
			return MovementResult{}, err
		}
		return movementResult(row), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return MovementResult{}, err
	}
	if err := lockPeriod(ctx, tx, householdID, command.OccurredAt); err != nil {
		return MovementResult{}, err
	}
	fromAccount, err := loadAccount(ctx, executor, householdID, fromID)
	if err != nil {
		return MovementResult{}, err
	}
	toAccount, err := loadAccount(ctx, executor, householdID, toID)
	if err != nil {
		return MovementResult{}, err
	}
	switch command.Kind {
	case "TRANSFER":
		if fromAccount.Kind == cardAccountKind || toAccount.Kind == cardAccountKind {
			return MovementResult{}, apperror.Wrap(apperror.KindBadInput, errors.New("card accounts require a bill payment movement"))
		}
	case "CARD_BILL_PAYMENT":
		if fromAccount.Kind != "BANK" || toAccount.Kind != cardAccountKind {
			return MovementResult{}, apperror.Wrap(apperror.KindBadInput, errors.New("card bill payments must move from a bank account to a card account"))
		}
	}
	movementID, journalID := newUUID(), newUUID()
	journalSource := "ACCOUNT_MOVEMENT"
	kind := command.Kind
	occurredAt := command.OccurredAt.UTC()
	if _, err := models.Journals.Insert(&models.JournalSetter{
		ID: &journalID, HouseholdID: &householdID, SourceKind: &journalSource, SourceID: &movementID,
		IdempotencyKey: &command.IdempotencyKey, OccurredAt: &occurredAt, CreatedBy: &actorID,
	}).Exec(ctx, executor); err != nil {
		return MovementResult{}, err
	}
	amount, zero := command.AmountYen, int64(0)
	noPool, noMember := sql.Null[gofrsuuid.UUID]{}, sql.Null[gofrsuuid.UUID]{}
	destination := sql.Null[gofrsuuid.UUID]{V: toID, Valid: true}
	source := sql.Null[gofrsuuid.UUID]{V: fromID, Valid: true}
	debitMemo, creditMemo := "Account movement destination", "Account movement source"
	lineOne, lineTwo := int32(1), int32(2)
	if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{JournalID: &journalID, LineNo: &lineOne, AccountID: &destination, PoolID: &noPool, MemberID: &noMember, DebitYen: &amount, CreditYen: &zero, Memo: &debitMemo}).Exec(ctx, executor); err != nil {
		return MovementResult{}, err
	}
	if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{JournalID: &journalID, LineNo: &lineTwo, AccountID: &source, PoolID: &noPool, MemberID: &noMember, DebitYen: &zero, CreditYen: &amount, Memo: &creditMemo}).Exec(ctx, executor); err != nil {
		return MovementResult{}, err
	}
	row, err := models.AccountMovements.Insert(&models.AccountMovementSetter{
		ID: &movementID, HouseholdID: &householdID, JournalID: &journalID, Kind: &kind, OccurredAt: &occurredAt,
		FromAccountID: &fromID, ToAccountID: &toID, AmountYen: &amount, Description: &command.Description,
	}).One(ctx, executor)
	if err != nil {
		return MovementResult{}, err
	}
	if err := saveIdempotency(ctx, executor, householdID, movementID, command.IdempotencyKey, movementOperation, command.RequestFingerprint); err != nil {
		return MovementResult{}, err
	}
	if err := writeEvent(ctx, tx, householdID, "account.movement.recorded", movementID, 1, []string{"kind", "fromAccountId", "toAccountId", "amountYen", "occurredAt"}, actorID); err != nil {
		return MovementResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MovementResult{}, err
	}
	return movementResult(row), nil
}

func (s *Service) RecordIncome(ctx context.Context, command IncomeCommand) (IncomeResult, error) {
	if command.AmountYen <= 0 || command.AmountYen > maxMoney || strings.TrimSpace(command.IdempotencyKey) == "" || len(command.IdempotencyKey) > 200 || command.OccurredAt.IsZero() || len(command.Description) > 240 {
		return IncomeResult{}, apperror.Wrap(apperror.KindBadInput, errors.New("invalid external income"))
	}
	householdID, err := parseID(command.HouseholdID, "household")
	if err != nil {
		return IncomeResult{}, err
	}
	actorID, err := parseID(command.ActorID, "actor")
	if err != nil {
		return IncomeResult{}, err
	}
	accountID, err := parseID(command.AccountID, "account")
	if err != nil {
		return IncomeResult{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return IncomeResult{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	existing, err := findIdempotent(ctx, executor, householdID, command.IdempotencyKey, incomeOperation)
	if err == nil {
		if err := validateFingerprint(existing, command.RequestFingerprint); err != nil {
			return IncomeResult{}, err
		}
		if !existing.ResourceID.Valid {
			return IncomeResult{}, apperror.ErrNotFound
		}
		if err := tx.Rollback(ctx); err != nil {
			return IncomeResult{}, err
		}
		row, err := models.AccountIncomes.Query(sm.Where(models.AccountIncomes.Columns.ID.EQ(psql.Arg(existing.ResourceID.V)))).One(ctx, bobpgx.NewPool(s.db))
		if errors.Is(err, sql.ErrNoRows) {
			return IncomeResult{}, apperror.ErrNotFound
		}
		if err != nil {
			return IncomeResult{}, err
		}
		return incomeResult(row), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return IncomeResult{}, err
	}
	account, err := loadAccount(ctx, executor, householdID, accountID)
	if err != nil {
		return IncomeResult{}, err
	}
	if account.Kind == cardAccountKind {
		return IncomeResult{}, apperror.Wrap(apperror.KindBadInput, errors.New("external income requires a bank, cash, or other account"))
	}
	if err := lockPeriod(ctx, tx, householdID, command.OccurredAt); err != nil {
		return IncomeResult{}, err
	}
	incomeID, journalID := newUUID(), newUUID()
	journalSource := "ACCOUNT_INCOME"
	occurredAt := command.OccurredAt.UTC()
	if _, err := models.Journals.Insert(&models.JournalSetter{
		ID: &journalID, HouseholdID: &householdID, SourceKind: &journalSource, SourceID: &incomeID,
		IdempotencyKey: &command.IdempotencyKey, OccurredAt: &occurredAt, CreatedBy: &actorID,
	}).Exec(ctx, executor); err != nil {
		return IncomeResult{}, err
	}
	amount, zero := command.AmountYen, int64(0)
	accountReference := sql.Null[gofrsuuid.UUID]{V: accountID, Valid: true}
	noAccount, noPool, noMember := sql.Null[gofrsuuid.UUID]{}, sql.Null[gofrsuuid.UUID]{}, sql.Null[gofrsuuid.UUID]{}
	lineOne, lineTwo := int32(1), int32(2)
	debitMemo, creditMemo := "External income", "External income offset"
	if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{JournalID: &journalID, LineNo: &lineOne, AccountID: &accountReference, PoolID: &noPool, MemberID: &noMember, DebitYen: &amount, CreditYen: &zero, Memo: &debitMemo}).Exec(ctx, executor); err != nil {
		return IncomeResult{}, err
	}
	if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{JournalID: &journalID, LineNo: &lineTwo, AccountID: &noAccount, PoolID: &noPool, MemberID: &noMember, DebitYen: &zero, CreditYen: &amount, Memo: &creditMemo}).Exec(ctx, executor); err != nil {
		return IncomeResult{}, err
	}
	row, err := models.AccountIncomes.Insert(&models.AccountIncomeSetter{
		ID: &incomeID, HouseholdID: &householdID, JournalID: &journalID, AccountID: &accountID,
		OccurredAt: &occurredAt, AmountYen: &amount, Description: &command.Description,
	}).One(ctx, executor)
	if err != nil {
		return IncomeResult{}, err
	}
	if err := saveIdempotency(ctx, executor, householdID, incomeID, command.IdempotencyKey, incomeOperation, command.RequestFingerprint); err != nil {
		return IncomeResult{}, err
	}
	if err := writeEvent(ctx, tx, householdID, "account.income.recorded", incomeID, 1, []string{"amountYen", "accountId", "occurredAt"}, actorID); err != nil {
		return IncomeResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return IncomeResult{}, err
	}
	return incomeResult(row), nil
}

func parseID(value, name string) (gofrsuuid.UUID, error) {
	id, err := gofrsuuid.FromString(value)
	if err != nil {
		return gofrsuuid.Nil, apperror.Wrap(apperror.KindNotFound, fmt.Errorf("invalid %s id", name))
	}
	return id, nil
}

func newUUID() gofrsuuid.UUID { return gofrsuuid.Must(gofrsuuid.NewV7()) }

func findIdempotent(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, key, operation string) (*models.IdempotencyKey, error) {
	return models.IdempotencyKeys.Query(sm.Where(psql.And(
		models.IdempotencyKeys.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.IdempotencyKeys.Columns.Key.EQ(psql.Arg(key)),
		models.IdempotencyKeys.Columns.Operation.EQ(psql.Arg(operation)),
	))).One(ctx, executor)
}

func validateFingerprint(existing *models.IdempotencyKey, fingerprint [sha256.Size]byte) error {
	if !existing.RequestFingerprint.Valid || !slices.Equal(existing.RequestFingerprint.V, fingerprint[:]) {
		return apperror.Wrap(apperror.KindConflict, errors.New("idempotency key was used with a different request"))
	}
	return nil
}

func saveIdempotency(ctx context.Context, executor bob.Executor, householdID, resourceID gofrsuuid.UUID, key, operation string, fingerprint [sha256.Size]byte) error {
	resource := sql.Null[gofrsuuid.UUID]{V: resourceID, Valid: true}
	requestFingerprint := sql.Null[[]byte]{V: fingerprint[:], Valid: true}
	_, err := models.IdempotencyKeys.Insert(&models.IdempotencyKeySetter{
		HouseholdID: &householdID, Key: &key, Operation: &operation, ResourceID: &resource,
		RequestFingerprint: &requestFingerprint,
	}).Exec(ctx, executor)
	return err
}

func loadAccount(ctx context.Context, executor bob.Executor, householdID, accountID gofrsuuid.UUID) (*models.Account, error) {
	account, err := models.Accounts.Query(sm.Where(psql.And(
		models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Accounts.Columns.ID.EQ(psql.Arg(accountID)),
	))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperror.ErrNotFound
	}
	return account, err
}

func lockPeriod(ctx context.Context, tx pgx.Tx, householdID gofrsuuid.UUID, occurredAt time.Time) error {
	period := occurredAt.In(time.FixedZone("JST", 9*60*60)).Format("2006-01")
	status, revision := "OPEN", int64(0)
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.AccountingPeriods.Insert(&models.AccountingPeriodSetter{
		HouseholdID: &householdID, Period: &period, Status: &status, Revision: &revision,
	}, im.OnConflict("household_id", "period").DoNothing()).Exec(ctx, executor); err != nil {
		return err
	}
	row, err := models.AccountingPeriods.Query(sm.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(period)),
	)), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return err
	}
	if row.Status == "CLOSED" {
		return apperror.Wrap(apperror.KindConflict, errors.New("accounting period is closed"))
	}
	return nil
}

func writeEvent(ctx context.Context, tx pgx.Tx, householdID gofrsuuid.UUID, eventType string, entityID gofrsuuid.UUID, revision int64, fields []string, actorID gofrsuuid.UUID) error {
	encoded, err := jsonv2.Marshal(fields)
	if err != nil {
		return err
	}
	changedFields := types.JSON[stdjson.RawMessage]{Val: encoded}
	actor := sql.Null[gofrsuuid.UUID]{V: actorID, Valid: true}
	_, err = models.HouseholdEvents.Insert(&models.HouseholdEventSetter{
		HouseholdID: &householdID, Type: &eventType, EntityID: &entityID, Revision: &revision,
		ChangedFields: &changedFields, ActorID: &actor,
	}).Exec(ctx, bobpgx.NewTx(tx, nil))
	return err
}

func movementResult(row *models.AccountMovement) MovementResult {
	return MovementResult{ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), Kind: row.Kind,
		OccurredAt: row.OccurredAt.UTC().Format(time.RFC3339Nano), FromAccountID: row.FromAccountID.String(), ToAccountID: row.ToAccountID.String(),
		AmountYen: row.AmountYen, Description: row.Description, CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339Nano)}
}

func incomeResult(row *models.AccountIncome) IncomeResult {
	return IncomeResult{ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), AccountID: row.AccountID.String(),
		OccurredAt: row.OccurredAt.UTC().Format(time.RFC3339Nano), AmountYen: row.AmountYen, Description: row.Description,
		CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339Nano)}
}
