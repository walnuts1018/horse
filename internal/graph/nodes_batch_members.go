package graph

import (
	"context"

	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (b *nodeBatch) loadMemberNodes(ctx context.Context) error {
	if len(b.idsByKind[nodeTypeMember]) == 0 {
		return nil
	}
	memberships, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.In(b.householdArgs...),
		models.HouseholdMembers.Columns.MemberID.In(b.ids(nodeTypeMember)...),
	)), sm.OrderBy(models.HouseholdMembers.Columns.JoinedAt)).All(ctx, b.executor)
	if err != nil {
		return err
	}
	memberIDs := make([]bob.Expression, 0, len(memberships))
	rolesByMember := make(map[string]string, len(memberships))
	for _, membership := range memberships {
		id := membership.MemberID.String()
		if _, exists := rolesByMember[id]; !exists {
			rolesByMember[id] = membership.Role
			memberIDs = append(memberIDs, psql.Arg(membership.MemberID))
		}
	}
	if len(memberIDs) == 0 {
		return nil
	}
	members, err := models.Members.Query(sm.Where(models.Members.Columns.ID.In(memberIDs...))).All(ctx, b.executor)
	if err != nil {
		return err
	}
	for _, member := range members {
		id := member.ID.String()
		b.set(nodeTypeMember, id, &model.Member{ID: id, DisplayName: member.DisplayName, Role: model.MemberRole(rolesByMember[id])})
	}
	return nil
}
