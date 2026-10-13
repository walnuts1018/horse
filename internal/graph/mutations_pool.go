package graph

import (
	"context"
	"fmt"
	"strings"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (r *mutationResolver) createPool(ctx context.Context, input model.CreatePoolInput) (*model.Pool, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 120 || !input.Kind.IsValid() {
		return nil, fmt.Errorf("%w: invalid pool", ErrBadInput)
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	householdUUID, err := gofrsuuid.FromString(input.HouseholdID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid household id", ErrBadInput)
	}
	executor := bobpgx.NewTx(tx, nil)
	householdRow, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID))), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return nil, err
	}
	version := int64(householdRow.DefaultAllocationVersion)
	poolID := newID()
	poolUUID := generatedUUID(poolID)
	poolKind := string(input.Kind)
	if _, err := models.Pools.Insert(&models.PoolSetter{ID: &poolUUID, HouseholdID: &householdUUID, Name: &name, Kind: &poolKind}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	initialFundingVersion := int32(1)
	createdByUUID, err := gofrsuuid.FromString(user.MemberID)
	if err != nil {
		return nil, err
	}
	if _, err := models.PoolFundingRules.Insert(&models.PoolFundingRuleSetter{HouseholdID: &householdUUID, PoolID: &poolUUID, Version: &initialFundingVersion, CreatedBy: &createdByUUID}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	newVersion := version + 1
	oldDefaults, err := models.AllocationDefaults.Query(sm.Where(psql.And(
		models.AllocationDefaults.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.AllocationDefaults.Columns.Version.EQ(psql.Arg(version)),
	))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	newVersionValue := newVersion
	for _, oldDefault := range oldDefaults {
		poolIDValue, weightValue := oldDefault.PoolID, oldDefault.Weight
		if _, err := models.AllocationDefaults.Insert(&models.AllocationDefaultSetter{
			HouseholdID: &householdUUID, Version: &newVersionValue, PoolID: &poolIDValue, Weight: &weightValue,
		}).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	defaultWeight := int64(1)
	if _, err := models.AllocationDefaults.Insert(&models.AllocationDefaultSetter{
		HouseholdID: &householdUUID, Version: &newVersionValue, PoolID: &poolUUID, Weight: &defaultWeight,
	}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	newUpdatedAt := time.Now().UTC()
	newRevision := householdRow.Revision + 1
	if _, err := models.Households.Update((&models.HouseholdSetter{
		DefaultAllocationVersion: &newVersionValue, Revision: &newRevision, UpdatedAt: &newUpdatedAt,
	}).UpdateMod(), um.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "pool.created", poolID, newVersion, []string{"name", "kind"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	household, err := loadHousehold(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	for _, pool := range household.PoolVariants {
		if value := poolData(pool); value != nil && value.ID == poolID {
			return value, nil
		}
	}
	return nil, ErrNotFound
}
