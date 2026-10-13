package graph

import (
	"context"
	"time"

	scheduleapp "github.com/walnuts1018/horse/internal/application/funding_schedule"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func (r *mutationResolver) setFundingSchedule(ctx context.Context, input model.SetFundingScheduleInput) (*model.FundingSchedule, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	start, err := parseDate(input.StartsOn)
	if err != nil {
		return nil, err
	}
	var end *time.Time
	if input.EndsOn != nil {
		parsed, err := parseDate(*input.EndsOn)
		if err != nil {
			return nil, err
		}
		end = &parsed
	}
	if err := verifyHouseholdMember(ctx, r.DB, input.HouseholdID, input.MemberID); err != nil {
		return nil, err
	}
	schedule, err := scheduleapp.NewService(r.DB).Set(ctx, scheduleapp.SetCommand{
		ActorID: user.MemberID, HouseholdID: input.HouseholdID, MemberID: input.MemberID, PoolID: input.PoolID,
		AmountYen: int64(input.AmountYen), Cadence: string(input.Cadence), StartsOn: start, EndsOn: end,
	})
	if err != nil {
		return nil, err
	}
	var endsOn *string
	if schedule.EndsOn != nil {
		value := schedule.EndsOn.Format(time.DateOnly)
		endsOn = &value
	}
	return &model.FundingSchedule{
		ID: schedule.ID, HouseholdID: schedule.HouseholdID, MemberID: schedule.MemberID, PoolID: schedule.PoolID,
		AmountYen: graphLong(schedule.AmountYen), Cadence: model.FundingScheduleCadence(schedule.Cadence),
		StartsOn: model.Date(schedule.StartsOn.Format(time.DateOnly)), EndsOn: datePointer(endsOn), Active: schedule.Active,
		CreatedAt: timeString(schedule.CreatedAt),
	}, nil
}
