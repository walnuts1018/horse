package graph

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	purchaseapp "github.com/walnuts1018/horse/internal/application/purchase"
	settlementapp "github.com/walnuts1018/horse/internal/application/settlement"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type accountingDB interface {
	bob.Executor
}

var ErrUnattributedPurchasePayment = purchaseapp.ErrUnattributedPurchasePayment
var ErrMissingPurchaseFundingSnapshot = settlementapp.ErrMissingPurchaseFundingSnapshot

var accountingLocation = time.FixedZone("JST", 9*60*60)

func accountingPeriod(value time.Time) string { return value.In(accountingLocation).Format("2006-01") }

func parseAccountingPeriod[T ~string](value T) (time.Time, error) {
	return time.ParseInLocation("2006-01", string(value), accountingLocation)
}

func (r *mutationResolver) setPoolFundingRule(ctx context.Context, input model.SetPoolFundingRuleInput) (*model.Pool, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	householdID, err := gofrsuuid.FromString(input.HouseholdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	poolID, err := gofrsuuid.FromString(input.PoolID)
	if err != nil {
		return nil, fmt.Errorf("parse pool id: %w", err)
	}
	userID, err := gofrsuuid.FromString(user.MemberID)
	if err != nil {
		return nil, fmt.Errorf("parse member id: %w", err)
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	household, err := models.Households.Query(
		sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))),
		sm.ForUpdate(),
	).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	pool, err := models.Pools.Query(sm.Where(psql.And(
		models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Pools.Columns.ID.EQ(psql.Arg(poolID)),
	)), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if int(pool.FundingRuleVersion) != input.ExpectedVersion {
		return nil, ErrConflict
	}
	configuration := input.Configuration
	if configuration == nil || (configuration.Personal == nil) == (configuration.Shared == nil) {
		return nil, fmt.Errorf("%w: pool funding configuration must select exactly one pool kind", ErrBadInput)
	}
	shares := make(map[gofrsuuid.UUID]int64)
	var ownerID sql.Null[gofrsuuid.UUID]
	var settlementPolicy sql.Null[string]
	if pool.Kind == string(model.PoolKindPersonal) {
		if configuration.Personal == nil {
			return nil, fmt.Errorf("%w: personal pools require a personal funding configuration", ErrBadInput)
		}
		ownerUUID, err := gofrsuuid.FromString(configuration.Personal.OwnerMemberID)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid owner member id", ErrBadInput)
		}
		member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
			models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(ownerUUID)),
		))).Exists(ctx, executor)
		if err != nil {
			return nil, err
		}
		if !member {
			return nil, fmt.Errorf("%w: owner must belong to the household", ErrBadInput)
		}
		ownerID = sql.Null[gofrsuuid.UUID]{V: ownerUUID, Valid: true}
	} else if pool.Kind == string(model.PoolKindShared) {
		if configuration.Shared == nil || !configuration.Shared.SettlementPolicy.IsValid() || len(configuration.Shared.FundingShares) == 0 {
			return nil, fmt.Errorf("%w: shared pools require funding shares and a settlement policy", ErrBadInput)
		}
		settlementPolicy = sql.Null[string]{V: string(configuration.Shared.SettlementPolicy), Valid: true}
		var total int64
		for _, share := range configuration.Shared.FundingShares {
			if share == nil || share.MemberID == "" || share.Weight <= 0 {
				return nil, fmt.Errorf("%w: funding shares require positive weights", ErrBadInput)
			}
			memberID, err := gofrsuuid.FromString(share.MemberID)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid funding share member id", ErrBadInput)
			}
			if _, exists := shares[memberID]; exists {
				return nil, fmt.Errorf("%w: duplicate funding share member", ErrBadInput)
			}
			if int64(share.Weight) > math.MaxInt64-total {
				return nil, fmt.Errorf("%w: funding share total is too large", ErrBadInput)
			}
			total += int64(share.Weight)
			shares[memberID] = int64(share.Weight)
		}
		for memberID := range shares {
			member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
				models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
				models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberID)),
			))).Exists(ctx, executor)
			if err != nil {
				return nil, err
			}
			if !member {
				return nil, fmt.Errorf("%w: funding share member must belong to the household", ErrBadInput)
			}
		}
	} else {
		return nil, fmt.Errorf("%w: invalid pool kind", ErrBadInput)
	}
	if pool.FundingRuleVersion == math.MaxInt32 {
		return nil, fmt.Errorf("%w: funding rule version is exhausted", ErrBadInput)
	}
	newVersion := pool.FundingRuleVersion + 1
	if _, err := models.PoolFundingRules.Insert(&models.PoolFundingRuleSetter{
		HouseholdID: &householdID, PoolID: &poolID, Version: &newVersion,
		OwnerMemberID: &ownerID, SettlementPolicy: &settlementPolicy, CreatedBy: &userID,
	}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	memberIDs := make([]gofrsuuid.UUID, 0, len(shares))
	for memberID := range shares {
		memberIDs = append(memberIDs, memberID)
	}
	slices.SortFunc(memberIDs, func(left, right gofrsuuid.UUID) int { return cmp.Compare(left.String(), right.String()) })
	for _, memberID := range memberIDs {
		weight := shares[memberID]
		if _, err := models.PoolFundingShares.Insert(&models.PoolFundingShareSetter{
			HouseholdID: &householdID, PoolID: &poolID, Version: &newVersion, MemberID: &memberID, Weight: &weight,
		}).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	if _, err := models.Pools.Update((&models.PoolSetter{FundingRuleVersion: &newVersion}).UpdateMod(), um.Where(psql.And(
		models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Pools.Columns.ID.EQ(psql.Arg(poolID)),
	))).Exec(ctx, executor); err != nil {
		return nil, err
	}
	updatedAt := time.Now()
	householdRevision, ok := checkedAdd(household.Revision, 1)
	if !ok {
		return nil, fmt.Errorf("%w: household revision is exhausted", ErrBadInput)
	}
	if _, err := models.Households.Update((&models.HouseholdSetter{Revision: &householdRevision, UpdatedAt: &updatedAt}).UpdateMod(), um.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "pool.funding_rule.updated", input.PoolID, int64(newVersion), []string{fieldOwnerMemberID, fieldFundingShares, fieldSettlementPolicy}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	householdResult, err := loadHousehold(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	for _, resultPool := range householdResult.PoolVariants {
		if value := poolData(resultPool); value != nil && value.ID == input.PoolID {
			return value, nil
		}
	}
	return nil, ErrNotFound
}

func poolFundingRules(ctx context.Context, db accountingDB, householdID string) (map[string]domain.PoolFundingRule, error) {
	return settlementapp.PoolFundingRules(ctx, db, householdID)
}

func calculatePeriodAccounting(ctx context.Context, db accountingDB, householdID, period string) ([]domain.SettlementAmount, error) {
	return settlementapp.CalculatePeriod(ctx, db, householdID, period)
}

func checkedAdd(left, right int64) (int64, bool) {
	if right > 0 && left > math.MaxInt64-right || right < 0 && left < math.MinInt64-right {
		return 0, false
	}
	return left + right, true
}
