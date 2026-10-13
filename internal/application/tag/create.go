package tag

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
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/application/inputvalidation"
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
	Name        string `validate:"notblank,max=80"`
}

type Tag struct {
	ID          string
	HouseholdID string
	Name        string
	Archived    bool
	Revision    int64
}

func (s *Service) Create(ctx context.Context, command CreateCommand) (Tag, error) {
	command.Name = strings.TrimSpace(command.Name)
	if err := inputvalidation.Check(command); err != nil {
		return Tag{}, err
	}
	name := command.Name
	householdID, err := gofrsuuid.FromString(command.HouseholdID)
	if err != nil {
		return Tag{}, apperror.Wrap(apperror.KindNotFound, err)
	}
	actorID, err := gofrsuuid.FromString(command.ActorID)
	if err != nil {
		return Tag{}, apperror.Wrap(apperror.KindNotFound, err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Tag{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return Tag{}, apperror.ErrNotFound
	} else if err != nil {
		return Tag{}, err
	}
	id, err := gofrsuuid.NewV7()
	if err != nil {
		return Tag{}, fmt.Errorf("generate tag id: %w", err)
	}
	revision := int64(1)
	row, err := models.Tags.Insert(&models.TagSetter{ID: &id, HouseholdID: &householdID, Name: &name, Revision: &revision}).One(ctx, executor)
	if err != nil {
		return Tag{}, err
	}
	if err := accounting.RecordEvent(ctx, executor, householdID, id, "tag.created", revision, []string{"name"}, actorID); err != nil {
		return Tag{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Tag{}, err
	}
	return Tag{ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), Name: row.Name, Archived: row.Archived, Revision: row.Revision}, nil
}
