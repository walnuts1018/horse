package graph

import (
	"context"
	"errors"
	"fmt"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (r *queryResolver) viewer(ctx context.Context) (*model.Viewer, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	result := &model.Viewer{ID: user.MemberID, Subject: user.Subject, DisplayName: user.DisplayName, Households: make([]*model.Household, 0)}
	memberID, err := gofrsuuid.FromString(user.MemberID)
	if err != nil {
		return nil, fmt.Errorf("parse member id: %w", err)
	}
	memberships, err := models.HouseholdMembers.Query(sm.Where(models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberID))), sm.OrderBy(models.HouseholdMembers.Columns.JoinedAt)).All(ctx, queryExecutor(r.DB))
	if err != nil {
		return nil, err
	}
	for _, membership := range memberships {
		household, err := loadHousehold(ctx, r.DB, membership.HouseholdID.String())
		if err != nil {
			return nil, err
		}
		result.Households = append(result.Households, household)
	}
	return result, nil
}

func (r *queryResolver) households(ctx context.Context) ([]*model.Household, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	memberID, err := gofrsuuid.FromString(user.MemberID)
	if err != nil {
		return nil, fmt.Errorf("parse member id: %w", err)
	}
	memberships, err := models.HouseholdMembers.Query(sm.Where(models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberID))), sm.OrderBy(models.HouseholdMembers.Columns.JoinedAt)).All(ctx, queryExecutor(r.DB))
	if err != nil {
		return nil, err
	}
	result := make([]*model.Household, 0, len(memberships))
	for _, membership := range memberships {
		household, err := loadHousehold(ctx, r.DB, membership.HouseholdID.String())
		if err != nil {
			return nil, err
		}
		result = append(result, household)
	}
	return result, nil
}

func (r *queryResolver) household(ctx context.Context, id string) (*model.Household, error) {
	if _, err := requireMember(ctx, r.DB, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	household, err := loadHousehold(ctx, r.DB, id)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return household, err
}
