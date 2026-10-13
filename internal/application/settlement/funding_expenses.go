package settlement

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"slices"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func loadFundingExpenses(ctx context.Context, db bob.Executor, householdUUID gofrsuuid.UUID, start, end time.Time) (map[string][]domain.FundingExpense, map[string]domain.PoolFundingRule, error) {
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
		models.Purchases.Columns.Status.In(psql.Arg(string("POSTED")), psql.Arg(string("CLOSED"))),
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
	expensesByPeriod := make(map[string][]domain.FundingExpense)
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
		period := accounting.PeriodFor(purchase.OccurredAt)
		expensesByPeriod[period] = append(expensesByPeriod[period], split...)
	}
	return expensesByPeriod, rules, nil
}
