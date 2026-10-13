package pool

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

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
	Kind        string
}

type CreatedPool struct {
	ID string
}

func (s *Service) Create(ctx context.Context, command CreateCommand) (CreatedPool, error) {
	name := strings.TrimSpace(command.Name)
	if name == "" || len(name) > 120 || (command.Kind != "PERSONAL" && command.Kind != "SHARED") {
		return CreatedPool{}, apperror.Wrap(apperror.KindBadInput, errors.New("invalid pool"))
	}
	householdID, err := parseUUID(command.HouseholdID)
	if err != nil {
		return CreatedPool{}, apperror.Wrap(apperror.KindBadInput, err)
	}
	actorID, err := parseUUID(command.ActorID)
	if err != nil {
		return CreatedPool{}, apperror.Wrap(apperror.KindNotFound, err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CreatedPool{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	household, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return CreatedPool{}, apperror.ErrNotFound
	}
	if err != nil {
		return CreatedPool{}, err
	}

	poolID, err := gofrsuuid.NewV7()
	if err != nil {
		return CreatedPool{}, fmt.Errorf("generate pool id: %w", err)
	}
	kind := command.Kind
	if _, err := models.Pools.Insert(&models.PoolSetter{ID: &poolID, HouseholdID: &householdID, Name: &name, Kind: &kind}).Exec(ctx, executor); err != nil {
		return CreatedPool{}, err
	}
	initialFundingVersion := int32(1)
	if _, err := models.PoolFundingRules.Insert(&models.PoolFundingRuleSetter{HouseholdID: &householdID, PoolID: &poolID, Version: &initialFundingVersion, CreatedBy: &actorID}).Exec(ctx, executor); err != nil {
		return CreatedPool{}, err
	}

	newVersion := household.DefaultAllocationVersion + 1
	oldDefaults, err := models.AllocationDefaults.Query(sm.Where(psql.And(
		models.AllocationDefaults.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.AllocationDefaults.Columns.Version.EQ(psql.Arg(household.DefaultAllocationVersion)),
	))).All(ctx, executor)
	if err != nil {
		return CreatedPool{}, err
	}
	for _, oldDefault := range oldDefaults {
		poolIDValue, weightValue := oldDefault.PoolID, oldDefault.Weight
		if _, err := models.AllocationDefaults.Insert(&models.AllocationDefaultSetter{
			HouseholdID: &householdID, Version: &newVersion, PoolID: &poolIDValue, Weight: &weightValue,
		}).Exec(ctx, executor); err != nil {
			return CreatedPool{}, err
		}
	}
	defaultWeight := int64(1)
	if _, err := models.AllocationDefaults.Insert(&models.AllocationDefaultSetter{
		HouseholdID: &householdID, Version: &newVersion, PoolID: &poolID, Weight: &defaultWeight,
	}).Exec(ctx, executor); err != nil {
		return CreatedPool{}, err
	}

	updatedAt := time.Now().UTC()
	revision := household.Revision + 1
	if _, err := models.Households.Update((&models.HouseholdSetter{
		DefaultAllocationVersion: &newVersion, Revision: &revision, UpdatedAt: &updatedAt,
	}).UpdateMod(), um.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).Exec(ctx, executor); err != nil {
		return CreatedPool{}, err
	}
	if err := accounting.RecordEvent(ctx, executor, householdID, poolID, "pool.created", newVersion, []string{"name", "kind"}, actorID); err != nil {
		return CreatedPool{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CreatedPool{}, err
	}
	return CreatedPool{ID: poolID.String()}, nil
}

func parseUUID(value string) (gofrsuuid.UUID, error) {
	id, err := gofrsuuid.FromString(value)
	if err != nil {
		return gofrsuuid.Nil, fmt.Errorf("invalid identifier: %w", err)
	}
	return id, nil
}
