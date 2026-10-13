package settlement

import (
	"cmp"
	"context"
	"database/sql"
	stdjson "encoding/json"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
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
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

var (
	ErrBadInput                       = errors.New("invalid input")
	ErrConflict                       = errors.New("resource conflict")
	ErrNotFound                       = errors.New("resource not found")
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
	if _, err := parsePeriod(period); err != nil {
		return Settlement{}, fmt.Errorf("%w: period must use YYYY-MM", ErrBadInput)
	}
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return Settlement{}, fmt.Errorf("%w: invalid household id", ErrBadInput)
	}
	db := bobpgx.NewPool(w.db)
	runs, err := models.SettlementRuns.Query(sm.Where(psql.And(
		models.SettlementRuns.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.SettlementRuns.Columns.Period.EQ(psql.Arg(period)),
		models.SettlementRuns.Columns.Status.EQ(psql.Arg("CLOSED")),
	)), sm.OrderBy(models.SettlementRuns.Columns.Revision).Desc(), sm.Limit(1)).All(ctx, db)
	if err != nil {
		return Settlement{}, err
	}
	if len(runs) == 0 {
		lines, err := calculateSettlementPreview(ctx, db, householdID, period)
		if err != nil {
			return Settlement{}, err
		}
		return Settlement{HouseholdID: householdID, Period: period, Status: "OPEN", Lines: lines}, nil
	}
	run := runs[0]
	lines, err := models.SettlementLines.Query(sm.Where(models.SettlementLines.Columns.SettlementRunID.EQ(psql.Arg(run.ID)))).All(ctx, db)
	if err != nil {
		return Settlement{}, err
	}
	members, err := models.HouseholdMembers.Query(sm.Where(models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdUUID)))).All(ctx, db)
	if err != nil {
		return Settlement{}, err
	}
	if err := members.LoadMember(ctx, db); err != nil {
		return Settlement{}, err
	}
	names := make(map[gofrsuuid.UUID]string, len(members))
	for _, member := range members {
		names[member.MemberID] = member.R.Member.DisplayName
	}
	slices.SortFunc(lines, func(left, right *models.SettlementLine) int {
		return cmp.Compare(names[left.MemberID], names[right.MemberID])
	})
	result := Settlement{HouseholdID: householdID, Period: period, Status: "CLOSED", Revision: run.Revision, Lines: make([]domain.SettlementAmount, 0, len(lines))}
	createdAt := run.CreatedAt
	result.CreatedAt = &createdAt
	for _, line := range lines {
		result.Lines = append(result.Lines, domain.SettlementAmount{
			MemberID: line.MemberID.String(), MemberName: names[line.MemberID], ScheduledYen: line.ScheduledYen,
			PaidYen: line.PaidYen, CarryYen: line.CarryYen, MonthlyAdjustmentYen: line.MonthlyAdjustmentYen,
			AdvanceCreditYen: line.AdvanceCreditYen, AmountDueYen: line.AmountDueYen,
		})
	}
	return result, nil
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
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID))), sm.ForUpdate()).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return Settlement{}, ErrNotFound
	} else if err != nil {
		return Settlement{}, err
	}
	openStatus, zeroRevision := "OPEN", int64(0)
	if _, err := models.AccountingPeriods.Insert(&models.AccountingPeriodSetter{
		HouseholdID: &householdUUID, Period: &command.Period, Status: &openStatus, Revision: &zeroRevision,
	}, im.OnConflict("household_id", "period").DoNothing()).Exec(ctx, executor); err != nil {
		return Settlement{}, err
	}
	periodRow, err := models.AccountingPeriods.Query(sm.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(command.Period)),
	)), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return Settlement{}, err
	}
	if periodRow.Status == "CLOSED" || periodRow.Revision != command.ExpectedRevision {
		return Settlement{}, ErrConflict
	}
	drafts, err := models.Purchases.Query(sm.Where(psql.And(
		models.Purchases.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.Purchases.Columns.Status.EQ(psql.Arg("DRAFT")),
		models.Purchases.Columns.OccurredAt.GTE(psql.Arg(periodStart)),
		models.Purchases.Columns.OccurredAt.LT(psql.Arg(periodEnd)),
	))).All(ctx, executor)
	if err != nil {
		return Settlement{}, err
	}
	if len(drafts) != 0 {
		return Settlement{}, fmt.Errorf("%w: post or remove draft purchases before closing the period", ErrConflict)
	}
	if err := materializeFundingObligations(ctx, executor, householdUUID, command.Period, periodStart, periodEnd); err != nil {
		return Settlement{}, err
	}
	rules, err := PoolFundingRules(ctx, executor, command.HouseholdID)
	if err != nil {
		return Settlement{}, err
	}
	lines, err := calculateSettlementPreviewWithRules(ctx, executor, command.HouseholdID, command.Period)
	if err != nil {
		return Settlement{}, err
	}
	settlementID, err := gofrsuuid.NewV7()
	if err != nil {
		return Settlement{}, err
	}
	newRevision := periodRow.Revision + 1
	fundingSnapshotBytes, err := json.Marshal(rules, json.Deterministic(true))
	if err != nil {
		return Settlement{}, err
	}
	fundingSnapshot := types.JSON[stdjson.RawMessage]{Val: fundingSnapshotBytes}
	closedStatus := "CLOSED"
	if _, err := models.SettlementRuns.Insert(&models.SettlementRunSetter{
		ID: &settlementID, HouseholdID: &householdUUID, Period: &command.Period, Revision: &newRevision,
		Status: &closedStatus, CreatedBy: &actorUUID, FundingSnapshot: &fundingSnapshot,
	}).Exec(ctx, executor); err != nil {
		return Settlement{}, err
	}
	for _, line := range lines {
		memberUUID, err := gofrsuuid.FromString(line.MemberID)
		if err != nil {
			return Settlement{}, err
		}
		scheduled, paid := int64(line.ScheduledYen), int64(line.PaidYen)
		carry, adjustment := int64(line.CarryYen), int64(line.MonthlyAdjustmentYen)
		advanceCredit, amountDue := int64(line.AdvanceCreditYen), int64(line.AmountDueYen)
		if _, err := models.SettlementLines.Insert(&models.SettlementLineSetter{
			SettlementRunID: &settlementID, MemberID: &memberUUID, ScheduledYen: &scheduled, PaidYen: &paid,
			CarryYen: &carry, MonthlyAdjustmentYen: &adjustment, AdvanceCreditYen: &advanceCredit, AmountDueYen: &amountDue,
		}).Exec(ctx, executor); err != nil {
			return Settlement{}, err
		}
	}
	postedStatus, closedPurchaseStatus := "POSTED", "CLOSED"
	updatedAt := time.Now()
	postedPurchases, err := models.Purchases.Query(sm.Where(psql.And(
		models.Purchases.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.Purchases.Columns.Status.EQ(psql.Arg(postedStatus)),
		models.Purchases.Columns.OccurredAt.GTE(psql.Arg(periodStart)),
		models.Purchases.Columns.OccurredAt.LT(psql.Arg(periodEnd)),
	))).All(ctx, executor)
	if err != nil {
		return Settlement{}, err
	}
	for _, purchase := range postedPurchases {
		revision := purchase.Revision + 1
		if _, err := models.Purchases.Update((&models.PurchaseSetter{Status: &closedPurchaseStatus, Revision: &revision, UpdatedAt: &updatedAt}).UpdateMod(), um.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchase.ID)))).Exec(ctx, executor); err != nil {
			return Settlement{}, err
		}
	}
	closedBy := sql.Null[gofrsuuid.UUID]{V: actorUUID, Valid: true}
	closedAt := sql.Null[time.Time]{V: updatedAt, Valid: true}
	if _, err := models.AccountingPeriods.Update((&models.AccountingPeriodSetter{
		Status: &closedStatus, Revision: &newRevision, ClosedBy: &closedBy, ClosedAt: &closedAt,
	}).UpdateMod(), um.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(command.Period)),
	))).Exec(ctx, executor); err != nil {
		return Settlement{}, err
	}
	if err := insertPeriodClosedEvent(ctx, executor, householdUUID, settlementID, newRevision, actorUUID); err != nil {
		return Settlement{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Settlement{}, err
	}
	return w.Get(ctx, command.HouseholdID, command.Period)
}

func materializeFundingObligations(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, period string, start, end time.Time) error {
	schedules, err := models.FundingSchedules.Query(sm.Where(psql.And(
		models.FundingSchedules.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		psql.Or(models.FundingSchedules.Columns.Active.EQ(psql.Arg(true)), models.FundingSchedules.Columns.EndsOn.IsNotNull()),
		models.FundingSchedules.Columns.Cadence.EQ(psql.Arg("MONTHLY")),
		models.FundingSchedules.Columns.StartsOn.LT(psql.Arg(end)),
		psql.Or(models.FundingSchedules.Columns.EndsOn.IsNull(), models.FundingSchedules.Columns.EndsOn.GTE(psql.Arg(start))),
	))).All(ctx, executor)
	if err != nil {
		return err
	}
	for _, schedule := range schedules {
		scheduleID := sql.Null[gofrsuuid.UUID]{V: schedule.ID, Valid: true}
		obligationID, err := gofrsuuid.NewV7()
		if err != nil {
			return err
		}
		if _, err := models.FundingObligations.Insert(&models.FundingObligationSetter{
			ID: &obligationID, HouseholdID: &householdID, ScheduleID: &scheduleID, MemberID: &schedule.MemberID,
			PoolID: &schedule.PoolID, Period: &period, AmountYen: &schedule.AmountYen,
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
