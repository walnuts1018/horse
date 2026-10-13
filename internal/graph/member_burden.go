package graph

import (
	"context"
	"database/sql"
	"fmt"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func refreshPurchaseMemberBurden(ctx context.Context, executor bob.Executor, householdID string, purchaseUUID gofrsuuid.UUID, allocations models.ItemAllocationSlice) error {
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return fmt.Errorf("parse household id: %w", err)
	}
	previous, err := models.PurchaseMemberBurdens.Query(sm.Where(psql.And(
		models.PurchaseMemberBurdens.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.PurchaseMemberBurdens.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)),
	))).All(ctx, executor)
	if err != nil {
		return err
	}
	type allocationKey struct {
		itemID gofrsuuid.UUID
		poolID gofrsuuid.UUID
	}
	weightsByAllocation := make(map[allocationKey][]domain.Weight)
	versionsByAllocation := make(map[allocationKey]int32)
	sourcesByAllocation := make(map[allocationKey]string)
	for _, row := range previous {
		key := allocationKey{itemID: row.ItemID, poolID: row.PoolID}
		weightsByAllocation[key] = append(weightsByAllocation[key], domain.Weight{PoolID: row.MemberID.String(), Value: row.FundingWeight})
		versionsByAllocation[key] = row.FundingRuleVersion
		sourcesByAllocation[key] = row.SnapshotSource
	}
	if _, err := models.PurchaseMemberBurdens.Delete(dm.Where(psql.And(
		models.PurchaseMemberBurdens.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.PurchaseMemberBurdens.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)),
	))).Exec(ctx, executor); err != nil {
		return err
	}
	for _, allocation := range allocations {
		key := allocationKey{itemID: allocation.ItemID, poolID: allocation.PoolID}
		weights := weightsByAllocation[key]
		if len(weights) == 0 {
			rule, err := currentPurchaseFundingRule(ctx, executor, householdUUID, allocation.PoolID)
			if err != nil {
				return fmt.Errorf("pool %s: %w", allocation.PoolID, err)
			}
			weights = rule.weights
			versionsByAllocation[key] = rule.version
			sourcesByAllocation[key] = statusPosted
		}
		parts, err := domain.Allocate(allocation.AmountYen, weights)
		if err != nil {
			return err
		}
		version := versionsByAllocation[key]
		snapshotSource := sourcesByAllocation[key]
		for _, part := range parts {
			memberID, err := gofrsuuid.FromString(part.PoolID)
			if err != nil {
				return fmt.Errorf("parse member id: %w", err)
			}
			itemUUID, poolUUID := allocation.ItemID, allocation.PoolID
			if _, err := models.PurchaseMemberBurdens.Insert(&models.PurchaseMemberBurdenSetter{
				HouseholdID: &householdUUID, PurchaseID: &purchaseUUID, ItemID: &itemUUID, PoolID: &poolUUID,
				MemberID: &memberID, AmountYen: &part.Amount, FundingRuleVersion: &version, FundingWeight: &part.Weight, SnapshotSource: &snapshotSource,
			}).Exec(ctx, executor); err != nil {
				return err
			}
		}
	}
	return nil
}

type purchaseFundingRuleSnapshot struct {
	version          int32
	weights          []domain.Weight
	settlementPolicy sql.Null[string]
}

func currentPurchaseFundingRule(ctx context.Context, executor bob.Executor, householdID, poolID gofrsuuid.UUID) (purchaseFundingRuleSnapshot, error) {
	pool, err := models.Pools.Query(sm.Where(psql.And(
		models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Pools.Columns.ID.EQ(psql.Arg(poolID)),
	))).One(ctx, executor)
	if err != nil {
		return purchaseFundingRuleSnapshot{}, err
	}
	if pool.FundingRuleVersion < 1 {
		return purchaseFundingRuleSnapshot{}, fmt.Errorf("%w: invalid funding rule version", ErrMissingPurchaseFundingSnapshot)
	}
	rule, err := models.PoolFundingRules.Query(sm.Where(psql.And(
		models.PoolFundingRules.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.PoolFundingRules.Columns.PoolID.EQ(psql.Arg(poolID)),
		models.PoolFundingRules.Columns.Version.EQ(psql.Arg(pool.FundingRuleVersion)),
	))).One(ctx, executor)
	if err != nil {
		return purchaseFundingRuleSnapshot{}, fmt.Errorf("%w: active funding rule is missing", ErrMissingPurchaseFundingSnapshot)
	}
	snapshot := purchaseFundingRuleSnapshot{version: pool.FundingRuleVersion, settlementPolicy: rule.SettlementPolicy}
	if pool.Kind == "PERSONAL" {
		if !rule.OwnerMemberID.Valid {
			return purchaseFundingRuleSnapshot{}, fmt.Errorf("%w: personal pool owner is missing", ErrMissingPurchaseFundingSnapshot)
		}
		snapshot.weights = []domain.Weight{{PoolID: rule.OwnerMemberID.V.String(), Value: 1}}
		return snapshot, nil
	}
	if pool.Kind != "SHARED" || !rule.SettlementPolicy.Valid || rule.SettlementPolicy.V == "" {
		return purchaseFundingRuleSnapshot{}, fmt.Errorf("%w: shared pool rule is incomplete", ErrMissingPurchaseFundingSnapshot)
	}
	shares, err := models.PoolFundingShares.Query(sm.Where(psql.And(
		models.PoolFundingShares.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.PoolFundingShares.Columns.PoolID.EQ(psql.Arg(poolID)),
		models.PoolFundingShares.Columns.Version.EQ(psql.Arg(pool.FundingRuleVersion)),
	)), sm.OrderBy(models.PoolFundingShares.Columns.MemberID)).All(ctx, executor)
	if err != nil {
		return purchaseFundingRuleSnapshot{}, err
	}
	if len(shares) == 0 {
		return purchaseFundingRuleSnapshot{}, fmt.Errorf("%w: shared pool has no member weights", ErrMissingPurchaseFundingSnapshot)
	}
	snapshot.weights = make([]domain.Weight, 0, len(shares))
	for _, share := range shares {
		snapshot.weights = append(snapshot.weights, domain.Weight{PoolID: share.MemberID.String(), Value: share.Weight})
	}
	if _, err := domain.Allocate(1, snapshot.weights); err != nil {
		return purchaseFundingRuleSnapshot{}, fmt.Errorf("%w: %w", ErrMissingPurchaseFundingSnapshot, err)
	}
	return snapshot, nil
}

func snapshotCorrectionMemberBurden(ctx context.Context, executor bob.Executor, householdID string, correctionID, purchaseID gofrsuuid.UUID, itemID sql.Null[gofrsuuid.UUID], shares []domain.AllocationAmount, delta int64) error {
	where := []bob.Expression{models.PurchaseMemberBurdens.Columns.PurchaseID.EQ(psql.Arg(purchaseID))}
	if itemID.Valid {
		where = append(where, models.PurchaseMemberBurdens.Columns.ItemID.EQ(psql.Arg(itemID.V)))
	}
	snapshots, err := models.PurchaseMemberBurdens.Query(sm.Where(psql.And(where...))).All(ctx, executor)
	if err != nil {
		return err
	}
	byPool := make(map[gofrsuuid.UUID]map[gofrsuuid.UUID]int64)
	versions := make(map[gofrsuuid.UUID]int32)
	for _, snapshot := range snapshots {
		if byPool[snapshot.PoolID] == nil {
			byPool[snapshot.PoolID] = make(map[gofrsuuid.UUID]int64)
		}
		byPool[snapshot.PoolID][snapshot.MemberID] = snapshot.FundingWeight
		versions[snapshot.PoolID] = snapshot.FundingRuleVersion
	}
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return fmt.Errorf("parse household id: %w", err)
	}
	for _, share := range shares {
		poolID, err := gofrsuuid.FromString(share.PoolID)
		if err != nil {
			return fmt.Errorf("parse pool id: %w", err)
		}
		memberWeightsByID := byPool[poolID]
		if len(memberWeightsByID) == 0 {
			return fmt.Errorf("%w: purchase %s pool %s has no member burden snapshot", ErrMissingPurchaseFundingSnapshot, purchaseID, poolID)
		}
		weights := make([]domain.Weight, 0, len(memberWeightsByID))
		for memberID, weight := range memberWeightsByID {
			weights = append(weights, domain.Weight{PoolID: memberID.String(), Value: weight})
		}
		parts, err := domain.Allocate(share.Amount, weights)
		if err != nil {
			return err
		}
		for _, part := range parts {
			if part.Amount == 0 {
				continue
			}
			memberID, err := gofrsuuid.FromString(part.PoolID)
			if err != nil {
				return fmt.Errorf("parse member id: %w", err)
			}
			amount := part.Amount
			if delta < 0 {
				amount = -amount
			}
			itemIDValue := itemID
			version := versions[poolID]
			if _, err := models.CorrectionMemberBurdens.Insert(&models.CorrectionMemberBurdenSetter{
				HouseholdID: &householdUUID, CorrectionID: &correctionID, ItemID: &itemIDValue,
				PoolID: &poolID, MemberID: &memberID, AmountDeltaYen: &amount,
				FundingRuleVersion: &version, FundingWeight: &part.Weight,
			}).Exec(ctx, executor); err != nil {
				return err
			}
		}
	}
	return nil
}
