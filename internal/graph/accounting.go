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
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type accountingDB interface {
	bob.Executor
}

var ErrUnattributedPurchasePayment = purchaseapp.ErrUnattributedPurchasePayment
var ErrMissingPurchaseFundingSnapshot = errors.New("posted purchase has no funding snapshot")

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
	shares := make(map[gofrsuuid.UUID]int64, len(input.FundingShares))
	var ownerID sql.Null[gofrsuuid.UUID]
	var settlementPolicy sql.Null[string]
	if pool.Kind == string(model.PoolKindPersonal) {
		if input.OwnerMemberID == nil || *input.OwnerMemberID == "" || input.SettlementPolicy != nil || len(input.FundingShares) != 0 {
			return nil, fmt.Errorf("%w: personal pools require one owner and no funding shares", ErrBadInput)
		}
		ownerUUID, err := gofrsuuid.FromString(*input.OwnerMemberID)
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
		if input.OwnerMemberID != nil || input.SettlementPolicy == nil || !input.SettlementPolicy.IsValid() || len(input.FundingShares) == 0 {
			return nil, fmt.Errorf("%w: shared pools require funding shares and a settlement policy", ErrBadInput)
		}
		settlementPolicy = sql.Null[string]{V: string(*input.SettlementPolicy), Valid: true}
		var total int64
		for _, share := range input.FundingShares {
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
	if err := writeEvent(ctx, tx, input.HouseholdID, "pool.funding_rule.updated", input.PoolID, int64(newVersion), []string{"ownerMemberId", "fundingShares", "settlementPolicy"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	householdResult, err := loadHousehold(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	for _, resultPool := range householdResult.Pools {
		if resultPool.ID == input.PoolID {
			return resultPool, nil
		}
	}
	return nil, ErrNotFound
}

func poolFundingRules(ctx context.Context, db accountingDB, householdID string) (map[string]domain.PoolFundingRule, error) {
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	pools, err := models.Pools.Query(sm.Where(models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdUUID)))).All(ctx, db)
	if err != nil {
		return nil, err
	}
	versionByPool := make(map[gofrsuuid.UUID]int32, len(pools))
	rules := make(map[string]domain.PoolFundingRule, len(pools))
	for _, pool := range pools {
		id := pool.ID.String()
		versionByPool[pool.ID] = pool.FundingRuleVersion
		rules[id] = domain.PoolFundingRule{PoolID: id, Kind: pool.Kind, Version: int(pool.FundingRuleVersion)}
	}
	fundingRules, err := models.PoolFundingRules.Query(sm.Where(models.PoolFundingRules.Columns.HouseholdID.EQ(psql.Arg(householdUUID)))).All(ctx, db)
	if err != nil {
		return nil, err
	}
	for _, storedRule := range fundingRules {
		if versionByPool[storedRule.PoolID] != storedRule.Version {
			continue
		}
		rule := rules[storedRule.PoolID.String()]
		if storedRule.OwnerMemberID.Valid {
			rule.OwnerMemberID = storedRule.OwnerMemberID.V.String()
		}
		if storedRule.SettlementPolicy.Valid {
			rule.SettlementPolicy = storedRule.SettlementPolicy.V
		}
		rules[rule.PoolID] = rule
	}
	fundingShares, err := models.PoolFundingShares.Query(sm.Where(models.PoolFundingShares.Columns.HouseholdID.EQ(psql.Arg(householdUUID)))).All(ctx, db)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(fundingShares, func(left, right *models.PoolFundingShare) int {
		if comparison := cmp.Compare(left.PoolID.String(), right.PoolID.String()); comparison != 0 {
			return comparison
		}
		if comparison := cmp.Compare(left.Version, right.Version); comparison != 0 {
			return comparison
		}
		return cmp.Compare(left.MemberID.String(), right.MemberID.String())
	})
	for _, share := range fundingShares {
		if versionByPool[share.PoolID] != share.Version {
			continue
		}
		rule := rules[share.PoolID.String()]
		rule.FundingShares = append(rule.FundingShares, domain.MemberWeight{MemberID: share.MemberID.String(), Weight: share.Weight})
		rules[rule.PoolID] = rule
	}
	return rules, nil
}

func calculatePeriodAccounting(ctx context.Context, db accountingDB, householdID, period string) ([]domain.SettlementAmount, error) {
	start, err := parseAccountingPeriod(period)
	if err != nil {
		return nil, fmt.Errorf("%w: period must use YYYY-MM", ErrBadInput)
	}
	end := start.AddDate(0, 1, 0)
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	householdMembers, err := models.HouseholdMembers.Query(
		sm.Where(models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdUUID))),
		sm.OrderBy(models.HouseholdMembers.Columns.JoinedAt),
		sm.OrderBy(models.HouseholdMembers.Columns.MemberID),
	).All(ctx, db)
	if err != nil {
		return nil, err
	}
	if err := householdMembers.LoadMember(ctx, db); err != nil {
		return nil, err
	}
	inputs := make([]domain.SettlementInput, 0, len(householdMembers))
	memberIndex := make(map[string]int, len(householdMembers))
	for _, householdMember := range householdMembers {
		memberID := householdMember.MemberID.String()
		memberIndex[memberID] = len(inputs)
		inputs = append(inputs, domain.SettlementInput{MemberID: memberID, MemberName: householdMember.R.Member.DisplayName})
	}
	if err := addFundingAmounts(ctx, db, householdUUID, period, start, end, inputs, memberIndex); err != nil {
		return nil, err
	}
	expenses, rules, err := loadFundingExpenses(ctx, db, householdUUID, start, end)
	if err != nil {
		return nil, err
	}
	changes, err := domain.CalculateExpenseAdjustments(expenses, rules)
	if err != nil {
		return nil, err
	}
	for _, change := range changes {
		index, found := memberIndex[change.MemberID]
		if !found {
			return nil, domain.ErrInvalidFundingExpense
		}
		input := &inputs[index]
		input.MonthlyAdjustmentYen, found = checkedAdd(input.MonthlyAdjustmentYen, change.MonthlyAdjustmentYen)
		if !found {
			return nil, domain.ErrInvalidFundingExpense
		}
		input.AdvanceCreditYen, found = checkedAdd(input.AdvanceCreditYen, change.AdvanceCreditYen)
		if !found {
			return nil, domain.ErrInvalidFundingExpense
		}
	}
	adjustments, err := models.SettlementAdjustments.Query(sm.Where(psql.And(
		models.SettlementAdjustments.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.SettlementAdjustments.Columns.Period.EQ(psql.Arg(period)),
	))).All(ctx, db)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(adjustments, func(left, right *models.SettlementAdjustment) int {
		return cmp.Compare(left.MemberID.String(), right.MemberID.String())
	})
	for _, adjustment := range adjustments {
		index, found := memberIndex[adjustment.MemberID.String()]
		if !found {
			return nil, domain.ErrInvalidSettlement
		}
		inputs[index].MonthlyAdjustmentYen, found = checkedAdd(inputs[index].MonthlyAdjustmentYen, adjustment.AmountYen)
		if !found {
			return nil, domain.ErrInvalidSettlement
		}
	}
	return domain.CalculateSettlement(inputs)
}

func addFundingAmounts(ctx context.Context, db accountingDB, householdUUID gofrsuuid.UUID, period string, start, end time.Time, inputs []domain.SettlementInput, memberIndex map[string]int) error {
	type fundingKey struct{ memberID, poolID string }
	type funding struct{ scheduled, paid int64 }
	amounts := make(map[fundingKey]funding)
	obligations, err := models.FundingObligations.Query(sm.Where(psql.And(
		models.FundingObligations.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.FundingObligations.Columns.Period.EQ(psql.Arg(period)),
	))).All(ctx, db)
	if err != nil {
		return err
	}
	for _, obligation := range obligations {
		key := fundingKey{memberID: obligation.MemberID.String(), poolID: obligation.PoolID.String()}
		amounts[key] = funding{scheduled: obligation.AmountYen, paid: obligation.PaidYen}
	}
	schedules, err := models.FundingSchedules.Query(sm.Where(psql.And(
		models.FundingSchedules.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.FundingSchedules.Columns.Cadence.EQ(psql.Arg("MONTHLY")),
		psql.Or(
			models.FundingSchedules.Columns.Active.EQ(psql.Arg(true)),
			models.FundingSchedules.Columns.EndsOn.IsNotNull(),
		),
		models.FundingSchedules.Columns.StartsOn.LT(psql.Arg(end)),
		psql.Or(
			models.FundingSchedules.Columns.EndsOn.IsNull(),
			models.FundingSchedules.Columns.EndsOn.GTE(psql.Arg(start)),
		),
	))).All(ctx, db)
	if err != nil {
		return err
	}
	slices.SortFunc(schedules, func(left, right *models.FundingSchedule) int {
		if comparison := cmp.Compare(left.MemberID.String(), right.MemberID.String()); comparison != 0 {
			return comparison
		}
		return cmp.Compare(left.PoolID.String(), right.PoolID.String())
	})
	scheduleAmounts := make(map[fundingKey]int64)
	for _, schedule := range schedules {
		key := fundingKey{memberID: schedule.MemberID.String(), poolID: schedule.PoolID.String()}
		var ok bool
		scheduleAmounts[key], ok = checkedAdd(scheduleAmounts[key], schedule.AmountYen)
		if !ok {
			return domain.ErrInvalidSettlement
		}
	}
	scheduleKeys := make([]fundingKey, 0, len(scheduleAmounts))
	for key := range scheduleAmounts {
		scheduleKeys = append(scheduleKeys, key)
	}
	slices.SortFunc(scheduleKeys, func(left, right fundingKey) int {
		if comparison := cmp.Compare(left.memberID, right.memberID); comparison != 0 {
			return comparison
		}
		return cmp.Compare(left.poolID, right.poolID)
	})
	for _, key := range scheduleKeys {
		value := amounts[key]
		if scheduleAmounts[key] > value.scheduled {
			value.scheduled = scheduleAmounts[key]
		}
		amounts[key] = value
	}
	keys := make([]fundingKey, 0, len(amounts))
	for key := range amounts {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(left, right fundingKey) int {
		if comparison := cmp.Compare(left.memberID, right.memberID); comparison != 0 {
			return comparison
		}
		return cmp.Compare(left.poolID, right.poolID)
	})
	for _, key := range keys {
		amount := amounts[key]
		index, exists := memberIndex[key.memberID]
		if !exists {
			return domain.ErrInvalidSettlement
		}
		input := &inputs[index]
		input.ScheduledYen, exists = checkedAdd(input.ScheduledYen, amount.scheduled)
		if !exists {
			return domain.ErrInvalidSettlement
		}
		input.PaidYen, exists = checkedAdd(input.PaidYen, amount.paid)
		if !exists {
			return domain.ErrInvalidSettlement
		}
	}
	return nil
}

func loadFundingExpenses(ctx context.Context, db accountingDB, householdUUID gofrsuuid.UUID, start, end time.Time) ([]domain.FundingExpense, map[string]domain.PoolFundingRule, error) {
	const queryBatchSize = 500
	uuidArgs := func(ids []gofrsuuid.UUID) []any {
		args := make([]any, len(ids))
		for index, id := range ids {
			args[index] = id
		}
		return args
	}
	type fundingRuleVersion struct {
		poolID  gofrsuuid.UUID
		version int32
	}
	type historicalFundingRule struct {
		row    *models.PoolFundingRule
		shares map[gofrsuuid.UUID]int64
	}
	purchases, err := models.Purchases.Query(sm.Where(psql.And(
		models.Purchases.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.Purchases.Columns.Status.In(psql.Arg(string(model.PurchaseStatusPosted)), psql.Arg(string(model.PurchaseStatusClosed))),
		models.Purchases.Columns.OccurredAt.GTE(psql.Arg(start)),
		models.Purchases.Columns.OccurredAt.LT(psql.Arg(end)),
	))).All(ctx, db)
	if err != nil {
		return nil, nil, err
	}
	slices.SortFunc(purchases, func(left, right *models.Purchase) int {
		return cmp.Compare(left.ID.String(), right.ID.String())
	})
	purchaseIDs := make([]gofrsuuid.UUID, 0, len(purchases))
	for _, purchase := range purchases {
		purchaseIDs = append(purchaseIDs, purchase.ID)
	}
	itemsByPurchase := make(map[gofrsuuid.UUID]models.PurchaseItemSlice)
	paymentsByPurchase := make(map[gofrsuuid.UUID]models.PurchasePaymentSlice)
	paymentSnapshotsByPurchase := make(map[gofrsuuid.UUID]models.PurchasePaymentFundingSnapshotSlice)
	burdensByPurchase := make(map[gofrsuuid.UUID]models.PurchaseMemberBurdenSlice)
	burdensByItem := make(map[gofrsuuid.UUID]map[gofrsuuid.UUID]models.PurchaseMemberBurdenSlice)
	for start := 0; start < len(purchaseIDs); start += queryBatchSize {
		end := min(start+queryBatchSize, len(purchaseIDs))
		batch := purchaseIDs[start:end]
		items, err := models.PurchaseItems.Query(
			sm.Where(psql.And(
				models.PurchaseItems.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
				models.PurchaseItems.Columns.PurchaseID.In(psql.Arg(uuidArgs(batch)...)),
			)),
			sm.OrderBy(models.PurchaseItems.Columns.PurchaseID),
			sm.OrderBy(models.PurchaseItems.Columns.ID),
		).All(ctx, db)
		if err != nil {
			return nil, nil, err
		}
		for _, item := range items {
			itemsByPurchase[item.PurchaseID] = append(itemsByPurchase[item.PurchaseID], item)
		}
		payments, err := models.PurchasePayments.Query(
			sm.Where(psql.And(
				models.PurchasePayments.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
				models.PurchasePayments.Columns.PurchaseID.In(psql.Arg(uuidArgs(batch)...)),
			)),
			sm.OrderBy(models.PurchasePayments.Columns.PurchaseID),
			sm.OrderBy(models.PurchasePayments.Columns.LineNo),
		).All(ctx, db)
		if err != nil {
			return nil, nil, err
		}
		for _, payment := range payments {
			paymentsByPurchase[payment.PurchaseID] = append(paymentsByPurchase[payment.PurchaseID], payment)
		}
		paymentSnapshots, err := models.PurchasePaymentFundingSnapshots.Query(
			sm.Where(psql.And(
				models.PurchasePaymentFundingSnapshots.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
				models.PurchasePaymentFundingSnapshots.Columns.PurchaseID.In(psql.Arg(uuidArgs(batch)...)),
			)),
			sm.OrderBy(models.PurchasePaymentFundingSnapshots.Columns.PurchaseID),
			sm.OrderBy(models.PurchasePaymentFundingSnapshots.Columns.PaymentLineNo),
			sm.OrderBy(models.PurchasePaymentFundingSnapshots.Columns.MemberID),
		).All(ctx, db)
		if err != nil {
			return nil, nil, err
		}
		for _, snapshot := range paymentSnapshots {
			paymentSnapshotsByPurchase[snapshot.PurchaseID] = append(paymentSnapshotsByPurchase[snapshot.PurchaseID], snapshot)
		}
		burdens, err := models.PurchaseMemberBurdens.Query(
			sm.Where(psql.And(
				models.PurchaseMemberBurdens.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
				models.PurchaseMemberBurdens.Columns.PurchaseID.In(psql.Arg(uuidArgs(batch)...)),
			)),
			sm.OrderBy(models.PurchaseMemberBurdens.Columns.PurchaseID),
			sm.OrderBy(models.PurchaseMemberBurdens.Columns.ItemID),
			sm.OrderBy(models.PurchaseMemberBurdens.Columns.PoolID),
			sm.OrderBy(models.PurchaseMemberBurdens.Columns.MemberID),
		).All(ctx, db)
		if err != nil {
			return nil, nil, err
		}
		for _, burden := range burdens {
			burdensByPurchase[burden.PurchaseID] = append(burdensByPurchase[burden.PurchaseID], burden)
			byItem := burdensByItem[burden.PurchaseID]
			if byItem == nil {
				byItem = make(map[gofrsuuid.UUID]models.PurchaseMemberBurdenSlice)
				burdensByItem[burden.PurchaseID] = byItem
			}
			byItem[burden.ItemID] = append(byItem[burden.ItemID], burden)
		}
	}
	accountsByID := make(map[gofrsuuid.UUID]*models.Account)
	accountsToLoad := make(map[gofrsuuid.UUID]struct{})
	for _, purchase := range purchases {
		if len(itemsByPurchase[purchase.ID]) == 0 {
			continue
		}
		snapshotsByLine := make(map[int32]struct{})
		for _, snapshot := range paymentSnapshotsByPurchase[purchase.ID] {
			snapshotsByLine[snapshot.PaymentLineNo] = struct{}{}
		}
		for _, payment := range paymentsByPurchase[purchase.ID] {
			if _, found := snapshotsByLine[payment.LineNo]; !found {
				accountsToLoad[payment.AccountID] = struct{}{}
			}
		}
	}
	accountIDs := make([]gofrsuuid.UUID, 0, len(accountsToLoad))
	for accountID := range accountsToLoad {
		accountIDs = append(accountIDs, accountID)
	}
	slices.SortFunc(accountIDs, func(left, right gofrsuuid.UUID) int { return cmp.Compare(left.String(), right.String()) })
	for start := 0; start < len(accountIDs); start += queryBatchSize {
		end := min(start+queryBatchSize, len(accountIDs))
		accounts, err := models.Accounts.Query(sm.Where(psql.And(
			models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			models.Accounts.Columns.ID.In(psql.Arg(uuidArgs(accountIDs[start:end])...)),
		))).All(ctx, db)
		if err != nil {
			return nil, nil, err
		}
		for _, account := range accounts {
			accountsByID[account.ID] = account
		}
	}
	historicalKeysSet := make(map[fundingRuleVersion]struct{})
	for _, burdens := range burdensByPurchase {
		for _, burden := range burdens {
			historicalKeysSet[fundingRuleVersion{poolID: burden.PoolID, version: burden.FundingRuleVersion}] = struct{}{}
		}
	}
	historicalKeys := make([]fundingRuleVersion, 0, len(historicalKeysSet))
	for key := range historicalKeysSet {
		historicalKeys = append(historicalKeys, key)
	}
	slices.SortFunc(historicalKeys, func(left, right fundingRuleVersion) int {
		if comparison := cmp.Compare(left.poolID.String(), right.poolID.String()); comparison != 0 {
			return comparison
		}
		return cmp.Compare(left.version, right.version)
	})
	historicalRulesByKey := make(map[fundingRuleVersion]historicalFundingRule, len(historicalKeys))
	for start := 0; start < len(historicalKeys); start += queryBatchSize {
		end := min(start+queryBatchSize, len(historicalKeys))
		conditions := make([]bob.Expression, 0, end-start)
		for _, key := range historicalKeys[start:end] {
			conditions = append(conditions, psql.And(
				models.PoolFundingRules.Columns.PoolID.EQ(psql.Arg(key.poolID)),
				models.PoolFundingRules.Columns.Version.EQ(psql.Arg(key.version)),
			))
		}
		storedRules, err := models.PoolFundingRules.Query(sm.Where(psql.And(
			models.PoolFundingRules.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			psql.Or(conditions...),
		))).All(ctx, db)
		if err != nil {
			return nil, nil, err
		}
		for _, storedRule := range storedRules {
			key := fundingRuleVersion{poolID: storedRule.PoolID, version: storedRule.Version}
			historicalRulesByKey[key] = historicalFundingRule{row: storedRule}
		}
	}
	sharedHistoricalKeys := make([]fundingRuleVersion, 0)
	for _, key := range historicalKeys {
		storedRule, found := historicalRulesByKey[key]
		if found && !storedRule.row.OwnerMemberID.Valid {
			sharedHistoricalKeys = append(sharedHistoricalKeys, key)
		}
	}
	for start := 0; start < len(sharedHistoricalKeys); start += queryBatchSize {
		end := min(start+queryBatchSize, len(sharedHistoricalKeys))
		conditions := make([]bob.Expression, 0, end-start)
		for _, key := range sharedHistoricalKeys[start:end] {
			conditions = append(conditions, psql.And(
				models.PoolFundingShares.Columns.PoolID.EQ(psql.Arg(key.poolID)),
				models.PoolFundingShares.Columns.Version.EQ(psql.Arg(key.version)),
			))
		}
		shares, err := models.PoolFundingShares.Query(
			sm.Where(psql.And(
				models.PoolFundingShares.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
				psql.Or(conditions...),
			)),
			sm.OrderBy(models.PoolFundingShares.Columns.PoolID),
			sm.OrderBy(models.PoolFundingShares.Columns.Version),
			sm.OrderBy(models.PoolFundingShares.Columns.MemberID),
		).All(ctx, db)
		if err != nil {
			return nil, nil, err
		}
		for _, share := range shares {
			key := fundingRuleVersion{poolID: share.PoolID, version: share.Version}
			historicalRule := historicalRulesByKey[key]
			if historicalRule.shares == nil {
				historicalRule.shares = make(map[gofrsuuid.UUID]int64)
			}
			historicalRule.shares[share.MemberID] = share.Weight
			historicalRulesByKey[key] = historicalRule
		}
	}
	expenses := make([]domain.FundingExpense, 0, len(purchases))
	rules := make(map[string]domain.PoolFundingRule)
	for _, purchase := range purchases {
		items := itemsByPurchase[purchase.ID]
		if len(items) == 0 {
			continue
		}
		payments := paymentsByPurchase[purchase.ID]
		paymentSnapshots := paymentSnapshotsByPurchase[purchase.ID]
		if len(payments) == 0 {
			return nil, nil, fmt.Errorf("%w: purchase %s payment", ErrMissingPurchaseFundingSnapshot, purchase.ID)
		}
		slices.SortFunc(paymentSnapshots, func(left, right *models.PurchasePaymentFundingSnapshot) int {
			if comparison := cmp.Compare(left.PaymentLineNo, right.PaymentLineNo); comparison != 0 {
				return comparison
			}
			return cmp.Compare(left.MemberID.String(), right.MemberID.String())
		})
		snapshotsByLine := make(map[int32]models.PurchasePaymentFundingSnapshotSlice)
		for _, snapshot := range paymentSnapshots {
			snapshotsByLine[snapshot.PaymentLineNo] = append(snapshotsByLine[snapshot.PaymentLineNo], snapshot)
		}
		for _, payment := range payments {
			if len(snapshotsByLine[payment.LineNo]) != 0 {
				continue
			}
			account, found := accountsByID[payment.AccountID]
			if !found {
				return nil, nil, sql.ErrNoRows
			}
			if !account.PoolID.Valid {
				return nil, nil, fmt.Errorf("%w: purchase %s payment account %s", ErrUnattributedPurchasePayment, purchase.ID, account.ID)
			}
		}
		storedPayments := make([]domain.FundingPayment, 0, len(payments))
		for _, payment := range payments {
			paymentRuleSnapshots := snapshotsByLine[payment.LineNo]
			if len(paymentRuleSnapshots) == 0 {
				return nil, nil, fmt.Errorf("%w: purchase %s payment line %d", ErrMissingPurchaseFundingSnapshot, purchase.ID, payment.LineNo)
			}
			alias := fmt.Sprintf("%s:payment:%d", purchase.ID, payment.LineNo)
			var rule domain.PoolFundingRule
			first := paymentRuleSnapshots[0]
			rule.PoolID = alias
			rule.Version = int(first.FundingRuleVersion)
			for _, snapshot := range paymentRuleSnapshots {
				if snapshot.PoolID != first.PoolID || snapshot.FundingRuleVersion != first.FundingRuleVersion || snapshot.SettlementPolicy != first.SettlementPolicy || snapshot.SnapshotSource != first.SnapshotSource {
					return nil, nil, fmt.Errorf("%w: purchase %s payment line %d is inconsistent", ErrMissingPurchaseFundingSnapshot, purchase.ID, payment.LineNo)
				}
				rule.FundingShares = append(rule.FundingShares, domain.MemberWeight{MemberID: snapshot.MemberID.String(), Weight: snapshot.FundingWeight})
			}
			if first.SettlementPolicy.Valid {
				rule.Kind = domain.FundingPoolShared
				rule.SettlementPolicy = first.SettlementPolicy.V
			} else {
				if len(paymentRuleSnapshots) != 1 || first.FundingWeight != 1 {
					return nil, nil, fmt.Errorf("%w: purchase %s personal payment line %d is inconsistent", ErrMissingPurchaseFundingSnapshot, purchase.ID, payment.LineNo)
				}
				rule.Kind = domain.FundingPoolPersonal
				rule.OwnerMemberID = first.MemberID.String()
				rule.FundingShares = nil
			}
			rules[alias] = rule
			storedPayments = append(storedPayments, domain.FundingPayment{ID: alias, PoolID: alias, Amount: payment.AmountYen})
			delete(snapshotsByLine, payment.LineNo)
		}
		if len(snapshotsByLine) != 0 {
			return nil, nil, fmt.Errorf("%w: purchase %s has orphaned payment snapshots", ErrMissingPurchaseFundingSnapshot, purchase.ID)
		}
		fundingItems := make([]domain.FundingExpenseItem, 0, len(items))
		for _, item := range items {
			burdens := burdensByItem[purchase.ID][item.ID]
			if len(burdens) == 0 {
				return nil, nil, fmt.Errorf("%w: purchase %s item %s", ErrMissingPurchaseFundingSnapshot, purchase.ID, item.ID)
			}
			allocationItems := make([]domain.PoolAmount, 0, len(burdens))
			var burdenTotal int64
			for _, burden := range burdens {
				versionKey := fundingRuleVersion{poolID: burden.PoolID, version: burden.FundingRuleVersion}
				historicalRule, exists := historicalRulesByKey[versionKey]
				if !exists {
					return nil, nil, sql.ErrNoRows
				}
				alias := fmt.Sprintf("%s:allocation:%s:%s:%s", purchase.ID, item.ID, burden.PoolID, burden.MemberID)
				rule := domain.PoolFundingRule{PoolID: alias}
				if historicalRule.row.OwnerMemberID.Valid {
					if historicalRule.row.OwnerMemberID.V != burden.MemberID || burden.FundingWeight != 1 {
						return nil, nil, fmt.Errorf("%w: purchase %s item %s has an invalid personal burden", ErrMissingPurchaseFundingSnapshot, purchase.ID, item.ID)
					}
					rule.Kind = domain.FundingPoolPersonal
					rule.OwnerMemberID = burden.MemberID.String()
				} else {
					weight, ok := historicalRule.shares[burden.MemberID]
					if !ok || weight != burden.FundingWeight {
						return nil, nil, fmt.Errorf("%w: purchase %s item %s has an invalid shared burden", ErrMissingPurchaseFundingSnapshot, purchase.ID, item.ID)
					}
					rule.Kind = domain.FundingPoolShared
					rule.FundingShares = []domain.MemberWeight{{MemberID: burden.MemberID.String(), Weight: 1}}
				}
				rules[alias] = rule
				allocationItems = append(allocationItems, domain.PoolAmount{PoolID: alias, Amount: burden.AmountYen})
				var ok bool
				burdenTotal, ok = checkedAdd(burdenTotal, burden.AmountYen)
				if !ok {
					return nil, nil, domain.ErrInvalidFundingExpense
				}
			}
			if burdenTotal != item.AmountYen {
				return nil, nil, fmt.Errorf("%w: purchase %s item %s total does not match", ErrMissingPurchaseFundingSnapshot, purchase.ID, item.ID)
			}
			fundingItems = append(fundingItems, domain.FundingExpenseItem{Amount: item.AmountYen, Allocations: allocationItems})
		}
		split, err := domain.SplitFundingExpense(fundingItems, storedPayments)
		if err != nil {
			return nil, nil, fmt.Errorf("purchase %s: %w", purchase.ID, err)
		}
		expenses = append(expenses, split...)
	}
	return expenses, rules, nil
}

func checkedAdd(left, right int64) (int64, bool) {
	if right > 0 && left > math.MaxInt64-right || right < 0 && left < math.MinInt64-right {
		return 0, false
	}
	return left + right, true
}

func calculateSettlementPreview(ctx context.Context, db accountingDB, householdID, period string) ([]domain.SettlementAmount, error) {
	return calculateSettlementPreviewWithRules(ctx, db, householdID, period)
}

func calculateSettlementPreviewWithRules(ctx context.Context, db accountingDB, householdID, period string) ([]domain.SettlementAmount, error) {
	target, err := parseAccountingPeriod(period)
	if err != nil {
		return nil, fmt.Errorf("%w: period must use YYYY-MM", ErrBadInput)
	}
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	runs, err := models.SettlementRuns.Query(sm.Where(psql.And(
		models.SettlementRuns.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.SettlementRuns.Columns.Status.EQ(psql.Arg("CLOSED")),
		models.SettlementRuns.Columns.Period.LT(psql.Arg(period)),
	))).All(ctx, db)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(runs, func(left, right *models.SettlementRun) int {
		if comparison := cmp.Compare(right.Period, left.Period); comparison != 0 {
			return comparison
		}
		return cmp.Compare(right.Revision, left.Revision)
	})
	var closedPeriod *string
	carry := make(map[string]int64)
	if len(runs) > 0 {
		latestRun := runs[0]
		periodCopy := latestRun.Period
		closedPeriod = &periodCopy
		lines, err := models.SettlementLines.Query(sm.Where(models.SettlementLines.Columns.SettlementRunID.EQ(psql.Arg(latestRun.ID)))).All(ctx, db)
		if err != nil {
			return nil, err
		}
		for _, line := range lines {
			carry[line.MemberID.String()] = line.AmountDueYen
		}
	}
	firstOpen := target
	if closedPeriod != nil {
		closedMonth, err := parseAccountingPeriod(*closedPeriod)
		if err != nil {
			return nil, err
		}
		firstOpen = closedMonth.AddDate(0, 1, 0)
	} else {
		earliest := ""
		obligations, err := models.FundingObligations.Query(sm.Where(models.FundingObligations.Columns.HouseholdID.EQ(psql.Arg(householdUUID)))).All(ctx, db)
		if err != nil {
			return nil, err
		}
		for _, obligation := range obligations {
			earliest = earlierPeriod(earliest, obligation.Period)
		}
		schedules, err := models.FundingSchedules.Query(sm.Where(models.FundingSchedules.Columns.HouseholdID.EQ(psql.Arg(householdUUID)))).All(ctx, db)
		if err != nil {
			return nil, err
		}
		for _, schedule := range schedules {
			earliest = earlierPeriod(earliest, accountingPeriod(schedule.StartsOn))
		}
		purchases, err := models.Purchases.Query(sm.Where(psql.And(
			models.Purchases.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			models.Purchases.Columns.Status.In(psql.Arg(string(model.PurchaseStatusPosted)), psql.Arg(string(model.PurchaseStatusClosed))),
		))).All(ctx, db)
		if err != nil {
			return nil, err
		}
		for _, purchase := range purchases {
			earliest = earlierPeriod(earliest, accountingPeriod(purchase.OccurredAt))
		}
		if earliest != "" {
			firstOpen, err = parseAccountingPeriod(earliest)
			if err != nil {
				return nil, err
			}
		}
	}
	if firstOpen.After(target) {
		firstOpen = target
	}
	for month := firstOpen; month.Before(target); month = month.AddDate(0, 1, 0) {
		monthInputs, err := calculatePeriodAccounting(ctx, db, householdID, month.Format("2006-01"))
		if err != nil {
			return nil, err
		}
		for _, line := range monthInputs {
			value, ok := checkedAdd(carry[line.MemberID], int64(line.AmountDueYen))
			if !ok {
				return nil, domain.ErrInvalidSettlement
			}
			carry[line.MemberID] = value
		}
	}
	current, err := calculatePeriodAccounting(ctx, db, householdID, period)
	if err != nil {
		return nil, err
	}
	for index := range current {
		current[index].CarryYen = carry[current[index].MemberID]
		amountDue, ok := checkedAdd(current[index].AmountDueYen, current[index].CarryYen)
		if !ok {
			return nil, domain.ErrInvalidSettlement
		}
		current[index].AmountDueYen = amountDue
	}
	return current, nil
}

func earlierPeriod(current, candidate string) string {
	if current == "" || candidate < current {
		return candidate
	}
	return current
}
