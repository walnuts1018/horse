package graph

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/graph/model"
)

type accountingDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

var accountingLocation = time.FixedZone("JST", 9*60*60)

func accountingPeriod(value time.Time) string { return value.In(accountingLocation).Format("2006-01") }

func parseAccountingPeriod(value string) (time.Time, error) {
	return time.ParseInLocation("2006-01", value, accountingLocation)
}

func (r *mutationResolver) setPoolFundingRule(ctx context.Context, input model.SetPoolFundingRuleInput) (*model.Pool, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var lockedHouseholdID string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM households WHERE id=$1 FOR UPDATE`, input.HouseholdID).Scan(&lockedHouseholdID); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	var kind string
	var version int
	err = tx.QueryRow(ctx, `SELECT kind, funding_rule_version FROM pools WHERE household_id=$1 AND id=$2 FOR UPDATE`, input.HouseholdID, input.PoolID).Scan(&kind, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if version != input.ExpectedVersion {
		return nil, ErrConflict
	}
	var owner any
	var policy any
	shares := make(map[string]int64, len(input.FundingShares))
	if kind == string(model.PoolKindPersonal) {
		if input.OwnerMemberID == nil || *input.OwnerMemberID == "" || input.SettlementPolicy != nil || len(input.FundingShares) != 0 {
			return nil, fmt.Errorf("%w: personal pools require one owner and no funding shares", ErrBadInput)
		}
		var member bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM household_members WHERE household_id=$1 AND member_id=$2)`, input.HouseholdID, *input.OwnerMemberID).Scan(&member); err != nil {
			return nil, err
		}
		if !member {
			return nil, fmt.Errorf("%w: owner must belong to the household", ErrBadInput)
		}
		owner = *input.OwnerMemberID
	} else if kind == string(model.PoolKindShared) {
		if input.OwnerMemberID != nil || input.SettlementPolicy == nil || !input.SettlementPolicy.IsValid() || len(input.FundingShares) == 0 {
			return nil, fmt.Errorf("%w: shared pools require funding shares and a settlement policy", ErrBadInput)
		}
		policy = string(*input.SettlementPolicy)
		var total int64
		for _, share := range input.FundingShares {
			if share == nil || share.MemberID == "" || share.Weight <= 0 {
				return nil, fmt.Errorf("%w: funding shares require positive weights", ErrBadInput)
			}
			if _, exists := shares[share.MemberID]; exists {
				return nil, fmt.Errorf("%w: duplicate funding share member", ErrBadInput)
			}
			if int64(share.Weight) > math.MaxInt64-total {
				return nil, fmt.Errorf("%w: funding share total is too large", ErrBadInput)
			}
			total += int64(share.Weight)
			shares[share.MemberID] = int64(share.Weight)
		}
		for memberID := range shares {
			var member bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM household_members WHERE household_id=$1 AND member_id=$2)`, input.HouseholdID, memberID).Scan(&member); err != nil {
				return nil, err
			}
			if !member {
				return nil, fmt.Errorf("%w: funding share member must belong to the household", ErrBadInput)
			}
		}
	} else {
		return nil, fmt.Errorf("%w: invalid pool kind", ErrBadInput)
	}
	newVersion := version + 1
	if _, err := tx.Exec(ctx, `INSERT INTO pool_funding_rules(household_id,pool_id,version,owner_member_id,settlement_policy,created_by) VALUES($1,$2,$3,$4,$5,$6)`, input.HouseholdID, input.PoolID, newVersion, owner, policy, user.MemberID); err != nil {
		return nil, err
	}
	for memberID, weight := range shares {
		if _, err := tx.Exec(ctx, `INSERT INTO pool_funding_shares(household_id,pool_id,version,member_id,weight) VALUES($1,$2,$3,$4,$5)`, input.HouseholdID, input.PoolID, newVersion, memberID, weight); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE pools SET funding_rule_version=$3 WHERE household_id=$1 AND id=$2`, input.HouseholdID, input.PoolID, newVersion); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE households SET revision=revision+1,updated_at=now() WHERE id=$1`, input.HouseholdID); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "pool.funding_rule.updated", input.PoolID, int64(newVersion), []string{"ownerMemberId", "fundingShares", "settlementPolicy"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	household, err := loadHousehold(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	for _, pool := range household.Pools {
		if pool.ID == input.PoolID {
			return pool, nil
		}
	}
	return nil, ErrNotFound
}

func poolFundingRules(ctx context.Context, db interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, householdID string) (map[string]domain.PoolFundingRule, error) {
	rows, err := db.Query(ctx, `SELECT p.id::text,p.kind,p.funding_rule_version,r.owner_member_id::text,r.settlement_policy FROM pools p LEFT JOIN pool_funding_rules r ON r.household_id=p.household_id AND r.pool_id=p.id AND r.version=p.funding_rule_version WHERE p.household_id=$1 ORDER BY p.id`, householdID)
	if err != nil {
		return nil, err
	}
	rules := make(map[string]domain.PoolFundingRule)
	for rows.Next() {
		var rule domain.PoolFundingRule
		var owner, policy *string
		if err := rows.Scan(&rule.PoolID, &rule.Kind, &rule.Version, &owner, &policy); err != nil {
			rows.Close()
			return nil, err
		}
		if owner != nil {
			rule.OwnerMemberID = *owner
		}
		if policy != nil {
			rule.SettlementPolicy = *policy
		}
		rules[rule.PoolID] = rule
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	shares, err := db.Query(ctx, `SELECT s.pool_id::text,s.member_id::text,s.weight FROM pool_funding_shares s JOIN pools p ON p.household_id=s.household_id AND p.id=s.pool_id AND p.funding_rule_version=s.version WHERE s.household_id=$1 ORDER BY s.pool_id,s.member_id`, householdID)
	if err != nil {
		return nil, err
	}
	defer shares.Close()
	for shares.Next() {
		var poolID, memberID string
		var weight int64
		if err := shares.Scan(&poolID, &memberID, &weight); err != nil {
			return nil, err
		}
		rule := rules[poolID]
		rule.FundingShares = append(rule.FundingShares, domain.MemberWeight{MemberID: memberID, Weight: weight})
		rules[poolID] = rule
	}
	return rules, shares.Err()
}

func calculatePeriodAccounting(ctx context.Context, db accountingDB, householdID, period string, rules map[string]domain.PoolFundingRule) ([]domain.SettlementAmount, error) {
	start, err := parseAccountingPeriod(period)
	if err != nil {
		return nil, fmt.Errorf("%w: period must use YYYY-MM", ErrBadInput)
	}
	end := start.AddDate(0, 1, 0)
	inputs := make([]domain.SettlementInput, 0)
	memberIndex := make(map[string]int)
	rows, err := db.Query(ctx, `SELECT m.id::text,m.display_name FROM household_members hm JOIN members m ON m.id=hm.member_id WHERE hm.household_id=$1 ORDER BY hm.joined_at,m.id`, householdID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var input domain.SettlementInput
		if err := rows.Scan(&input.MemberID, &input.MemberName); err != nil {
			rows.Close()
			return nil, err
		}
		memberIndex[input.MemberID] = len(inputs)
		inputs = append(inputs, input)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := addFundingAmounts(ctx, db, householdID, period, start, end, inputs, memberIndex); err != nil {
		return nil, err
	}
	expenses, err := loadFundingExpenses(ctx, db, householdID, start, end)
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
	return domain.CalculateSettlement(inputs)
}

func addFundingAmounts(ctx context.Context, db accountingDB, householdID, period string, start, end time.Time, inputs []domain.SettlementInput, memberIndex map[string]int) error {
	type fundingKey struct{ memberID, poolID string }
	type funding struct{ scheduled, paid int64 }
	amounts := make(map[fundingKey]funding)
	rows, err := db.Query(ctx, `SELECT member_id::text,pool_id::text,amount_yen,paid_yen FROM funding_obligations WHERE household_id=$1 AND period=$2`, householdID, period)
	if err != nil {
		return err
	}
	for rows.Next() {
		var key fundingKey
		var value funding
		if err := rows.Scan(&key.memberID, &key.poolID, &value.scheduled, &value.paid); err != nil {
			rows.Close()
			return err
		}
		amounts[key] = value
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	schedules, err := db.Query(ctx, `SELECT member_id::text,pool_id::text,sum(amount_yen)::bigint FROM funding_schedules WHERE household_id=$1 AND (active OR ends_on IS NOT NULL) AND cadence='MONTHLY' AND starts_on<$3 AND (ends_on IS NULL OR ends_on >= $2) GROUP BY member_id,pool_id`, householdID, start, end)
	if err != nil {
		return err
	}
	for schedules.Next() {
		var key fundingKey
		var scheduled int64
		if err := schedules.Scan(&key.memberID, &key.poolID, &scheduled); err != nil {
			schedules.Close()
			return err
		}
		value := amounts[key]
		if scheduled > value.scheduled {
			value.scheduled = scheduled
		}
		amounts[key] = value
	}
	schedules.Close()
	if err := schedules.Err(); err != nil {
		return err
	}
	for key, amount := range amounts {
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

func loadFundingExpenses(ctx context.Context, db accountingDB, householdID string, start, end time.Time) ([]domain.FundingExpense, error) {
	rows, err := db.Query(ctx, `SELECT p.id::text,a.pool_id::text,i.id::text,i.amount_yen,ia.pool_id::text,ia.amount_yen FROM purchases p JOIN accounts a ON a.household_id=p.household_id AND a.id=p.payment_account_id JOIN purchase_items i ON i.household_id=p.household_id AND i.purchase_id=p.id JOIN item_allocations ia ON ia.household_id=i.household_id AND ia.purchase_id=i.purchase_id AND ia.item_id=i.id WHERE p.household_id=$1 AND a.pool_id IS NOT NULL AND p.status IN ('POSTED','CLOSED') AND p.occurred_at >= $2 AND p.occurred_at < $3 ORDER BY p.id,i.id,ia.pool_id`, householdID, start, end)
	if err != nil {
		return nil, err
	}
	type itemKey struct{ purchaseID, itemID string }
	expenseByID := make(map[string]*domain.FundingExpense)
	itemsByID := make(map[itemKey]*domain.FundingExpenseItem)
	for rows.Next() {
		var purchaseID, paymentPoolID, itemID, allocationPoolID string
		var amount, allocated int64
		if err := rows.Scan(&purchaseID, &paymentPoolID, &itemID, &amount, &allocationPoolID, &allocated); err != nil {
			rows.Close()
			return nil, err
		}
		expense := expenseByID[purchaseID]
		if expense == nil {
			expense = &domain.FundingExpense{PaymentPoolID: paymentPoolID}
			expenseByID[purchaseID] = expense
		}
		key := itemKey{purchaseID: purchaseID, itemID: itemID}
		item := itemsByID[key]
		if item == nil {
			item = &domain.FundingExpenseItem{Amount: amount}
			itemsByID[key] = item
		}
		item.Allocations = append(item.Allocations, domain.PoolAmount{PoolID: allocationPoolID, Amount: allocated})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	purchaseIDs := make([]string, 0, len(expenseByID))
	itemIDsByPurchase := make(map[string][]string)
	for purchaseID := range expenseByID {
		purchaseIDs = append(purchaseIDs, purchaseID)
	}
	slices.Sort(purchaseIDs)
	for key := range itemsByID {
		itemIDsByPurchase[key.purchaseID] = append(itemIDsByPurchase[key.purchaseID], key.itemID)
	}
	for purchaseID, itemIDs := range itemIDsByPurchase {
		slices.Sort(itemIDs)
		for _, itemID := range itemIDs {
			expenseByID[purchaseID].Items = append(expenseByID[purchaseID].Items, *itemsByID[itemKey{purchaseID: purchaseID, itemID: itemID}])
		}
	}
	result := make([]domain.FundingExpense, 0, len(expenseByID))
	for _, purchaseID := range purchaseIDs {
		result = append(result, *expenseByID[purchaseID])
	}
	return result, nil
}

func checkedAdd(left, right int64) (int64, bool) {
	if right > 0 && left > math.MaxInt64-right || right < 0 && left < math.MinInt64-right {
		return 0, false
	}
	return left + right, true
}

func calculateSettlementPreview(ctx context.Context, db accountingDB, householdID, period string) ([]domain.SettlementAmount, error) {
	rules, err := poolFundingRules(ctx, db, householdID)
	if err != nil {
		return nil, err
	}
	return calculateSettlementPreviewWithRules(ctx, db, householdID, period, rules)
}

func calculateSettlementPreviewWithRules(ctx context.Context, db accountingDB, householdID, period string, rules map[string]domain.PoolFundingRule) ([]domain.SettlementAmount, error) {
	target, err := parseAccountingPeriod(period)
	if err != nil {
		return nil, fmt.Errorf("%w: period must use YYYY-MM", ErrBadInput)
	}
	var closedPeriod *string
	closedRows, err := db.Query(ctx, `SELECT sr.period,sl.member_id::text,sl.amount_due_yen FROM settlement_runs sr JOIN settlement_lines sl ON sl.settlement_run_id=sr.id WHERE sr.household_id=$1 AND sr.status='CLOSED' AND sr.period=(SELECT max(period) FROM settlement_runs WHERE household_id=$1 AND status='CLOSED' AND period < $2) AND sr.revision=(SELECT max(revision) FROM settlement_runs WHERE household_id=$1 AND status='CLOSED' AND period=sr.period)`, householdID, period)
	if err != nil {
		return nil, err
	}
	carry := make(map[string]int64)
	for closedRows.Next() {
		var month, memberID string
		var amount int64
		if err := closedRows.Scan(&month, &memberID, &amount); err != nil {
			closedRows.Close()
			return nil, err
		}
		if closedPeriod == nil || month > *closedPeriod {
			monthCopy := month
			closedPeriod = &monthCopy
		}
		carry[memberID] = amount
	}
	closedRows.Close()
	if err := closedRows.Err(); err != nil {
		return nil, err
	}
	firstOpen := target
	if closedPeriod != nil {
		closedMonth, err := parseAccountingPeriod(*closedPeriod)
		if err != nil {
			return nil, err
		}
		firstOpen = closedMonth.AddDate(0, 1, 0)
	} else {
		var earliest *string
		err := db.QueryRow(ctx, `SELECT min(month) FROM (SELECT period AS month FROM funding_obligations WHERE household_id=$1 UNION SELECT to_char(starts_on,'YYYY-MM') FROM funding_schedules WHERE household_id=$1 UNION SELECT to_char(occurred_at AT TIME ZONE 'Asia/Tokyo','YYYY-MM') FROM purchases WHERE household_id=$1 AND status IN ('POSTED','CLOSED')) activity`, householdID).Scan(&earliest)
		if err != nil {
			return nil, err
		}
		if earliest != nil {
			firstOpen, err = parseAccountingPeriod(*earliest)
			if err != nil {
				return nil, err
			}
		}
	}
	if firstOpen.After(target) {
		firstOpen = target
	}
	for month := firstOpen; month.Before(target); month = month.AddDate(0, 1, 0) {
		monthInputs, err := calculatePeriodAccounting(ctx, db, householdID, month.Format("2006-01"), rules)
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
	current, err := calculatePeriodAccounting(ctx, db, householdID, period, rules)
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
