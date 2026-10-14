package graph

import (
	"context"
	"errors"
	"fmt"

	"github.com/99designs/gqlgen/graphql"
	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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
	memberships, err := models.HouseholdMembers.Query(
		sm.Where(psql.And(
			models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberID)),
			models.HouseholdMembers.Columns.LeftAt.IsNull(),
		)),
		sm.OrderBy(models.HouseholdMembers.Columns.JoinedAt),
	).All(ctx, queryExecutor(r.DB))
	if err != nil {
		return nil, err
	}
	householdIDs := make([]gofrsuuid.UUID, len(memberships))
	for i, membership := range memberships {
		householdIDs[i] = membership.HouseholdID
	}
	households, err := loadHouseholds(ctx, r.DB, householdIDs, requestedHouseholdFields(ctx, "households."))
	if err != nil {
		return nil, err
	}
	for _, membership := range memberships {
		household := households[membership.HouseholdID]
		if household != nil {
			result.Households = append(result.Households, household)
		}
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
	memberships, err := models.HouseholdMembers.Query(
		sm.Where(psql.And(
			models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberID)),
			models.HouseholdMembers.Columns.LeftAt.IsNull(),
		)),
		sm.OrderBy(models.HouseholdMembers.Columns.JoinedAt),
	).All(ctx, queryExecutor(r.DB))
	if err != nil {
		return nil, err
	}
	householdIDs := make([]gofrsuuid.UUID, len(memberships))
	for i, membership := range memberships {
		householdIDs[i] = membership.HouseholdID
	}
	households, err := loadHouseholds(ctx, r.DB, householdIDs, requestedHouseholdFields(ctx, ""))
	if err != nil {
		return nil, err
	}
	result := make([]*model.Household, 0, len(memberships))
	for _, membership := range memberships {
		household := households[membership.HouseholdID]
		if household != nil {
			result = append(result, household)
		}
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
	householdID, err := gofrsuuid.FromString(id)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	households, err := loadHouseholds(ctx, r.DB, []gofrsuuid.UUID{householdID}, requestedHouseholdFields(ctx, ""))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return households[householdID], nil
}

func requestedHouseholdFields(ctx context.Context, prefix string) householdFields {
	if !graphql.HasOperationContext(ctx) {
		return allHouseholdFields
	}
	return householdFields{
		members:           graphql.FieldRequested(ctx, prefix+"members"),
		pools:             graphql.FieldRequested(ctx, prefix+"pools"),
		accounts:          graphql.FieldRequested(ctx, prefix+"accounts"),
		categories:        graphql.FieldRequested(ctx, prefix+"categories"),
		defaultAllocation: graphql.FieldRequested(ctx, prefix+"defaultAllocation"),
	}
}

func loadHouseholds(ctx context.Context, db *pgxpool.Pool, ids []gofrsuuid.UUID, fields householdFields) (map[gofrsuuid.UUID]*model.Household, error) {
	households, allocationVersions, err := loadHouseholdSummaryData(ctx, db, ids)
	if err != nil {
		return nil, err
	}
	if err := loadHouseholdDetails(ctx, db, households, allocationVersions, fields); err != nil {
		return nil, err
	}
	return households, nil
}
