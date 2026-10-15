package settlement

import (
	"context"
	"database/sql"
	stdjson "encoding/json"
	"encoding/json/v2"
	"errors"
	"fmt"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/application/txcleanup"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const closedStatus = "CLOSED"

var (
	ErrBadInput                       = apperror.ErrBadInput
	ErrConflict                       = apperror.ErrConflict
	ErrNotFound                       = apperror.ErrNotFound
	ErrUnattributedPurchasePayment    = errors.New("posted purchase payment has no funding pool")
	ErrMissingPurchaseFundingSnapshot = errors.New("posted purchase has no funding snapshot")
)

type Settlement struct {
	HouseholdID string
	Period      string
	Status      string
	Revision    int64
	CreatedAt   *time.Time
	Lines       []domain.SettlementAmount
	Evidence    []Evidence
}

type CloseCommand struct {
	ActorID          string
	HouseholdID      string
	Period           string
	ExpectedRevision int64
}

type Workflow struct {
	db *pgxpool.Pool
}

func New(db *pgxpool.Pool) *Workflow {
	return &Workflow{db: db}
}

func (w *Workflow) Preview(ctx context.Context, householdID, period string) ([]domain.SettlementAmount, error) {
	return calculateSettlementPreview(ctx, bobpgx.NewPool(w.db), householdID, period)
}

func (w *Workflow) Get(ctx context.Context, householdID, period string) (Settlement, error) {
	results, err := w.GetMany(ctx, []GetRequest{{HouseholdID: householdID, Period: period}})
	if err != nil {
		return Settlement{}, err
	}
	return results[0], nil
}

func (w *Workflow) Close(ctx context.Context, command CloseCommand) (Settlement, error) {
	periodStart, err := parsePeriod(command.Period)
	if err != nil {
		return Settlement{}, fmt.Errorf("%w: period must use YYYY-MM", ErrBadInput)
	}
	householdUUID, err := gofrsuuid.FromString(command.HouseholdID)
	if err != nil {
		return Settlement{}, fmt.Errorf("%w: invalid household id", ErrBadInput)
	}
	actorUUID, err := gofrsuuid.FromString(command.ActorID)
	if err != nil {
		return Settlement{}, fmt.Errorf("%w: invalid actor id", ErrBadInput)
	}
	periodEnd := periodStart.AddDate(0, 1, 0)
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return Settlement{}, err
	}
	defer txcleanup.Rollback(ctx, tx)
	executor := bobpgx.NewTx(tx, nil)
	periodRow, err := lockAccountingPeriod(ctx, executor, householdUUID, command.Period, command.ExpectedRevision)
	if err != nil {
		return Settlement{}, err
	}
	if err := ensureNoDraftPurchases(ctx, executor, householdUUID, periodStart, periodEnd); err != nil {
		return Settlement{}, err
	}
	if err := materializeFundingObligations(ctx, executor, householdUUID, command.Period, periodStart, periodEnd); err != nil {
		return Settlement{}, err
	}
	rules, lines, err := calculateCloseSettlement(ctx, executor, command.HouseholdID, command.Period)
	if err != nil {
		return Settlement{}, err
	}
	settlementID, err := gofrsuuid.NewV7()
	if err != nil {
		return Settlement{}, err
	}
	newRevision := periodRow.Revision + 1
	if err := insertSettlementSnapshot(ctx, executor, householdUUID, command.Period, actorUUID, settlementID, newRevision, rules, lines); err != nil {
		return Settlement{}, err
	}
	updatedAt := time.Now()
	if err := closePostedPurchases(ctx, executor, householdUUID, periodStart, periodEnd, updatedAt); err != nil {
		return Settlement{}, err
	}
	if err := recordPeriodClosure(ctx, executor, householdUUID, command.Period, settlementID, newRevision, actorUUID, updatedAt); err != nil {
		return Settlement{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Settlement{}, err
	}
	return w.Get(ctx, command.HouseholdID, command.Period)
}

func lockAccountingPeriod(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, period string, expectedRevision int64) (*models.AccountingPeriod, error) {
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	openStatus, zeroRevision := "OPEN", int64(0)
	if _, err := models.AccountingPeriods.Insert(&models.AccountingPeriodSetter{
		HouseholdID: &householdID, Period: &period, Status: &openStatus, Revision: &zeroRevision,
	}, im.OnConflict("household_id", "period").DoNothing()).Exec(ctx, executor); err != nil {
		return nil, err
	}
	periodRow, err := models.AccountingPeriods.Query(sm.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(period)),
	)), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return nil, err
	}
	if periodRow.Status == closedStatus || periodRow.Revision != expectedRevision {
		return nil, ErrConflict
	}
	return periodRow, nil
}

func ensureNoDraftPurchases(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, start, end time.Time) error {
	drafts, err := models.Purchases.Query(sm.Where(psql.And(
		models.Purchases.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Purchases.Columns.Status.EQ(psql.Arg("DRAFT")),
		models.Purchases.Columns.OccurredAt.GTE(psql.Arg(start)),
		models.Purchases.Columns.OccurredAt.LT(psql.Arg(end)),
	))).All(ctx, executor)
	if err != nil {
		return err
	}
	if len(drafts) != 0 {
		return fmt.Errorf("%w: post or remove draft purchases before closing the period", ErrConflict)
	}
	return nil
}

func calculateCloseSettlement(ctx context.Context, executor bob.Executor, householdID, period string) (map[string]domain.PoolFundingRule, []domain.SettlementAmount, error) {
	rules, err := PoolFundingRules(ctx, executor, householdID)
	if err != nil {
		return nil, nil, err
	}
	lines, err := calculateSettlementPreviewWithRules(ctx, executor, householdID, period)
	if err != nil {
		return nil, nil, err
	}
	return rules, lines, nil
}

func insertSettlementSnapshot(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, period string, actorID, settlementID gofrsuuid.UUID, revision int64, rules map[string]domain.PoolFundingRule, lines []domain.SettlementAmount) error {
	fundingSnapshotBytes, err := json.Marshal(rules, json.Deterministic(true))
	if err != nil {
		return err
	}
	fundingSnapshot := types.JSON[stdjson.RawMessage]{Val: fundingSnapshotBytes}
	status := closedStatus
	if _, err := models.SettlementRuns.Insert(&models.SettlementRunSetter{
		ID: &settlementID, HouseholdID: &householdID, Period: &period, Revision: &revision,
		Status: &status, CreatedBy: &actorID, FundingSnapshot: &fundingSnapshot,
	}).Exec(ctx, executor); err != nil {
		return err
	}
	for _, line := range lines {
		memberUUID, err := gofrsuuid.FromString(line.MemberID)
		if err != nil {
			return err
		}
		scheduled, paid := line.ScheduledYen, line.PaidYen
		carry, adjustment := line.CarryYen, line.MonthlyAdjustmentYen
		advanceCredit, amountDue := line.AdvanceCreditYen, line.AmountDueYen
		if _, err := models.SettlementLines.Insert(&models.SettlementLineSetter{
			SettlementRunID: &settlementID, MemberID: &memberUUID, ScheduledYen: &scheduled, PaidYen: &paid,
			CarryYen: &carry, MonthlyAdjustmentYen: &adjustment, AdvanceCreditYen: &advanceCredit, AmountDueYen: &amountDue,
		}).Exec(ctx, executor); err != nil {
			return err
		}
	}
	return nil
}

func closePostedPurchases(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, start, end, updatedAt time.Time) error {
	postedStatus, closedPurchaseStatus := "POSTED", closedStatus
	postedPurchases, err := models.Purchases.Query(sm.Where(psql.And(
		models.Purchases.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Purchases.Columns.Status.EQ(psql.Arg(postedStatus)),
		models.Purchases.Columns.OccurredAt.GTE(psql.Arg(start)),
		models.Purchases.Columns.OccurredAt.LT(psql.Arg(end)),
	))).All(ctx, executor)
	if err != nil {
		return err
	}
	for _, purchase := range postedPurchases {
		revision := purchase.Revision + 1
		if _, err := models.Purchases.Update((&models.PurchaseSetter{Status: &closedPurchaseStatus, Revision: &revision, UpdatedAt: &updatedAt}).UpdateMod(), um.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchase.ID)))).Exec(ctx, executor); err != nil {
			return err
		}
	}
	return nil
}

func recordPeriodClosure(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, period string, settlementID gofrsuuid.UUID, revision int64, actorID gofrsuuid.UUID, updatedAt time.Time) error {
	status := closedStatus
	closedBy := sql.Null[gofrsuuid.UUID]{V: actorID, Valid: true}
	closedAt := sql.Null[time.Time]{V: updatedAt, Valid: true}
	if _, err := models.AccountingPeriods.Update((&models.AccountingPeriodSetter{
		Status: &status, Revision: &revision, ClosedBy: &closedBy, ClosedAt: &closedAt,
	}).UpdateMod(), um.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(period)),
	))).Exec(ctx, executor); err != nil {
		return err
	}
	return insertPeriodClosedEvent(ctx, executor, householdID, settlementID, revision, actorID)
}

func materializeFundingObligations(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, period string, start, end time.Time) error {
	schedules, err := models.FundingSchedules.Query(sm.Where(psql.And(
		models.FundingSchedules.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		psql.Or(models.FundingSchedules.Columns.Active.EQ(psql.Arg(true)), models.FundingSchedules.Columns.EndsOn.IsNotNull()),
		models.FundingSchedules.Columns.StartsOn.LT(psql.Arg(end)),
		psql.Or(models.FundingSchedules.Columns.EndsOn.IsNull(), models.FundingSchedules.Columns.EndsOn.GTE(psql.Arg(start))),
	))).All(ctx, executor)
	if err != nil {
		return err
	}
	for _, schedule := range schedules {
		amountYen, err := fundingScheduleAmount(start, end, schedule)
		if err != nil {
			return err
		}
		scheduleID := sql.Null[gofrsuuid.UUID]{V: schedule.ID, Valid: true}
		obligationID, err := gofrsuuid.NewV7()
		if err != nil {
			return err
		}
		if _, err := models.FundingObligations.Insert(&models.FundingObligationSetter{
			ID: &obligationID, HouseholdID: &householdID, ScheduleID: &scheduleID, MemberID: &schedule.MemberID,
			PoolID: &schedule.PoolID, Period: &period, AmountYen: &amountYen,
		}, im.OnConflict("household_id", "member_id", "pool_id", "period").DoUpdate(
			im.SetCol("schedule_id").To(im.Excluded("schedule_id")),
			im.SetCol("amount_yen").To(im.Excluded("amount_yen")),
		)).Exec(ctx, executor); err != nil {
			return err
		}
	}
	return nil
}

func insertPeriodClosedEvent(ctx context.Context, executor bob.Executor, householdID, settlementID gofrsuuid.UUID, revision int64, actorID gofrsuuid.UUID) error {
	encodedFields, err := json.Marshal([]string{"status"})
	if err != nil {
		return err
	}
	changedFields := types.JSON[stdjson.RawMessage]{Val: encodedFields}
	actor := sql.Null[gofrsuuid.UUID]{V: actorID, Valid: true}
	eventType := "period.closed"
	_, err = models.HouseholdEvents.Insert(&models.HouseholdEventSetter{
		HouseholdID: &householdID, Type: &eventType, EntityID: &settlementID, Revision: &revision,
		ChangedFields: &changedFields, ActorID: &actor,
	}).Exec(ctx, executor)
	return err
}

func parsePeriod(period string) (time.Time, error) {
	return time.ParseInLocation("2006-01", period, time.FixedZone("JST", 9*60*60))
}
