package graph

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"uuid"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func principal(ctx context.Context) (auth.Principal, error) {
	return auth.PrincipalFromContext(ctx)
}

func requireMember(ctx context.Context, db *pgxpool.Pool, householdID string) (auth.Principal, error) {
	user, err := principal(ctx)
	if err != nil {
		return auth.Principal{}, err
	}
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return auth.Principal{}, fmt.Errorf("parse household id: %w", err)
	}
	memberUUID, err := gofrsuuid.FromString(user.MemberID)
	if err != nil {
		return auth.Principal{}, fmt.Errorf("parse member id: %w", err)
	}
	member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberUUID)),
	))).Exists(ctx, bobpgx.NewPool(db))
	if err != nil {
		return auth.Principal{}, err
	}
	if !member {
		return auth.Principal{}, ErrNotFound
	}
	return user, nil
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: timestamp must use RFC3339", ErrBadInput)
	}
	return parsed.UTC(), nil
}

func timeString(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func newID() string { return uuid.NewV7().String() }

func graphLong(value int64) model.Long { return model.Long(value) }

func loadHousehold(ctx context.Context, db *pgxpool.Pool, householdID string) (*model.Household, error) {
	id, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	household, err := models.FindHousehold(ctx, bobpgx.NewPool(db), id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	result := model.Household{
		ID: household.ID.String(), Name: household.Name,
		Revision: int(household.Revision), CreatedAt: timeString(household.CreatedAt), UpdatedAt: timeString(household.UpdatedAt),
	}
	allocationVersion := household.DefaultAllocationVersion
	executor := bobpgx.NewPool(db)
	result.Members = make([]*model.Member, 0)
	householdMembers, err := models.HouseholdMembers.Query(
		sm.Where(models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(id))),
		sm.OrderBy(models.HouseholdMembers.Columns.JoinedAt),
		sm.OrderBy(models.HouseholdMembers.Columns.MemberID),
	).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	if err := householdMembers.LoadMember(ctx, executor); err != nil {
		return nil, err
	}
	for _, householdMember := range householdMembers {
		result.Members = append(result.Members, &model.Member{
			ID: householdMember.MemberID.String(), DisplayName: householdMember.R.Member.DisplayName, Role: householdMember.Role,
		})
	}
	pools, err := models.Pools.Query(
		sm.Where(models.Pools.Columns.HouseholdID.EQ(psql.Arg(id))),
		sm.OrderBy(models.Pools.Columns.CreatedAt),
		sm.OrderBy(models.Pools.Columns.ID),
	).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	result.Pools = make([]*model.Pool, 0)
	defaults, err := models.AllocationDefaults.Query(sm.Where(psql.And(
		models.AllocationDefaults.Columns.HouseholdID.EQ(psql.Arg(id)),
		models.AllocationDefaults.Columns.Version.EQ(psql.Arg(allocationVersion)),
	))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	defaultWeightByPool := make(map[gofrsuuid.UUID]int64, len(defaults))
	for _, allocation := range defaults {
		defaultWeightByPool[allocation.PoolID] = allocation.Weight
	}
	poolByID := make(map[gofrsuuid.UUID]*model.Pool, len(pools))
	for _, pool := range pools {
		value := &model.Pool{
			ID: pool.ID.String(), Name: pool.Name, Kind: model.PoolKind(pool.Kind),
			DefaultShareWeight: graphLong(defaultWeightByPool[pool.ID]),
			FundingRuleVersion: int(pool.FundingRuleVersion), FundingShares: make([]*model.PoolFundingShare, 0),
		}
		result.Pools = append(result.Pools, value)
		poolByID[pool.ID] = value
	}
	fundingRules, err := models.PoolFundingRules.Query(sm.Where(models.PoolFundingRules.Columns.HouseholdID.EQ(psql.Arg(id)))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	fundingShares, err := models.PoolFundingShares.Query(sm.Where(models.PoolFundingShares.Columns.HouseholdID.EQ(psql.Arg(id)))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	memberNameByID := make(map[gofrsuuid.UUID]string, len(householdMembers))
	for _, householdMember := range householdMembers {
		memberNameByID[householdMember.MemberID] = householdMember.R.Member.DisplayName
	}
	activeRules := make(map[gofrsuuid.UUID]*models.PoolFundingRule)
	for _, rule := range fundingRules {
		if pool := poolByID[rule.PoolID]; pool != nil && int64(pool.FundingRuleVersion) == int64(rule.Version) {
			activeRules[rule.PoolID] = rule
		}
	}
	for _, rule := range activeRules {
		pool := poolByID[rule.PoolID]
		if rule.OwnerMemberID.Valid {
			owner := rule.OwnerMemberID.V.String()
			pool.OwnerMemberID = &owner
		}
		if rule.SettlementPolicy.Valid {
			policy := model.SharedPoolSettlementPolicy(rule.SettlementPolicy.V)
			pool.SettlementPolicy = &policy
		}
	}
	slices.SortFunc(fundingShares, func(left, right *models.PoolFundingShare) int {
		if comparison := cmp.Compare(left.PoolID.String(), right.PoolID.String()); comparison != 0 {
			return comparison
		}
		return cmp.Compare(left.MemberID.String(), right.MemberID.String())
	})
	for _, share := range fundingShares {
		rule := activeRules[share.PoolID]
		pool := poolByID[share.PoolID]
		if rule == nil || pool == nil || share.Version != rule.Version {
			continue
		}
		pool.FundingShares = append(pool.FundingShares, &model.PoolFundingShare{
			MemberID: share.MemberID.String(), MemberName: memberNameByID[share.MemberID], Weight: graphLong(share.Weight),
		})
	}
	result.Accounts = make([]*model.Account, 0)
	accounts, err := models.Accounts.Query(
		sm.Where(models.Accounts.Columns.HouseholdID.EQ(psql.Arg(id))),
		sm.OrderBy(models.Accounts.Columns.CreatedAt),
		sm.OrderBy(models.Accounts.Columns.ID),
	).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	for _, storedAccount := range accounts {
		account := &model.Account{ID: storedAccount.ID.String(), Name: storedAccount.Name, Kind: model.AccountKind(storedAccount.Kind)}
		if storedAccount.PoolID.Valid {
			poolID := storedAccount.PoolID.V.String()
			account.PoolID = &poolID
		}
		result.Accounts = append(result.Accounts, account)
	}
	result.Categories = make([]*model.Category, 0)
	categories, err := models.Categories.Query(
		sm.Where(models.Categories.Columns.HouseholdID.EQ(psql.Arg(id))),
		sm.OrderBy(models.Categories.Columns.Name),
		sm.OrderBy(models.Categories.Columns.ID),
	).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	for _, storedCategory := range categories {
		category := &model.Category{ID: storedCategory.ID.String(), Name: storedCategory.Name, Archived: storedCategory.Archived}
		if storedCategory.ParentID.Valid {
			parentID := storedCategory.ParentID.V.String()
			category.ParentID = &parentID
		}
		result.Categories = append(result.Categories, category)
	}
	result.DefaultAllocation = make([]*model.Allocation, 0)
	for _, allocation := range defaults {
		pool := poolByID[allocation.PoolID]
		if pool == nil {
			continue
		}
		poolName := pool.Name
		result.DefaultAllocation = append(result.DefaultAllocation, &model.Allocation{
			PoolID: pool.ID, PoolName: &poolName, Weight: graphLong(allocation.Weight),
		})
	}
	return &result, nil
}

func loadPurchase(ctx context.Context, db *pgxpool.Pool, purchaseID string) (*model.Purchase, error) {
	id, err := gofrsuuid.FromString(purchaseID)
	if err != nil {
		return nil, ErrNotFound
	}
	executor := bobpgx.NewPool(db)
	purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(id)))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	result := &model.Purchase{
		ID: purchase.ID.String(), HouseholdID: purchase.HouseholdID.String(),
		OccurredAt: timeString(purchase.OccurredAt), Merchant: purchase.Merchant,
		Origin: model.PurchaseOrigin(purchase.Origin), SourceKinds: make([]model.SourceKind, 0),
		Revision: int(purchase.Revision), Status: model.PurchaseStatus(purchase.Status), Currency: "JPY",
		CreatedAt: timeString(purchase.CreatedAt), UpdatedAt: timeString(purchase.UpdatedAt),
		Items: make([]*model.PurchaseItem, 0), PaymentAllocations: make([]*model.PurchasePayment, 0),
		Corrections: make([]*model.PurchaseCorrection, 0),
	}
	evidence, err := models.EvidenceLinks.Query(sm.Where(psql.And(
		models.EvidenceLinks.Columns.HouseholdID.EQ(psql.Arg(purchase.HouseholdID)),
		models.EvidenceLinks.Columns.PurchaseID.EQ(psql.Arg(purchase.ID)),
	))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	if err := evidence.LoadSourceDocument(ctx, executor); err != nil {
		return nil, err
	}
	confirmations, err := models.DocumentConfirmations.Query(sm.Where(psql.And(
		models.DocumentConfirmations.Columns.HouseholdID.EQ(psql.Arg(purchase.HouseholdID)),
		models.DocumentConfirmations.Columns.PurchaseID.EQ(psql.Arg(purchase.ID)),
	))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	if err := confirmations.LoadSourceDocument(ctx, executor); err != nil {
		return nil, err
	}
	result.HasEvidence = len(evidence)+len(confirmations) > 0
	sourceKinds := make(map[model.SourceKind]struct{}, len(evidence))
	for _, link := range evidence {
		if link.R.SourceDocument == nil {
			continue
		}
		kind := model.SourceKind(strings.ToUpper(link.R.SourceDocument.SourceKind))
		sourceKinds[kind] = struct{}{}
	}
	for _, confirmation := range confirmations {
		if confirmation.R.SourceDocument == nil {
			continue
		}
		kind := model.SourceKind(strings.ToUpper(confirmation.R.SourceDocument.SourceKind))
		sourceKinds[kind] = struct{}{}
	}
	for kind := range sourceKinds {
		result.SourceKinds = append(result.SourceKinds, kind)
	}
	slices.Sort(result.SourceKinds)
	var legacyPaymentAccount *model.Account
	if purchase.PaymentAccountID.Valid {
		if err := purchase.LoadAccount(ctx, executor); err != nil {
			return nil, err
		}
		legacyPaymentAccount = graphAccount(purchase.R.Account)
	}
	items, err := models.PurchaseItems.Query(
		sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchase.ID))),
		sm.OrderBy(models.PurchaseItems.Columns.CreatedAt),
		sm.OrderBy(models.PurchaseItems.Columns.ID),
	).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	allocationsByItem := make(map[gofrsuuid.UUID]models.ItemAllocationSlice, len(items))
	for _, item := range items {
		allocations, err := models.ItemAllocations.Query(sm.Where(psql.And(
			models.ItemAllocations.Columns.HouseholdID.EQ(psql.Arg(item.HouseholdID)),
			models.ItemAllocations.Columns.PurchaseID.EQ(psql.Arg(item.PurchaseID)),
			models.ItemAllocations.Columns.ItemID.EQ(psql.Arg(item.ID)),
		))).All(ctx, executor)
		if err != nil {
			return nil, err
		}
		allocationsByItem[item.ID] = allocations
	}
	for _, allocations := range allocationsByItem {
		if err := allocations.LoadPool(ctx, executor); err != nil {
			return nil, err
		}
	}
	var total int64
	for _, storedItem := range items {
		item := &model.PurchaseItem{
			ID: storedItem.ID.String(), Name: storedItem.Name, AmountYen: graphLong(storedItem.AmountYen),
			Revision: int(storedItem.Revision), Currency: "JPY", Allocations: make([]*model.Allocation, 0), Tags: make([]*model.Tag, 0),
		}
		measure := purchaseItemMeasure{
			quantityNumerator: storedItem.QuantityNumerator, quantityDenominator: storedItem.QuantityDenominator,
			quantityUnit: storedItem.QuantityUnit, printedUnitPriceYen: storedItem.PrintedUnitPriceYen,
			printedUnitPriceQuantityNumerator:   storedItem.PrintedUnitPriceQuantityNumerator,
			printedUnitPriceQuantityDenominator: storedItem.PrintedUnitPriceQuantityDenominator, printedUnitPriceUnit: storedItem.PrintedUnitPriceUnit,
		}
		if measure.quantityNumerator.Valid {
			item.Quantity = &model.PurchaseQuantity{Numerator: strconv.FormatInt(measure.quantityNumerator.V, 10), Denominator: strconv.FormatInt(measure.quantityDenominator.V, 10), Unit: measure.quantityUnit.V}
		}
		if measure.printedUnitPriceYen.Valid {
			item.PrintedUnitPrice = &model.PrintedUnitPrice{
				AmountYen: graphLong(measure.printedUnitPriceYen.V),
				Basis:     &model.PurchaseQuantity{Numerator: strconv.FormatInt(measure.printedUnitPriceQuantityNumerator.V, 10), Denominator: strconv.FormatInt(measure.printedUnitPriceQuantityDenominator.V, 10), Unit: measure.printedUnitPriceUnit.V},
			}
		}
		effectiveNumerator, effectiveDenominator, ok := effectiveUnitPrice(storedItem.AmountYen, measure)
		if !ok {
			return nil, fmt.Errorf("purchase item %s effective unit price exceeds int64", storedItem.ID)
		}
		if effectiveNumerator != nil && effectiveDenominator != nil {
			item.EffectiveUnitPrice = &model.EffectiveUnitPrice{NumeratorYen: *effectiveNumerator, Denominator: *effectiveDenominator}
		}
		if storedItem.CategoryID.Valid {
			categoryID := storedItem.CategoryID.V.String()
			item.CategoryID = &categoryID
		}
		if storedItem.ProductVariantID.Valid {
			variantID := storedItem.ProductVariantID.V.String()
			item.ProductVariantID = &variantID
		}
		total, ok = checkedAdd(total, storedItem.AmountYen)
		if !ok {
			return nil, fmt.Errorf("purchase %s total exceeds int64", purchaseID)
		}
		for _, storedAllocation := range allocationsByItem[storedItem.ID] {
			poolName := storedAllocation.R.Pool.Name
			allocation := &model.Allocation{
				PoolID: storedAllocation.PoolID.String(), PoolName: &poolName,
				Weight: graphLong(storedAllocation.Weight),
			}
			amount := graphLong(storedAllocation.AmountYen)
			allocation.AmountYen = &amount
			item.Allocations = append(item.Allocations, allocation)
		}
		tagLinks, err := models.PurchaseItemTags.Query(sm.Where(models.PurchaseItemTags.Columns.ItemID.EQ(psql.Arg(storedItem.ID)))).All(ctx, executor)
		if err != nil {
			return nil, err
		}
		if err := tagLinks.LoadTag(ctx, executor); err != nil {
			return nil, err
		}
		for _, tagLink := range tagLinks {
			item.Tags = append(item.Tags, tagModel(tagLink.R.Tag))
		}
		result.Items = append(result.Items, item)
	}
	result.TotalYen = graphLong(total)
	paymentRows, err := models.PurchasePayments.Query(
		sm.Where(models.PurchasePayments.Columns.PurchaseID.EQ(psql.Arg(purchase.ID))),
		sm.OrderBy(models.PurchasePayments.Columns.LineNo),
	).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	if err := paymentRows.LoadAccount(ctx, executor); err != nil {
		return nil, err
	}
	for _, storedPayment := range paymentRows {
		result.PaymentAllocations = append(result.PaymentAllocations, &model.PurchasePayment{
			PaymentAccountID: storedPayment.AccountID.String(), AmountYen: graphLong(storedPayment.AmountYen),
			Account: graphAccount(storedPayment.R.Account),
		})
	}
	if len(result.PaymentAllocations) == 0 && purchase.PaymentAccountID.Valid {
		result.PaymentAllocations = append(result.PaymentAllocations, &model.PurchasePayment{
			PaymentAccountID: purchase.PaymentAccountID.V.String(), Account: legacyPaymentAccount, AmountYen: graphLong(total),
		})
	}
	correctionRows, err := models.Corrections.Query(
		sm.Where(models.Corrections.Columns.PurchaseID.EQ(psql.Arg(purchase.ID))),
		sm.OrderBy(models.Corrections.Columns.CreatedAt),
		sm.OrderBy(models.Corrections.Columns.ID),
	).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	if err := correctionRows.LoadJournal(ctx, executor); err != nil {
		return nil, err
	}
	for _, storedCorrection := range correctionRows {
		correction := &model.PurchaseCorrection{
			ID: storedCorrection.ID.String(), AmountDeltaYen: graphLong(storedCorrection.AmountDeltaYen),
			Reason: storedCorrection.Reason, OccurredAt: timeString(storedCorrection.R.Journal.OccurredAt),
			SettlementMonth: accountingPeriod(purchase.OccurredAt), PaymentAllocations: make([]*model.PurchasePayment, 0),
		}
		if storedCorrection.ItemID.Valid {
			itemID := storedCorrection.ItemID.V.String()
			correction.ItemID = &itemID
		}
		if storedCorrection.EffectivePeriod.Valid {
			correction.SettlementMonth = storedCorrection.EffectivePeriod.V
		}
		payments, err := models.CorrectionPayments.Query(
			sm.Where(models.CorrectionPayments.Columns.CorrectionID.EQ(psql.Arg(storedCorrection.ID))),
			sm.OrderBy(models.CorrectionPayments.Columns.LineNo),
		).All(ctx, executor)
		if err != nil {
			return nil, err
		}
		if err := payments.LoadAccount(ctx, executor); err != nil {
			return nil, err
		}
		for _, storedPayment := range payments {
			correction.PaymentAllocations = append(correction.PaymentAllocations, &model.PurchasePayment{
				PaymentAccountID: storedPayment.AccountID.String(), AmountYen: graphLong(storedPayment.AmountYen),
				Account: graphAccount(storedPayment.R.Account),
			})
		}
		result.Corrections = append(result.Corrections, correction)
	}
	return result, nil
}

func graphAccount(account *models.Account) *model.Account {
	if account == nil {
		return nil
	}
	result := &model.Account{ID: account.ID.String(), Name: account.Name, Kind: model.AccountKind(account.Kind)}
	if account.PoolID.Valid {
		poolID := account.PoolID.V.String()
		result.PoolID = &poolID
	}
	return result
}

func loadWeights(ctx context.Context, tx pgx.Tx, householdID string, version int64) ([]domain.Weight, error) {
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, err
	}
	defaults, err := models.AllocationDefaults.Query(sm.Where(psql.And(
		models.AllocationDefaults.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.AllocationDefaults.Columns.Version.EQ(psql.Arg(version)),
	)), sm.OrderBy(models.AllocationDefaults.Columns.PoolID)).All(ctx, bobpgx.NewTx(tx, nil))
	if err != nil {
		return nil, err
	}
	weights := make([]domain.Weight, 0, len(defaults))
	for _, allocation := range defaults {
		weights = append(weights, domain.Weight{PoolID: allocation.PoolID.String(), Value: allocation.Weight})
	}
	return weights, nil
}
