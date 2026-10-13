package purchase

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const (
	draftStatus  = "DRAFT"
	postedStatus = "POSTED"
)

func (s *Service) post(ctx context.Context, input PostCommand, fingerprint [sha256.Size]byte) (PostResult, error) {
	if input.IdempotencyKey == "" || len(input.IdempotencyKey) > 200 {
		return PostResult{}, fmt.Errorf("%w: idempotency key is required", ErrBadInput)
	}
	purchaseID, err := parseUUID(input.PurchaseID, "purchase")
	if err != nil {
		return PostResult{}, err
	}
	actorID, err := parseUUID(input.ActorID, "actor")
	if err != nil {
		return PostResult{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return PostResult{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseID)))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return PostResult{}, ErrNotFound
	}
	if err != nil {
		return PostResult{}, err
	}
	householdID := purchase.HouseholdID
	member, err := models.HouseholdMembers.Query(sm.Where(psql.And(models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(actorID))))).Exists(ctx, executor)
	if err != nil {
		return PostResult{}, err
	}
	if !member {
		return PostResult{}, ErrNotFound
	}
	operation := "postPurchase"
	existing, err := models.IdempotencyKeys.Query(sm.Where(psql.And(models.IdempotencyKeys.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.IdempotencyKeys.Columns.Key.EQ(psql.Arg(input.IdempotencyKey)), models.IdempotencyKeys.Columns.Operation.EQ(psql.Arg(operation))))).One(ctx, executor)
	if err == nil {
		if err := validateIdempotencyFingerprint(existing, fingerprint); err != nil {
			return PostResult{}, err
		}
		if err := tx.Rollback(ctx); err != nil {
			return PostResult{}, err
		}
		return PostResult{PurchaseID: existing.ResourceID.V.String()}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return PostResult{}, err
	}
	period := accountingPeriod(purchase.OccurredAt)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PostResult{}, ErrNotFound
		}
		return PostResult{}, err
	}
	if err := ensureAccountingPeriod(ctx, executor, householdID, period); err != nil {
		return PostResult{}, err
	}
	periodRow, err := models.AccountingPeriods.Query(sm.Where(psql.And(models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.AccountingPeriods.Columns.Period.EQ(psql.Arg(period)))), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return PostResult{}, err
	}
	if periodRow.Status == "CLOSED" {
		return PostResult{}, ErrConflict
	}
	purchase, err = models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseID))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return PostResult{}, ErrNotFound
	}
	if err != nil {
		return PostResult{}, err
	}
	if accountingPeriod(purchase.OccurredAt) != period || purchase.Revision != input.ExpectedRevision || purchase.Status != draftStatus {
		return PostResult{}, ErrConflict
	}
	items, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseID)))).All(ctx, executor)
	if err != nil {
		return PostResult{}, err
	}
	var total int64
	for _, item := range items {
		var ok bool
		total, ok = checkedAdd(total, item.AmountYen)
		if !ok {
			return PostResult{}, fmt.Errorf("%w: purchase total is too large", ErrBadInput)
		}
	}
	if total <= 0 {
		return PostResult{}, fmt.Errorf("%w: purchase total must be greater than zero", ErrBadInput)
	}
	journalID, ledgerID := newUUID(), newUUID()
	source, occurredAt := "PURCHASE", purchase.OccurredAt
	if _, err := models.Journals.Insert(&models.JournalSetter{ID: &journalID, HouseholdID: &householdID, SourceKind: &source, SourceID: &purchaseID, IdempotencyKey: &input.IdempotencyKey, OccurredAt: &occurredAt, CreatedBy: &actorID}).Exec(ctx, executor); err != nil {
		return PostResult{}, err
	}
	allocations, err := models.ItemAllocations.Query(sm.Where(models.ItemAllocations.Columns.PurchaseID.EQ(psql.Arg(purchaseID)))).All(ctx, executor)
	if err != nil {
		return PostResult{}, err
	}
	if err := snapshotMemberBurden(ctx, executor, householdID, purchaseID, allocations); err != nil {
		return PostResult{}, err
	}
	if err := snapshotPaymentFunding(ctx, executor, householdID, purchase, total); err != nil {
		return PostResult{}, err
	}
	purchaseAllocations := make([]accounting.PurchaseAllocation, 0, len(allocations))
	for _, allocation := range allocations {
		purchaseAllocations = append(purchaseAllocations, accounting.PurchaseAllocation{PoolID: allocation.PoolID, AmountYen: allocation.AmountYen})
	}
	payments, err := purchasePayments(ctx, executor, householdID, purchaseID, total)
	if err != nil {
		return PostResult{}, err
	}
	purchasePayments := make([]accounting.PurchasePayment, 0, len(payments))
	for _, payment := range payments {
		purchasePayments = append(purchasePayments, accounting.PurchasePayment{AccountID: payment.AccountID, AmountYen: payment.AmountYen})
	}
	entries, err := accounting.BuildPurchaseEntries(purchaseAllocations, purchasePayments, total)
	if err != nil {
		return PostResult{}, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	if err := accounting.InsertEntries(ctx, executor, journalID, entries); err != nil {
		return PostResult{}, err
	}
	ledgerKind, description, status := "PURCHASE", purchase.Merchant, postedStatus
	noPool := sql.Null[gofrsuuid.UUID]{}
	if _, err := models.LedgerEntries.Insert(&models.LedgerEntrySetter{ID: &ledgerID, HouseholdID: &householdID, JournalID: &journalID, Kind: &ledgerKind, OccurredAt: &occurredAt, Description: &description, AmountYen: &total, PoolID: &noPool, SourcePurchaseID: &sql.Null[gofrsuuid.UUID]{V: purchaseID, Valid: true}}).Exec(ctx, executor); err != nil {
		return PostResult{}, err
	}
	revision, updatedAt := purchase.Revision+1, time.Now().UTC()
	if _, err := models.Purchases.Update((&models.PurchaseSetter{Status: &status, Revision: &revision, UpdatedAt: &updatedAt}).UpdateMod(), um.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseID)))).Exec(ctx, executor); err != nil {
		return PostResult{}, err
	}
	resource := sql.Null[gofrsuuid.UUID]{V: purchaseID, Valid: true}
	fingerprintValue := sql.Null[[]byte]{V: fingerprint[:], Valid: true}
	if _, err := models.IdempotencyKeys.Insert(&models.IdempotencyKeySetter{HouseholdID: &householdID, Key: &input.IdempotencyKey, Operation: &operation, ResourceID: &resource, RequestFingerprint: &fingerprintValue}).Exec(ctx, executor); err != nil {
		return PostResult{}, err
	}
	if err := writeEvent(ctx, tx, householdID, purchaseID, "purchase.posted", revision, []string{"status"}, actorID); err != nil {
		return PostResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PostResult{}, err
	}
	return PostResult{PurchaseID: purchaseID.String()}, nil
}

type payment struct {
	AccountID gofrsuuid.UUID
	AmountYen int64
}

func purchasePayments(ctx context.Context, executor bob.Executor, householdID, purchaseID gofrsuuid.UUID, total int64) ([]payment, error) {
	rows, err := models.PurchasePayments.Query(sm.Where(psql.And(models.PurchasePayments.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.PurchasePayments.Columns.PurchaseID.EQ(psql.Arg(purchaseID)))), sm.OrderBy(models.PurchasePayments.Columns.LineNo)).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	result := make([]payment, 0, len(rows))
	var paid int64
	for _, row := range rows {
		var ok bool
		paid, ok = checkedAdd(paid, row.AmountYen)
		if !ok {
			return nil, fmt.Errorf("%w: payment total is too large", ErrBadInput)
		}
		result = append(result, payment{AccountID: row.AccountID, AmountYen: row.AmountYen})
	}
	if len(result) == 0 {
		purchase, err := models.Purchases.Query(sm.Where(psql.And(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseID)), models.Purchases.Columns.HouseholdID.EQ(psql.Arg(householdID))))).One(ctx, executor)
		if err != nil {
			return nil, err
		}
		if purchase.PaymentAccountID.Valid {
			result = append(result, payment{AccountID: purchase.PaymentAccountID.V, AmountYen: total})
			paid = total
		}
	}
	if len(result) > 0 && paid != total {
		return nil, fmt.Errorf("%w: payment allocations must equal purchase total", ErrBadInput)
	}
	return result, nil
}

func snapshotMemberBurden(ctx context.Context, executor bob.Executor, householdID, purchaseID gofrsuuid.UUID, allocations models.ItemAllocationSlice) error {
	rules, err := fundingRules(ctx, executor, householdID)
	if err != nil {
		return err
	}
	for _, allocation := range allocations {
		rule, ok := rules[allocation.PoolID]
		if !ok || !rule.configured || rule.version < 1 {
			return fmt.Errorf("%w: pool %s", domain.ErrIncompleteFundingRule, allocation.PoolID)
		}
		weights, err := memberWeights(rule)
		if err != nil {
			return fmt.Errorf("pool %s: %w", allocation.PoolID, err)
		}
		if rule.kind == domain.FundingPoolShared && rule.settlement == "" {
			return fmt.Errorf("%w: pool %s has no settlement policy", domain.ErrIncompleteFundingRule, allocation.PoolID)
		}
		parts, err := domain.Allocate(allocation.AmountYen, weights)
		if err != nil {
			return err
		}
		for _, part := range parts {
			memberID, err := gofrsuuid.FromString(part.PoolID)
			if err != nil {
				return err
			}
			poolID, itemID := allocation.PoolID, allocation.ItemID
			version, source := rule.version, postedStatus
			if _, err := models.PurchaseMemberBurdens.Insert(&models.PurchaseMemberBurdenSetter{HouseholdID: &householdID, PurchaseID: &purchaseID, ItemID: &itemID, PoolID: &poolID, MemberID: &memberID, AmountYen: &part.Amount, FundingRuleVersion: &version, FundingWeight: &part.Weight, SnapshotSource: &source}).Exec(ctx, executor); err != nil {
				return err
			}
		}
	}
	return nil
}

type fundingRule struct {
	kind       string
	version    int32
	configured bool
	owner      string
	settlement string
	shares     []domain.MemberWeight
}

func fundingRules(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID) (map[gofrsuuid.UUID]fundingRule, error) {
	pools, err := models.Pools.Query(sm.Where(models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdID)))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	versionByPool := make(map[gofrsuuid.UUID]int32, len(pools))
	rules := make(map[gofrsuuid.UUID]fundingRule, len(pools))
	for _, pool := range pools {
		versionByPool[pool.ID] = pool.FundingRuleVersion
		rules[pool.ID] = fundingRule{kind: pool.Kind, version: pool.FundingRuleVersion}
	}
	storedRules, err := models.PoolFundingRules.Query(sm.Where(models.PoolFundingRules.Columns.HouseholdID.EQ(psql.Arg(householdID)))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	for _, stored := range storedRules {
		if versionByPool[stored.PoolID] != stored.Version {
			continue
		}
		rule := rules[stored.PoolID]
		rule.configured = true
		if stored.OwnerMemberID.Valid {
			rule.owner = stored.OwnerMemberID.V.String()
		}
		if stored.SettlementPolicy.Valid {
			rule.settlement = stored.SettlementPolicy.V
		}
		rules[stored.PoolID] = rule
	}
	shares, err := models.PoolFundingShares.Query(sm.Where(models.PoolFundingShares.Columns.HouseholdID.EQ(psql.Arg(householdID)))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(shares, func(left, right *models.PoolFundingShare) int {
		if c := strings.Compare(left.PoolID.String(), right.PoolID.String()); c != 0 {
			return c
		}
		if left.Version < right.Version {
			return -1
		}
		if left.Version > right.Version {
			return 1
		}
		return strings.Compare(left.MemberID.String(), right.MemberID.String())
	})
	for _, share := range shares {
		if versionByPool[share.PoolID] != share.Version {
			continue
		}
		rule := rules[share.PoolID]
		rule.shares = append(rule.shares, domain.MemberWeight{MemberID: share.MemberID.String(), Weight: share.Weight})
		rules[share.PoolID] = rule
	}
	return rules, nil
}

func memberWeights(rule fundingRule) ([]domain.Weight, error) {
	if rule.kind == domain.FundingPoolPersonal {
		if rule.owner == "" {
			return nil, domain.ErrIncompleteFundingRule
		}
		return []domain.Weight{{PoolID: rule.owner, Value: 1}}, nil
	}
	if rule.kind != domain.FundingPoolShared || len(rule.shares) == 0 {
		return nil, domain.ErrIncompleteFundingRule
	}
	weights := make([]domain.Weight, 0, len(rule.shares))
	for _, share := range rule.shares {
		weights = append(weights, domain.Weight{PoolID: share.MemberID, Value: share.Weight})
	}
	return weights, nil
}

func snapshotPaymentFunding(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, purchase *models.Purchase, total int64) error {
	payments, err := purchasePayments(ctx, executor, householdID, purchase.ID, total)
	if err != nil {
		return err
	}
	if len(payments) == 0 {
		return fmt.Errorf("%w: purchase %s has no payment account", ErrUnattributedPurchasePayment, purchase.ID)
	}
	storedPayments, err := models.PurchasePayments.Query(sm.Where(psql.And(
		models.PurchasePayments.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.PurchasePayments.Columns.PurchaseID.EQ(psql.Arg(purchase.ID)),
	))).All(ctx, executor)
	if err != nil {
		return err
	}
	if len(storedPayments) == 0 && purchase.PaymentAccountID.Valid {
		lineNo, accountID, amount := int32(1), purchase.PaymentAccountID.V, total
		if _, err := models.PurchasePayments.Insert(&models.PurchasePaymentSetter{
			HouseholdID: &householdID, PurchaseID: &purchase.ID, LineNo: &lineNo, AccountID: &accountID, AmountYen: &amount,
		}).Exec(ctx, executor); err != nil {
			return err
		}
	}
	rules, err := fundingRules(ctx, executor, householdID)
	if err != nil {
		return err
	}
	for index, payment := range payments {
		account, err := models.Accounts.Query(sm.Where(psql.And(models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.Accounts.Columns.ID.EQ(psql.Arg(payment.AccountID))))).One(ctx, executor)
		if err != nil {
			return err
		}
		if !account.PoolID.Valid {
			return fmt.Errorf("%w: purchase %s payment account %s has no pool", ErrUnattributedPurchasePayment, purchase.ID, account.ID)
		}
		rule, ok := rules[account.PoolID.V]
		if !ok || !rule.configured || rule.version < 1 {
			return fmt.Errorf("%w: pool %s", domain.ErrIncompleteFundingRule, account.PoolID.V)
		}
		weights, err := memberWeights(rule)
		if err != nil {
			return fmt.Errorf("pool %s: %w", account.PoolID.V, err)
		}
		if rule.kind == domain.FundingPoolShared && rule.settlement == "" {
			return fmt.Errorf("%w: pool %s has no settlement policy", domain.ErrIncompleteFundingRule, account.PoolID.V)
		}
		var settlement sql.Null[string]
		if rule.kind == domain.FundingPoolShared {
			settlement = sql.Null[string]{V: rule.settlement, Valid: true}
		}
		version, lineNo, source := rule.version, int32(index+1), postedStatus
		poolID := account.PoolID.V
		for _, weight := range weights {
			memberID, err := gofrsuuid.FromString(weight.PoolID)
			if err != nil {
				return err
			}
			fundingWeight := weight.Value
			if _, err := models.PurchasePaymentFundingSnapshots.Insert(&models.PurchasePaymentFundingSnapshotSetter{HouseholdID: &householdID, PurchaseID: &purchase.ID, PaymentLineNo: &lineNo, PoolID: &poolID, MemberID: &memberID, FundingRuleVersion: &version, FundingWeight: &fundingWeight, SettlementPolicy: &settlement, SnapshotSource: &source}).Exec(ctx, executor); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkedAdd(left, right int64) (int64, bool) {
	if right > 0 && left > math.MaxInt64-right || right < 0 && left < math.MinInt64-right {
		return 0, false
	}
	return left + right, true
}
