package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (r *mutationResolver) setFundingSchedule(ctx context.Context, input model.SetFundingScheduleInput) (*model.FundingSchedule, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	if input.AmountYen < 0 || int64(input.AmountYen) > maxMoney || input.Cadence != "MONTHLY" {
		return nil, fmt.Errorf("%w: funding schedules currently require MONTHLY cadence", ErrBadInput)
	}
	start, err := parseDate(input.StartsOn)
	if err != nil {
		return nil, err
	}
	var end sql.Null[time.Time]
	if input.EndsOn != nil {
		parsed, err := parseDate(*input.EndsOn)
		if err != nil {
			return nil, err
		}
		if parsed.Before(start) {
			return nil, fmt.Errorf("%w: schedule end precedes start", ErrBadInput)
		}
		end = sql.Null[time.Time]{V: parsed, Valid: true}
	}
	if err := verifyHouseholdMember(ctx, r.DB, input.HouseholdID, input.MemberID); err != nil {
		return nil, err
	}
	householdUUID, poolUUID := generatedUUID(input.HouseholdID), generatedUUID(input.PoolID)
	if _, err := models.Pools.Query(sm.Where(psql.And(
		models.Pools.Columns.ID.EQ(psql.Arg(poolUUID)),
		models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
	))).One(ctx, bobpgx.NewPool(r.DB)); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID))), sm.ForUpdate()).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	memberUUID := generatedUUID(input.MemberID)
	priorDay := start.AddDate(0, 0, -1)
	schedules, err := models.FundingSchedules.Query(sm.Where(psql.And(
		models.FundingSchedules.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.FundingSchedules.Columns.MemberID.EQ(psql.Arg(memberUUID)),
		models.FundingSchedules.Columns.PoolID.EQ(psql.Arg(poolUUID)),
		models.FundingSchedules.Columns.Active.EQ(psql.Arg(true)),
		models.FundingSchedules.Columns.StartsOn.LT(psql.Arg(start)),
	))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	for _, schedule := range schedules {
		endDate := priorDay
		if schedule.EndsOn.Valid && schedule.EndsOn.V.Before(endDate) {
			endDate = schedule.EndsOn.V
		}
		endValue := sql.Null[time.Time]{V: endDate, Valid: true}
		active := false
		if _, err := models.FundingSchedules.Update((&models.FundingScheduleSetter{EndsOn: &endValue, Active: &active}).UpdateMod(), um.Where(models.FundingSchedules.Columns.ID.EQ(psql.Arg(schedule.ID)))).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	activeSchedules, err := models.FundingSchedules.Query(sm.Where(psql.And(
		models.FundingSchedules.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.FundingSchedules.Columns.MemberID.EQ(psql.Arg(memberUUID)),
		models.FundingSchedules.Columns.PoolID.EQ(psql.Arg(poolUUID)),
		models.FundingSchedules.Columns.Active.EQ(psql.Arg(true)),
	))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	active := false
	for _, schedule := range activeSchedules {
		if _, err := models.FundingSchedules.Update((&models.FundingScheduleSetter{Active: &active}).UpdateMod(), um.Where(models.FundingSchedules.Columns.ID.EQ(psql.Arg(schedule.ID)))).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	scheduleID := newID()
	scheduleUUID := generatedUUID(scheduleID)
	amountYen := int64(input.AmountYen)
	cadence := string(input.Cadence)
	active = true
	created, err := models.FundingSchedules.Insert(&models.FundingScheduleSetter{
		ID: &scheduleUUID, HouseholdID: &householdUUID, MemberID: &memberUUID, PoolID: &poolUUID,
		AmountYen: &amountYen, Cadence: &cadence, StartsOn: &start, EndsOn: &end, Active: &active,
	}).One(ctx, executor)
	if err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "funding.schedule.updated", scheduleID, 1, []string{fieldAmountYen, fieldStartsOn, fieldEndsOn}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return fundingScheduleModel(created), nil
}
