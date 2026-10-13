package graph

import (
	"context"
	"fmt"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (r *mutationResolver) createHousehold(ctx context.Context, input model.CreateHouseholdInput) (*model.Household, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 120 || len(input.MemberNames) > 12 {
		return nil, fmt.Errorf("%w: invalid household name or member count", ErrBadInput)
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	householdID := newID()
	householdUUID := generatedUUID(householdID)
	userUUID, err := gofrsuuid.FromString(user.MemberID)
	if err != nil {
		return nil, err
	}
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Insert(&models.HouseholdSetter{ID: &householdUUID, Name: &name}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	ownerRole := "OWNER"
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdUUID, MemberID: &userUUID, Role: &ownerRole}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	for _, memberName := range input.MemberNames {
		memberName = strings.TrimSpace(memberName)
		if memberName == "" || len(memberName) > 120 {
			return nil, fmt.Errorf("%w: member names must not be empty", ErrBadInput)
		}
		placeholderID := newID()
		placeholderSubject := "invite:" + placeholderID
		placeholderUUID := generatedUUID(placeholderID)
		if _, err := models.Members.Insert(&models.MemberSetter{ID: &placeholderUUID, OidcSubject: &placeholderSubject, DisplayName: &memberName}).Exec(ctx, executor); err != nil {
			return nil, err
		}
		if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdUUID, MemberID: &placeholderUUID}).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	if err := writeEvent(ctx, tx, householdID, "household.created", householdID, 1, []string{}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadHousehold(ctx, r.DB, householdID)
}
