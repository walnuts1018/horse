package purchase

import (
	"context"
	"database/sql"
	"fmt"
	"slices"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/settlement"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func lockPurchaseAccountingPeriods(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, periods ...string) error {
	slices.Sort(periods)
	periods = slices.Compact(periods)
	for _, period := range periods {
		if err := ensureAccountingPeriod(ctx, executor, householdID, period); err != nil {
			return err
		}
		row, err := models.AccountingPeriods.Query(sm.Where(psql.And(
			models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.AccountingPeriods.Columns.Period.EQ(psql.Arg(period)),
		)), sm.ForUpdate()).One(ctx, executor)
		if err != nil {
			return err
		}
		if row.Status == correctionStatusClosed {
			return fmt.Errorf("%w: accounting period is closed", ErrConflict)
		}
	}
	return nil
}

func upsertProductAlias(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, variantID *string, merchantKey, alias, normalizedAlias string) (string, error) {
	variantUUID, err := parseUUID(*variantID, "product variant")
	if err != nil {
		return "", err
	}
	newID := newUUID()
	row, err := models.ProductAliases.Insert(
		&models.ProductAliasSetter{ID: &newID, HouseholdID: &householdID, VariantID: &variantUUID, MerchantKey: &merchantKey, AliasText: &alias, NormalizedAlias: &normalizedAlias},
		im.OnConflict("household_id", "merchant_key", "normalized_alias").DoUpdate(
			im.SetCol("variant_id").To(im.Excluded("variant_id")),
			im.SetCol("alias_text").To(im.Excluded("alias_text")),
			im.SetCol("revision").To(models.ProductAliases.Columns.Revision.Plus(psql.Arg(1))),
			im.SetCol("updated_at").To(psql.F("now")),
		),
	).One(ctx, executor)
	if err != nil {
		return "", err
	}
	return row.ID.String(), nil
}

func syncPostedPurchaseAccounting(ctx context.Context, executor bob.Executor, purchase *models.Purchase) error {
	purchaseUUID, householdID := purchase.ID, purchase.HouseholdID
	journal, err := models.Journals.Query(sm.Where(psql.And(
		models.Journals.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Journals.Columns.SourceKind.EQ(psql.Arg("PURCHASE")),
		models.Journals.Columns.SourceID.EQ(psql.Arg(purchaseUUID)),
	))).One(ctx, executor)
	if err != nil {
		return err
	}
	items, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, executor)
	if err != nil {
		return err
	}
	var total int64
	for _, item := range items {
		var ok bool
		total, ok = checkedAdd(total, item.AmountYen)
		if !ok {
			return fmt.Errorf("%w: purchase total is too large", ErrBadInput)
		}
	}
	if total <= 0 {
		return fmt.Errorf("%w: purchase total must be greater than zero", ErrBadInput)
	}
	if _, err := models.Journals.Update((&models.JournalSetter{OccurredAt: &purchase.OccurredAt}).UpdateMod(), um.Where(models.Journals.Columns.ID.EQ(psql.Arg(journal.ID)))).Exec(ctx, executor); err != nil {
		return err
	}
	if _, err := models.JournalEntries.Delete(dm.Where(models.JournalEntries.Columns.JournalID.EQ(psql.Arg(journal.ID)))).Exec(ctx, executor); err != nil {
		return err
	}
	allocations, err := models.ItemAllocations.Query(sm.Where(models.ItemAllocations.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, executor)
	if err != nil {
		return err
	}
	if err := refreshPurchaseMemberBurden(ctx, executor, householdID, purchaseUUID, allocations); err != nil {
		return err
	}
	purchaseAllocations := make([]accounting.PurchaseAllocation, 0, len(allocations))
	for _, allocation := range allocations {
		purchaseAllocations = append(purchaseAllocations, accounting.PurchaseAllocation{PoolID: allocation.PoolID, AmountYen: allocation.AmountYen})
	}
	payments, err := purchasePayments(ctx, executor, householdID, purchaseUUID, total)
	if err != nil {
		return err
	}
	purchasePayments := make([]accounting.PurchasePayment, 0, len(payments))
	for _, payment := range payments {
		purchasePayments = append(purchasePayments, accounting.PurchasePayment{AccountID: payment.AccountID, AmountYen: payment.AmountYen})
	}
	entries, err := accounting.BuildPurchaseEntries(purchaseAllocations, purchasePayments, total)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	if err := accounting.InsertEntries(ctx, executor, journal.ID, entries); err != nil {
		return err
	}
	ledgers, err := models.LedgerEntries.Query(sm.Where(psql.And(
		models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.LedgerEntries.Columns.SourcePurchaseID.EQ(psql.Arg(purchaseUUID)),
	))).All(ctx, executor)
	if err != nil {
		return err
	}
	for _, ledger := range ledgers {
		if _, err := models.LedgerEntries.Update((&models.LedgerEntrySetter{
			OccurredAt: &purchase.OccurredAt, Description: &purchase.Merchant, AmountYen: &total,
		}).UpdateMod(), um.Where(models.LedgerEntries.Columns.ID.EQ(psql.Arg(ledger.ID)))).Exec(ctx, executor); err != nil {
			return err
		}
	}
	return nil
}

func refreshPurchaseMemberBurden(ctx context.Context, executor bob.Executor, householdID, purchaseID gofrsuuid.UUID, allocations models.ItemAllocationSlice) error {
	previous, err := models.PurchaseMemberBurdens.Query(sm.Where(psql.And(
		models.PurchaseMemberBurdens.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.PurchaseMemberBurdens.Columns.PurchaseID.EQ(psql.Arg(purchaseID)),
	))).All(ctx, executor)
	if err != nil {
		return err
	}
	type allocationKey struct{ itemID, poolID gofrsuuid.UUID }
	weightsByAllocation := make(map[allocationKey][]domain.Weight)
	versionsByAllocation := make(map[allocationKey]int32)
	sourcesByAllocation := make(map[allocationKey]string)
	for _, row := range previous {
		key := allocationKey{itemID: row.ItemID, poolID: row.PoolID}
		weightsByAllocation[key] = append(weightsByAllocation[key], domain.Weight{PoolID: row.MemberID.String(), Value: row.FundingWeight})
		versionsByAllocation[key], sourcesByAllocation[key] = row.FundingRuleVersion, row.SnapshotSource
	}
	if _, err := models.PurchaseMemberBurdens.Delete(dm.Where(psql.And(
		models.PurchaseMemberBurdens.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.PurchaseMemberBurdens.Columns.PurchaseID.EQ(psql.Arg(purchaseID)),
	))).Exec(ctx, executor); err != nil {
		return err
	}
	for _, allocation := range allocations {
		key := allocationKey{itemID: allocation.ItemID, poolID: allocation.PoolID}
		weights := weightsByAllocation[key]
		if len(weights) == 0 {
			rule, err := currentPurchaseFundingRule(ctx, executor, householdID, allocation.PoolID)
			if err != nil {
				return fmt.Errorf("pool %s: %w", allocation.PoolID, err)
			}
			weights, versionsByAllocation[key], sourcesByAllocation[key] = rule.weights, rule.version, postedStatus
		}
		parts, err := domain.Allocate(allocation.AmountYen, weights)
		if err != nil {
			return err
		}
		version, source := versionsByAllocation[key], sourcesByAllocation[key]
		for _, part := range parts {
			memberID, err := gofrsuuid.FromString(part.PoolID)
			if err != nil {
				return fmt.Errorf("parse member id: %w", err)
			}
			itemID, poolID := allocation.ItemID, allocation.PoolID
			if _, err := models.PurchaseMemberBurdens.Insert(&models.PurchaseMemberBurdenSetter{
				HouseholdID: &householdID, PurchaseID: &purchaseID, ItemID: &itemID, PoolID: &poolID,
				MemberID: &memberID, AmountYen: &part.Amount, FundingRuleVersion: &version, FundingWeight: &part.Weight, SnapshotSource: &source,
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
		return purchaseFundingRuleSnapshot{}, fmt.Errorf("%w: invalid funding rule version", settlement.ErrMissingPurchaseFundingSnapshot)
	}
	rule, err := models.PoolFundingRules.Query(sm.Where(psql.And(
		models.PoolFundingRules.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.PoolFundingRules.Columns.PoolID.EQ(psql.Arg(poolID)),
		models.PoolFundingRules.Columns.Version.EQ(psql.Arg(pool.FundingRuleVersion)),
	))).One(ctx, executor)
	if err != nil {
		return purchaseFundingRuleSnapshot{}, fmt.Errorf("%w: active funding rule is missing", settlement.ErrMissingPurchaseFundingSnapshot)
	}
	snapshot := purchaseFundingRuleSnapshot{version: pool.FundingRuleVersion, settlementPolicy: rule.SettlementPolicy}
	if pool.Kind == "PERSONAL" {
		if !rule.OwnerMemberID.Valid {
			return purchaseFundingRuleSnapshot{}, fmt.Errorf("%w: personal pool owner is missing", settlement.ErrMissingPurchaseFundingSnapshot)
		}
		snapshot.weights = []domain.Weight{{PoolID: rule.OwnerMemberID.V.String(), Value: 1}}
		return snapshot, nil
	}
	if pool.Kind != "SHARED" || !rule.SettlementPolicy.Valid || rule.SettlementPolicy.V == "" {
		return purchaseFundingRuleSnapshot{}, fmt.Errorf("%w: shared pool rule is incomplete", settlement.ErrMissingPurchaseFundingSnapshot)
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
		return purchaseFundingRuleSnapshot{}, fmt.Errorf("%w: shared pool has no member weights", settlement.ErrMissingPurchaseFundingSnapshot)
	}
	snapshot.weights = make([]domain.Weight, 0, len(shares))
	for _, share := range shares {
		snapshot.weights = append(snapshot.weights, domain.Weight{PoolID: share.MemberID.String(), Value: share.Weight})
	}
	if _, err := domain.Allocate(1, snapshot.weights); err != nil {
		return purchaseFundingRuleSnapshot{}, fmt.Errorf("%w: %w", settlement.ErrMissingPurchaseFundingSnapshot, err)
	}
	return snapshot, nil
}

func replaceUpdatePayments(ctx context.Context, executor bob.Executor, purchase *models.Purchase, payments []Payment) error {
	householdID, purchaseID := purchase.HouseholdID, purchase.ID
	var oldSnapshots map[gofrsuuid.UUID][]*models.PurchasePaymentFundingSnapshot
	var oldSnapshotsByPool map[gofrsuuid.UUID][]*models.PurchasePaymentFundingSnapshot
	var oldAccounts map[gofrsuuid.UUID]struct{}
	if purchase.Status == postedStatus {
		oldPayments, err := models.PurchasePayments.Query(sm.Where(psql.And(models.PurchasePayments.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.PurchasePayments.Columns.PurchaseID.EQ(psql.Arg(purchaseID))))).All(ctx, executor)
		if err != nil {
			return err
		}
		accountByLine := make(map[int32]gofrsuuid.UUID, len(oldPayments))
		oldAccounts = make(map[gofrsuuid.UUID]struct{}, len(oldPayments))
		for _, oldPayment := range oldPayments {
			accountByLine[oldPayment.LineNo] = oldPayment.AccountID
			oldAccounts[oldPayment.AccountID] = struct{}{}
		}
		oldSnapshots = make(map[gofrsuuid.UUID][]*models.PurchasePaymentFundingSnapshot, len(oldPayments))
		oldSnapshotsByPool = make(map[gofrsuuid.UUID][]*models.PurchasePaymentFundingSnapshot)
		byLinePool := make(map[purchasePaymentSnapshotKey][]*models.PurchasePaymentFundingSnapshot)
		poolByLine := make(map[int32]gofrsuuid.UUID)
		snapshots, err := models.PurchasePaymentFundingSnapshots.Query(sm.Where(psql.And(models.PurchasePaymentFundingSnapshots.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.PurchasePaymentFundingSnapshots.Columns.PurchaseID.EQ(psql.Arg(purchaseID))))).All(ctx, executor)
		if err != nil {
			return err
		}
		for _, snapshot := range snapshots {
			accountID, ok := accountByLine[snapshot.PaymentLineNo]
			if !ok {
				return fmt.Errorf("%w: purchase %s payment line %d has no payment", settlement.ErrMissingPurchaseFundingSnapshot, purchaseID, snapshot.PaymentLineNo)
			}
			oldSnapshots[accountID] = append(oldSnapshots[accountID], snapshot)
			if poolID, found := poolByLine[snapshot.PaymentLineNo]; found && poolID != snapshot.PoolID {
				return fmt.Errorf("%w: purchase %s payment line %d has multiple pools", settlement.ErrMissingPurchaseFundingSnapshot, purchaseID, snapshot.PaymentLineNo)
			}
			poolByLine[snapshot.PaymentLineNo] = snapshot.PoolID
			key := purchasePaymentSnapshotKey{lineNo: snapshot.PaymentLineNo, poolID: snapshot.PoolID}
			byLinePool[key] = append(byLinePool[key], snapshot)
		}
		for key, snapshotSet := range byLinePool {
			if previousSet, ok := oldSnapshotsByPool[key.poolID]; ok {
				if !equalPurchasePaymentSnapshotSets(previousSet, snapshotSet) {
					return fmt.Errorf("%w: purchase %s has inconsistent snapshots for pool %s", settlement.ErrMissingPurchaseFundingSnapshot, purchaseID, key.poolID)
				}
				continue
			}
			oldSnapshotsByPool[key.poolID] = snapshotSet
		}
	}
	if _, err := models.PurchasePayments.Delete(dm.Where(psql.And(models.PurchasePayments.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.PurchasePayments.Columns.PurchaseID.EQ(psql.Arg(purchaseID))))).Exec(ctx, executor); err != nil {
		return err
	}
	for index, payment := range payments {
		accountID, err := parseUUID(payment.AccountID, "payment account")
		if err != nil {
			return err
		}
		lineNo, amount := int32(index+1), payment.AmountYen
		if _, err := models.PurchasePayments.Insert(&models.PurchasePaymentSetter{HouseholdID: &householdID, PurchaseID: &purchaseID, LineNo: &lineNo, AccountID: &accountID, AmountYen: &amount}).Exec(ctx, executor); err != nil {
			return err
		}
		if purchase.Status != postedStatus {
			continue
		}
		account, err := models.Accounts.Query(sm.Where(psql.And(models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.Accounts.Columns.ID.EQ(psql.Arg(accountID))))).One(ctx, executor)
		if err != nil {
			return err
		}
		if !account.PoolID.Valid {
			return fmt.Errorf("%w: purchase %s payment account %s has no pool", ErrUnattributedPurchasePayment, purchaseID, accountID)
		}
		existing := snapshotsForPool(oldSnapshots[accountID], account.PoolID.V)
		if len(existing) == 0 {
			existing = oldSnapshotsByPool[account.PoolID.V]
		}
		if len(existing) > 0 {
			for _, snapshot := range existing {
				if err := insertPaymentSnapshot(ctx, executor, householdID, purchaseID, lineNo, snapshot.PoolID, snapshot.MemberID, snapshot.FundingRuleVersion, snapshot.FundingWeight, snapshot.SettlementPolicy, snapshot.SnapshotSource); err != nil {
					return err
				}
			}
			continue
		}
		if _, wasPaidByAccount := oldAccounts[accountID]; wasPaidByAccount && len(oldSnapshots[accountID]) == 0 && len(oldSnapshotsByPool[account.PoolID.V]) == 0 {
			return fmt.Errorf("%w: purchase %s payment account %s has no existing snapshot", settlement.ErrMissingPurchaseFundingSnapshot, purchaseID, accountID)
		}
		fundingRule, err := currentPurchaseFundingRule(ctx, executor, householdID, account.PoolID.V)
		if err != nil {
			return fmt.Errorf("pool %s: %w", account.PoolID.V, err)
		}
		for _, weight := range fundingRule.weights {
			memberID, err := parseUUID(weight.PoolID, "funding member")
			if err != nil {
				return err
			}
			if err := insertPaymentSnapshot(ctx, executor, householdID, purchaseID, lineNo, account.PoolID.V, memberID, fundingRule.version, weight.Value, fundingRule.settlementPolicy, postedStatus); err != nil {
				return err
			}
		}
	}
	return nil
}

func insertPaymentSnapshot(ctx context.Context, executor bob.Executor, householdID, purchaseID gofrsuuid.UUID, lineNo int32, poolID, memberID gofrsuuid.UUID, version int32, weight int64, policy sql.Null[string], source string) error {
	_, err := models.PurchasePaymentFundingSnapshots.Insert(&models.PurchasePaymentFundingSnapshotSetter{
		HouseholdID: &householdID, PurchaseID: &purchaseID, PaymentLineNo: &lineNo, PoolID: &poolID, MemberID: &memberID,
		FundingRuleVersion: &version, FundingWeight: &weight, SettlementPolicy: &policy, SnapshotSource: &source,
	}).Exec(ctx, executor)
	return err
}

type purchasePaymentSnapshotKey struct {
	lineNo int32
	poolID gofrsuuid.UUID
}

func snapshotsForPool(snapshots []*models.PurchasePaymentFundingSnapshot, poolID gofrsuuid.UUID) []*models.PurchasePaymentFundingSnapshot {
	result := make([]*models.PurchasePaymentFundingSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.PoolID == poolID {
			result = append(result, snapshot)
		}
	}
	return result
}

func equalPurchasePaymentSnapshotSets(left, right []*models.PurchasePaymentFundingSnapshot) bool {
	if len(left) != len(right) || len(left) == 0 {
		return false
	}
	byMember := make(map[gofrsuuid.UUID]*models.PurchasePaymentFundingSnapshot, len(left))
	for _, snapshot := range left {
		byMember[snapshot.MemberID] = snapshot
	}
	for _, snapshot := range right {
		previous, ok := byMember[snapshot.MemberID]
		if !ok || previous.PoolID != snapshot.PoolID || previous.FundingRuleVersion != snapshot.FundingRuleVersion || previous.FundingWeight != snapshot.FundingWeight || previous.SettlementPolicy != snapshot.SettlementPolicy || previous.SnapshotSource != snapshot.SnapshotSource {
			return false
		}
	}
	return true
}
