package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	invitationapp "github.com/walnuts1018/horse/internal/application/invitation"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (r *mutationResolver) createHouseholdInvitation(ctx context.Context, input model.CreateHouseholdInvitationInput) (*model.CreatedHouseholdInvitation, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	expiresAt, err := parseTime(input.ExpiresAt)
	if err != nil {
		return nil, err
	}
	created, err := invitationapp.NewService(r.DB).Create(ctx, invitationapp.CreateCommand{
		ActorID: user.MemberID, HouseholdID: input.HouseholdID, ExpiresAt: expiresAt,
	})
	if err != nil {
		return nil, err
	}
	return &model.CreatedHouseholdInvitation{
		Invitation: &model.PendingHouseholdInvitation{HouseholdInvitation: &model.HouseholdInvitation{
			ID: created.ID, HouseholdID: input.HouseholdID,
			ExpiresAt: timeString(created.ExpiresAt), CreatedAt: timeString(created.CreatedAt),
		}},
		Token: created.Token,
	}, nil
}

func (r *queryResolver) householdInvitations(ctx context.Context, householdID string, after, before *string, first, last *int) (*model.HouseholdInvitationConnection, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	executor := bobpgx.NewPool(r.DB)
	if err := requireHouseholdOwner(ctx, executor, householdID, user.MemberID); err != nil {
		return nil, err
	}
	pageSize, backward, err := pageDirection(first, last)
	if err != nil {
		return nil, err
	}
	filters := []bob.Expression{models.HouseholdInvitations.Columns.HouseholdID.EQ(psql.Arg(householdID))}
	count, err := models.HouseholdInvitations.Query(sm.Where(filters[0])).Count(ctx, executor)
	if err != nil {
		return nil, err
	}
	if id, err := cursorID(after, nodeTypeHouseholdInvitation); err != nil {
		return nil, err
	} else if id != nil {
		parsedCursorID, err := gofrsuuid.FromString(*id)
		if err != nil {
			return nil, fmt.Errorf("parse household invitation cursor: %w", err)
		}
		cursor, err := models.HouseholdInvitations.Query(sm.Where(psql.And(
			models.HouseholdInvitations.Columns.ID.EQ(psql.Arg(parsedCursorID)),
			models.HouseholdInvitations.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		filters = append(filters, psql.Or(
			models.HouseholdInvitations.Columns.CreatedAt.LT(psql.Arg(cursor.CreatedAt)),
			psql.And(models.HouseholdInvitations.Columns.CreatedAt.EQ(psql.Arg(cursor.CreatedAt)), models.HouseholdInvitations.Columns.ID.GT(psql.Arg(cursor.ID))),
		))
	}
	if id, err := cursorID(before, nodeTypeHouseholdInvitation); err != nil {
		return nil, err
	} else if id != nil {
		parsedCursorID, err := gofrsuuid.FromString(*id)
		if err != nil {
			return nil, fmt.Errorf("parse household invitation cursor: %w", err)
		}
		cursor, err := models.HouseholdInvitations.Query(sm.Where(psql.And(
			models.HouseholdInvitations.Columns.ID.EQ(psql.Arg(parsedCursorID)),
			models.HouseholdInvitations.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		filters = append(filters, psql.Or(
			models.HouseholdInvitations.Columns.CreatedAt.GT(psql.Arg(cursor.CreatedAt)),
			psql.And(models.HouseholdInvitations.Columns.CreatedAt.EQ(psql.Arg(cursor.CreatedAt)), models.HouseholdInvitations.Columns.ID.LT(psql.Arg(cursor.ID))),
		))
	}
	createdAtOrder := sm.OrderBy(models.HouseholdInvitations.Columns.CreatedAt).Desc()
	idOrder := sm.OrderBy(models.HouseholdInvitations.Columns.ID)
	if backward {
		createdAtOrder = sm.OrderBy(models.HouseholdInvitations.Columns.CreatedAt)
		idOrder = sm.OrderBy(models.HouseholdInvitations.Columns.ID).Desc()
	}
	rows, err := models.HouseholdInvitations.Query(
		sm.Where(psql.And(filters...)),
		createdAtOrder,
		idOrder,
		sm.Limit(pageSize+1),
	).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	hasExtra := len(rows) > pageSize
	if hasExtra {
		rows = rows[:pageSize]
	}
	if backward {
		slices.Reverse(rows)
	}
	pageInfo := &model.PageInfo{HasPreviousPage: backward && hasExtra, HasNextPage: !backward && hasExtra}
	if !backward && after != nil && *after != "" {
		pageInfo.HasPreviousPage = true
	}
	if backward && before != nil && *before != "" {
		pageInfo.HasNextPage = true
	}
	connection := &model.HouseholdInvitationConnection{Edges: make([]*model.HouseholdInvitationEdge, 0, len(rows)), TotalCount: int(count), PageInfo: pageInfo}
	for _, row := range rows {
		invitation := householdInvitationModel(row, time.Now())
		connection.Edges = append(connection.Edges, &model.HouseholdInvitationEdge{Cursor: encodeCursor(nodeTypeHouseholdInvitation, row.ID.String()), Node: invitation})
	}
	if len(rows) > 0 {
		start, end := encodeCursor(nodeTypeHouseholdInvitation, rows[0].ID.String()), encodeCursor(nodeTypeHouseholdInvitation, rows[len(rows)-1].ID.String())
		connection.PageInfo.StartCursor, connection.PageInfo.EndCursor = &start, &end
	}
	return connection, nil
}

func householdInvitationModel(row *models.HouseholdInvitation, now time.Time) model.HouseholdInvitationVariant {
	invitation := &model.HouseholdInvitation{
		ID: row.ID.String(), HouseholdID: row.HouseholdID.String(),
		ExpiresAt: timeString(row.ExpiresAt), CreatedAt: timeString(row.CreatedAt),
	}
	if row.AcceptedAt.Valid {
		return &model.AcceptedHouseholdInvitation{HouseholdInvitation: invitation, AcceptedAt: timeString(row.AcceptedAt.V)}
	}
	if row.RevokedAt.Valid {
		return &model.RevokedHouseholdInvitation{HouseholdInvitation: invitation, RevokedAt: timeString(row.RevokedAt.V)}
	}
	if !row.ExpiresAt.After(now) {
		return &model.ExpiredHouseholdInvitation{HouseholdInvitation: invitation}
	}
	return &model.PendingHouseholdInvitation{HouseholdInvitation: invitation}
}

func (r *mutationResolver) revokeHouseholdInvitation(ctx context.Context, householdID, invitationID string) (model.HouseholdInvitationVariant, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	invitation, err := invitationapp.NewService(r.DB).Revoke(ctx, invitationapp.RevokeCommand{
		ActorID: user.MemberID, HouseholdID: householdID, InvitationID: invitationID,
	})
	if err != nil {
		return nil, err
	}
	return invitationStateModel(invitation), nil
}

func (r *mutationResolver) acceptHouseholdInvitation(ctx context.Context, token string) (*model.Household, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	householdID, err := invitationapp.NewService(r.DB).Accept(ctx, invitationapp.AcceptCommand{MemberID: user.MemberID, Token: token})
	if err != nil {
		return nil, err
	}
	return loadHousehold(ctx, r.DB, householdID, requestedHouseholdFields(ctx, ""))
}

func invitationStateModel(state invitationapp.InvitationState) model.HouseholdInvitationVariant {
	invitation := &model.HouseholdInvitation{
		ID: state.ID, HouseholdID: state.HouseholdID,
		ExpiresAt: timeString(state.ExpiresAt), CreatedAt: timeString(state.CreatedAt),
	}
	if state.AcceptedAt.Valid {
		return &model.AcceptedHouseholdInvitation{HouseholdInvitation: invitation, AcceptedAt: timeString(state.AcceptedAt.V)}
	}
	if state.RevokedAt.Valid {
		return &model.RevokedHouseholdInvitation{HouseholdInvitation: invitation, RevokedAt: timeString(state.RevokedAt.V)}
	}
	return &model.ExpiredHouseholdInvitation{HouseholdInvitation: invitation}
}

func requireHouseholdOwner(ctx context.Context, db bob.Executor, householdID, memberID string) error {
	member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberID)),
	)), sm.ForShare()).One(ctx, db)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if member.Role != "OWNER" {
		return ErrNotFound
	}
	return nil
}
