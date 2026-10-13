package category

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/apperror"
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
	HouseholdID string
	Name        string
	ParentID    *string
}

type Category struct {
	ID       string
	Name     string
	ParentID *string
	Archived bool
}

func (s *Service) Create(ctx context.Context, command CreateCommand) (Category, error) {
	name := strings.TrimSpace(command.Name)
	if name == "" || len(name) > 120 {
		return Category{}, apperror.Wrap(apperror.KindBadInput, errors.New("invalid category"))
	}

	actorID, err := parseID(command.ActorID, "actor")
	if err != nil {
		return Category{}, err
	}
	householdID, err := parseID(command.HouseholdID, "household")
	if err != nil {
		return Category{}, err
	}
	var parentID sql.Null[gofrsuuid.UUID]
	if command.ParentID != nil {
		parentUUID, err := parseID(*command.ParentID, "parent category")
		if err != nil {
			return Category{}, err
		}
		parentID = sql.Null[gofrsuuid.UUID]{V: parentUUID, Valid: true}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Category{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return Category{}, apperror.ErrNotFound
	} else if err != nil {
		return Category{}, err
	}
	if parentID.Valid {
		if _, err := models.Categories.Query(sm.Where(psql.And(
			models.Categories.Columns.ID.EQ(psql.Arg(parentID.V)),
			models.Categories.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		))).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
			return Category{}, apperror.ErrNotFound
		} else if err != nil {
			return Category{}, err
		}
	}

	id, err := gofrsuuid.NewV7()
	if err != nil {
		return Category{}, fmt.Errorf("generate category id: %w", err)
	}
	row, err := models.Categories.Insert(&models.CategorySetter{ID: &id, HouseholdID: &householdID, ParentID: &parentID, Name: &name}).One(ctx, executor)
	if err != nil {
		return Category{}, err
	}
	if err := accounting.RecordEvent(ctx, executor, householdID, id, "category.created", 1, []string{"name", "parentId"}, actorID); err != nil {
		return Category{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Category{}, err
	}
	var resultParentID *string
	if row.ParentID.Valid {
		value := row.ParentID.V.String()
		resultParentID = &value
	}
	return Category{ID: row.ID.String(), Name: row.Name, ParentID: resultParentID, Archived: row.Archived}, nil
}

type SetArchivedCommand struct {
	ActorID    string
	CategoryID string
	Archived   bool
}

func (s *Service) SetArchived(ctx context.Context, command SetArchivedCommand) (Category, error) {
	actorID, err := parseID(command.ActorID, "actor")
	if err != nil {
		return Category{}, err
	}
	categoryID, err := parseID(command.CategoryID, "category")
	if err != nil {
		return Category{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Category{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	category, err := models.Categories.Query(sm.Where(models.Categories.Columns.ID.EQ(psql.Arg(categoryID))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return Category{}, apperror.ErrNotFound
	} else if err != nil {
		return Category{}, err
	}
	member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(category.HouseholdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(actorID)),
	))).Exists(ctx, executor)
	if err != nil {
		return Category{}, err
	}
	if !member {
		return Category{}, apperror.ErrNotFound
	}
	if category.Archived != command.Archived {
		if _, err := models.Categories.Update((&models.CategorySetter{Archived: &command.Archived}).UpdateMod(), um.Where(psql.And(
			models.Categories.Columns.ID.EQ(psql.Arg(categoryID)),
			models.Categories.Columns.HouseholdID.EQ(psql.Arg(category.HouseholdID)),
		))).Exec(ctx, executor); err != nil {
			return Category{}, err
		}
		eventType := "category.unarchived"
		if command.Archived {
			eventType = "category.archived"
		}
		if err := accounting.RecordEvent(ctx, executor, category.HouseholdID, categoryID, eventType, 1, []string{"archived"}, actorID); err != nil {
			return Category{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Category{}, err
	}
	var parentID *string
	if category.ParentID.Valid {
		parent := category.ParentID.V.String()
		parentID = &parent
	}
	return Category{ID: category.ID.String(), Name: category.Name, ParentID: parentID, Archived: command.Archived}, nil
}

func parseID(value, resource string) (gofrsuuid.UUID, error) {
	id, err := gofrsuuid.FromString(value)
	if err != nil {
		return gofrsuuid.UUID{}, apperror.Wrap(apperror.KindBadInput, fmt.Errorf("invalid %s id", resource))
	}
	return id, nil
}
