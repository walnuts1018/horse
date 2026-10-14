package funding_schedule

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/application/txcleanup"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const maxAmountYen int64 = 1_000_000_000_000

type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

type SetCommand struct {
	ActorID     string
	HouseholdID string
	MemberID    string
	PoolID      string
	AmountYen   int64
	Cadence     string
	StartsOn    time.Time
	EndsOn      *time.Time
}

type Schedule struct {
	ID          string
	HouseholdID string
	MemberID    string
	PoolID      string
	AmountYen   int64
	Cadence     string
	StartsOn    time.Time
	EndsOn      *time.Time
	Active      bool
	CreatedAt   time.Time
}

func (s *Service) Set(ctx context.Context, command SetCommand) (Schedule, error) {
	if command.AmountYen < 0 || command.AmountYen > maxAmountYen || command.Cadence != "MONTHLY" {
		return Schedule{}, apperror.Wrap(apperror.KindBadInput, errors.New("funding schedules currently require MONTHLY cadence"))
	}
	if command.EndsOn != nil && command.EndsOn.Before(command.StartsOn) {
		return Schedule{}, apperror.Wrap(apperror.KindBadInput, errors.New("schedule end precedes start"))
	}
	actorID, err := parseID(command.ActorID)
	if err != nil {
		return Schedule{}, apperror.Wrap(apperror.KindNotFound, err)
	}
	householdID, err := parseID(command.HouseholdID)
	if err != nil {
		return Schedule{}, apperror.Wrap(apperror.KindNotFound, err)
	}
	memberID, err := parseID(command.MemberID)
	if err != nil {
		return Schedule{}, apperror.Wrap(apperror.KindNotFound, err)
	}
	poolID, err := parseID(command.PoolID)
	if err != nil {
		return Schedule{}, apperror.Wrap(apperror.KindNotFound, err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Schedule{}, err
	}
	defer txcleanup.Rollback(ctx, tx)
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return Schedule{}, apperror.ErrNotFound
	} else if err != nil {
		return Schedule{}, err
	}
	if _, err := models.Pools.Query(sm.Where(psql.And(
		models.Pools.Columns.ID.EQ(psql.Arg(poolID)),
		models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdID)),
	))).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return Schedule{}, apperror.ErrNotFound
	} else if err != nil {
		return Schedule{}, err
	}

	priorDay := command.StartsOn.AddDate(0, 0, -1)
	priorSchedules, err := models.FundingSchedules.Query(sm.Where(psql.And(
		models.FundingSchedules.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.FundingSchedules.Columns.MemberID.EQ(psql.Arg(memberID)),
		models.FundingSchedules.Columns.PoolID.EQ(psql.Arg(poolID)),
		models.FundingSchedules.Columns.Active.EQ(psql.Arg(true)),
		models.FundingSchedules.Columns.StartsOn.LT(psql.Arg(command.StartsOn)),
	))).All(ctx, executor)
	if err != nil {
		return Schedule{}, err
	}
	for _, schedule := range priorSchedules {
		endDate := priorDay
		if schedule.EndsOn.Valid && schedule.EndsOn.V.Before(endDate) {
			endDate = schedule.EndsOn.V
		}
		endValue := sql.Null[time.Time]{V: endDate, Valid: true}
		active := false
		if _, err := models.FundingSchedules.Update((&models.FundingScheduleSetter{EndsOn: &endValue, Active: &active}).UpdateMod(), um.Where(models.FundingSchedules.Columns.ID.EQ(psql.Arg(schedule.ID)))).Exec(ctx, executor); err != nil {
			return Schedule{}, err
		}
	}

	activeSchedules, err := models.FundingSchedules.Query(sm.Where(psql.And(
		models.FundingSchedules.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.FundingSchedules.Columns.MemberID.EQ(psql.Arg(memberID)),
		models.FundingSchedules.Columns.PoolID.EQ(psql.Arg(poolID)),
		models.FundingSchedules.Columns.Active.EQ(psql.Arg(true)),
	))).All(ctx, executor)
	if err != nil {
		return Schedule{}, err
	}
	active := false
	for _, schedule := range activeSchedules {
		if _, err := models.FundingSchedules.Update((&models.FundingScheduleSetter{Active: &active}).UpdateMod(), um.Where(models.FundingSchedules.Columns.ID.EQ(psql.Arg(schedule.ID)))).Exec(ctx, executor); err != nil {
			return Schedule{}, err
		}
	}

	scheduleID, err := gofrsuuid.NewV7()
	if err != nil {
		return Schedule{}, fmt.Errorf("generate funding schedule id: %w", err)
	}
	amountYen, cadence := command.AmountYen, command.Cadence
	endsOn := sql.Null[time.Time]{}
	if command.EndsOn != nil {
		endsOn = sql.Null[time.Time]{V: *command.EndsOn, Valid: true}
	}
	active = true
	created, err := models.FundingSchedules.Insert(&models.FundingScheduleSetter{
		ID: &scheduleID, HouseholdID: &householdID, MemberID: &memberID, PoolID: &poolID,
		AmountYen: &amountYen, Cadence: &cadence, StartsOn: &command.StartsOn, EndsOn: &endsOn, Active: &active,
	}).One(ctx, executor)
	if err != nil {
		return Schedule{}, err
	}
	if err := accounting.RecordEvent(ctx, executor, householdID, scheduleID, "funding.schedule.updated", 1, []string{"amountYen", "startsOn", "endsOn"}, actorID); err != nil {
		return Schedule{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Schedule{}, err
	}
	return toSchedule(created), nil
}

func parseID(value string) (gofrsuuid.UUID, error) {
	id, err := gofrsuuid.FromString(value)
	if err != nil {
		return gofrsuuid.Nil, fmt.Errorf("invalid identifier: %w", err)
	}
	return id, nil
}

func toSchedule(row *models.FundingSchedule) Schedule {
	result := Schedule{
		ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), MemberID: row.MemberID.String(), PoolID: row.PoolID.String(),
		AmountYen: row.AmountYen, Cadence: row.Cadence, StartsOn: row.StartsOn, Active: row.Active, CreatedAt: row.CreatedAt,
	}
	if row.EndsOn.Valid {
		result.EndsOn = &row.EndsOn.V
	}
	return result
}
