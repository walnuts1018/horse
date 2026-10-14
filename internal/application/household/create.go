package household

import (
	"context"
	"errors"
	"fmt"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/application/txcleanup"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

type CreateCommand struct {
	ActorID     string
	Name        string
	MemberNames []string
}

type CreatedHousehold struct {
	ID string
}

func (s *Service) Create(ctx context.Context, command CreateCommand) (CreatedHousehold, error) {
	name := strings.TrimSpace(command.Name)
	if name == "" || len(name) > 120 || len(command.MemberNames) > 12 {
		return CreatedHousehold{}, apperror.Wrap(apperror.KindBadInput, errors.New("invalid household name or member count"))
	}
	memberNames := make([]string, len(command.MemberNames))
	for index, memberName := range command.MemberNames {
		memberName = strings.TrimSpace(memberName)
		if memberName == "" || len(memberName) > 120 {
			return CreatedHousehold{}, apperror.Wrap(apperror.KindBadInput, errors.New("member names must not be empty"))
		}
		memberNames[index] = memberName
	}
	actorID, err := gofrsuuid.FromString(command.ActorID)
	if err != nil {
		return CreatedHousehold{}, fmt.Errorf("parse household actor id: %w", err)
	}
	householdID, err := gofrsuuid.NewV7()
	if err != nil {
		return CreatedHousehold{}, fmt.Errorf("generate household id: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CreatedHousehold{}, err
	}
	defer txcleanup.Rollback(ctx, tx)
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Insert(&models.HouseholdSetter{ID: &householdID, Name: &name}).Exec(ctx, executor); err != nil {
		return CreatedHousehold{}, err
	}
	ownerRole := "OWNER"
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{
		HouseholdID: &householdID,
		MemberID:    &actorID,
		Role:        &ownerRole,
	}).Exec(ctx, executor); err != nil {
		return CreatedHousehold{}, err
	}
	for _, memberName := range memberNames {
		placeholderID, err := gofrsuuid.NewV7()
		if err != nil {
			return CreatedHousehold{}, fmt.Errorf("generate placeholder member id: %w", err)
		}
		placeholderSubject := "invite:" + placeholderID.String()
		if _, err := models.Members.Insert(&models.MemberSetter{
			ID:          &placeholderID,
			OidcSubject: &placeholderSubject,
			DisplayName: &memberName,
		}).Exec(ctx, executor); err != nil {
			return CreatedHousehold{}, err
		}
		if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{
			HouseholdID: &householdID,
			MemberID:    &placeholderID,
		}).Exec(ctx, executor); err != nil {
			return CreatedHousehold{}, err
		}
	}
	if err := accounting.RecordEvent(ctx, executor, householdID, householdID, "household.created", 1, []string{}, actorID); err != nil {
		return CreatedHousehold{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CreatedHousehold{}, err
	}
	return CreatedHousehold{ID: householdID.String()}, nil
}
