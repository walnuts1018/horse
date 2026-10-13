package graph

import (
	"context"
	"database/sql"
	stdjson "encoding/json"
	"encoding/json/v2"
	"errors"
	"fmt"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (r *mutationResolver) closePeriod(ctx context.Context, input model.ClosePeriodInput) (*model.Settlement, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	period := string(input.Period)
	periodStart, err := parseAccountingPeriod(period)
	if err != nil {
		return nil, fmt.Errorf("%w: period must use YYYY-MM", ErrBadInput)
	}
	periodEnd := periodStart.AddDate(0, 1, 0)
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	householdUUID := generatedUUID(input.HouseholdID)
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID))), sm.ForUpdate()).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	openStatus, zeroRevision := "OPEN", int64(0)
	if _, err := models.AccountingPeriods.Insert(&models.AccountingPeriodSetter{
		HouseholdID: &householdUUID, Period: &period, Status: &openStatus, Revision: &zeroRevision,
	}, im.OnConflict("household_id", "period").DoNothing()).Exec(ctx, executor); err != nil {
		return nil, err
	}
	periodRow, err := models.AccountingPeriods.Query(sm.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(period)),
	)), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return nil, err
	}
	if periodRow.Status == "CLOSED" {
		return nil, ErrConflict
	}
	if periodRow.Revision != int64(input.ExpectedRevision) {
		return nil, ErrConflict
	}
	purchases, err := models.Purchases.Query(sm.Where(psql.And(
		models.Purchases.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.Purchases.Columns.Status.EQ(psql.Arg(string(model.PurchaseStatusDraft))),
		models.Purchases.Columns.OccurredAt.GTE(psql.Arg(periodStart)),
		models.Purchases.Columns.OccurredAt.LT(psql.Arg(periodEnd)),
	))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	if len(purchases) != 0 {
		return nil, fmt.Errorf("%w: post or remove draft purchases before closing the period", ErrConflict)
	}
	schedules, err := models.FundingSchedules.Query(sm.Where(psql.And(
		models.FundingSchedules.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		psql.Or(
			models.FundingSchedules.Columns.Active.EQ(psql.Arg(true)),
			models.FundingSchedules.Columns.EndsOn.IsNotNull(),
		),
		models.FundingSchedules.Columns.Cadence.EQ(psql.Arg("MONTHLY")),
		models.FundingSchedules.Columns.StartsOn.LT(psql.Arg(periodEnd)),
		psql.Or(
			models.FundingSchedules.Columns.EndsOn.IsNull(),
			models.FundingSchedules.Columns.EndsOn.GTE(psql.Arg(periodStart)),
		),
	))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	for _, item := range schedules {
		scheduleID := sql.Null[gofrsuuid.UUID]{V: item.ID, Valid: true}
		if _, err := models.FundingObligations.Insert(&models.FundingObligationSetter{
			ID: func() *gofrsuuid.UUID { id := generatedUUID(newID()); return &id }(), HouseholdID: &householdUUID, ScheduleID: &scheduleID,
			MemberID: &item.MemberID, PoolID: &item.PoolID, Period: &period, AmountYen: &item.AmountYen,
		}, im.OnConflict("household_id", "member_id", "pool_id", "period").DoUpdate(
			im.SetCol("schedule_id").To(im.Excluded("schedule_id")),
			im.SetCol("amount_yen").To(im.Excluded("amount_yen")),
		)).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	rules, err := poolFundingRules(ctx, executor, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	lines, err := calculateSettlementPreviewWithRules(ctx, executor, input.HouseholdID, period)
	if err != nil {
		return nil, err
	}
	settlementID := newID()
	newRevision := periodRow.Revision + 1
	fundingSnapshot, err := json.Marshal(rules)
	if err != nil {
		return nil, err
	}
	settlementUUID, userUUID := generatedUUID(settlementID), generatedUUID(user.MemberID)
	closedStatus := "CLOSED"
	snapshot := types.JSON[stdjson.RawMessage]{Val: fundingSnapshot}
	if _, err := models.SettlementRuns.Insert(&models.SettlementRunSetter{
		ID: &settlementUUID, HouseholdID: &householdUUID, Period: &period, Revision: &newRevision,
		Status: &closedStatus, CreatedBy: &userUUID, FundingSnapshot: &snapshot,
	}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	for _, line := range lines {
		memberUUID := generatedUUID(line.MemberID)
		scheduled, paid := int64(line.ScheduledYen), int64(line.PaidYen)
		carry, monthlyAdjustment := int64(line.CarryYen), int64(line.MonthlyAdjustmentYen)
		advanceCredit, amountDue := int64(line.AdvanceCreditYen), int64(line.AmountDueYen)
		if _, err := models.SettlementLines.Insert(&models.SettlementLineSetter{
			SettlementRunID: &settlementUUID, MemberID: &memberUUID, ScheduledYen: &scheduled, PaidYen: &paid,
			CarryYen: &carry, MonthlyAdjustmentYen: &monthlyAdjustment, AdvanceCreditYen: &advanceCredit, AmountDueYen: &amountDue,
		}).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	postedStatus := string(model.PurchaseStatusPosted)
	closedPurchaseStatus := string(model.PurchaseStatusClosed)
	updatedAt := time.Now()
	postedPurchases, err := models.Purchases.Query(sm.Where(psql.And(
		models.Purchases.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.Purchases.Columns.Status.EQ(psql.Arg(postedStatus)),
		models.Purchases.Columns.OccurredAt.GTE(psql.Arg(periodStart)),
		models.Purchases.Columns.OccurredAt.LT(psql.Arg(periodEnd)),
	))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	for _, purchase := range postedPurchases {
		revision := purchase.Revision + 1
		if _, err := models.Purchases.Update((&models.PurchaseSetter{Status: &closedPurchaseStatus, Revision: &revision, UpdatedAt: &updatedAt}).UpdateMod(), um.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchase.ID)))).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	closedBy := sql.Null[gofrsuuid.UUID]{V: userUUID, Valid: true}
	closedAt := sql.Null[time.Time]{V: updatedAt, Valid: true}
	if _, err := models.AccountingPeriods.Update((&models.AccountingPeriodSetter{
		Status: &closedStatus, Revision: &newRevision, ClosedBy: &closedBy, ClosedAt: &closedAt,
	}).UpdateMod(), um.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(period)),
	))).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "period.closed", settlementID, newRevision, []string{"status"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return (&queryResolver{r.Resolver}).settlement(ctx, input.HouseholdID, string(input.Period))
}
