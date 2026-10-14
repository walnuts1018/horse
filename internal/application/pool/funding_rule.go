package pool

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"math"
	"slices"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/application/txcleanup"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type FundingShare struct {
	MemberID string
	Weight   int64
}

type SetFundingRuleCommand struct {
	ActorID          string
	HouseholdID      string
	PoolID           string
	ExpectedVersion  int
	Personal         bool
	Shared           bool
	PersonalOwnerID  string
	FundingShares    []FundingShare
	SettlementPolicy string
}

func (s *Service) SetFundingRule(ctx context.Context, command SetFundingRuleCommand) error {
	if command.Personal == command.Shared {
		return badFundingRule("pool funding configuration must select exactly one pool kind")
	}
	householdID, err := parseUUID(command.HouseholdID)
	if err != nil {
		return apperror.Wrap(apperror.KindBadInput, err)
	}
	poolID, err := parseUUID(command.PoolID)
	if err != nil {
		return apperror.Wrap(apperror.KindBadInput, err)
	}
	actorID, err := parseUUID(command.ActorID)
	if err != nil {
		return apperror.Wrap(apperror.KindNotFound, err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer txcleanup.Rollback(ctx, tx)
	executor := bobpgx.NewTx(tx, nil)
	household, err := models.Households.Query(
		sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))),
		sm.ForUpdate(),
	).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return apperror.ErrNotFound
	}
	if err != nil {
		return err
	}
	pool, err := models.Pools.Query(sm.Where(psql.And(
		models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Pools.Columns.ID.EQ(psql.Arg(poolID)),
	)), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return apperror.ErrNotFound
	}
	if err != nil {
		return err
	}
	if int(pool.FundingRuleVersion) != command.ExpectedVersion {
		return apperror.ErrConflict
	}

	shares := make(map[gofrsuuid.UUID]int64)
	var ownerID sql.Null[gofrsuuid.UUID]
	var settlementPolicy sql.Null[string]
	switch pool.Kind {
	case "PERSONAL":
		if !command.Personal || command.Shared || command.PersonalOwnerID == "" || len(command.FundingShares) > 0 || command.SettlementPolicy != "" {
			return badFundingRule("personal pools require only an owner member")
		}
		ownerUUID, err := gofrsuuid.FromString(command.PersonalOwnerID)
		if err != nil {
			return badFundingRule("invalid owner member id")
		}
		member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
			models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(ownerUUID)),
		))).Exists(ctx, executor)
		if err != nil {
			return err
		}
		if !member {
			return badFundingRule("owner must belong to the household")
		}
		ownerID = sql.Null[gofrsuuid.UUID]{V: ownerUUID, Valid: true}
	case "SHARED":
		if command.Personal || !command.Shared || command.PersonalOwnerID != "" || len(command.FundingShares) == 0 || !validSettlementPolicy(command.SettlementPolicy) {
			return badFundingRule("shared pools require funding shares and a settlement policy")
		}
		settlementPolicy = sql.Null[string]{V: command.SettlementPolicy, Valid: true}
		var total int64
		for _, share := range command.FundingShares {
			if share.MemberID == "" || share.Weight <= 0 {
				return badFundingRule("funding shares require positive weights")
			}
			memberID, err := gofrsuuid.FromString(share.MemberID)
			if err != nil {
				return badFundingRule("invalid funding share member id")
			}
			if _, exists := shares[memberID]; exists {
				return badFundingRule("duplicate funding share member")
			}
			if share.Weight > math.MaxInt64-total {
				return badFundingRule("funding share total is too large")
			}
			total += share.Weight
			shares[memberID] = share.Weight
		}
		for memberID := range shares {
			member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
				models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
				models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberID)),
			))).Exists(ctx, executor)
			if err != nil {
				return err
			}
			if !member {
				return badFundingRule("funding share member must belong to the household")
			}
		}
	default:
		return badFundingRule("invalid pool kind")
	}
	if pool.FundingRuleVersion == math.MaxInt32 {
		return badFundingRule("funding rule version is exhausted")
	}
	newVersion := pool.FundingRuleVersion + 1
	if _, err := models.PoolFundingRules.Insert(&models.PoolFundingRuleSetter{
		HouseholdID: &householdID, PoolID: &poolID, Version: &newVersion,
		OwnerMemberID: &ownerID, SettlementPolicy: &settlementPolicy, CreatedBy: &actorID,
	}).Exec(ctx, executor); err != nil {
		return err
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
			return err
		}
	}
	if _, err := models.Pools.Update((&models.PoolSetter{FundingRuleVersion: &newVersion}).UpdateMod(), um.Where(psql.And(
		models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Pools.Columns.ID.EQ(psql.Arg(poolID)),
	))).Exec(ctx, executor); err != nil {
		return err
	}
	if household.Revision == math.MaxInt64 {
		return badFundingRule("household revision is exhausted")
	}
	revision := household.Revision + 1
	updatedAt := time.Now()
	if _, err := models.Households.Update((&models.HouseholdSetter{Revision: &revision, UpdatedAt: &updatedAt}).UpdateMod(), um.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).Exec(ctx, executor); err != nil {
		return err
	}
	if err := accounting.RecordEvent(ctx, executor, householdID, poolID, "pool.funding_rule.updated", int64(newVersion), []string{"ownerMemberId", "fundingShares", "settlementPolicy"}, actorID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func badFundingRule(message string) error {
	return apperror.Wrap(apperror.KindBadInput, errors.New(message))
}

func validSettlementPolicy(policy string) bool {
	return policy == "BALANCE_TO_STANDARD" || policy == "REIMBURSE_PERSONAL"
}
