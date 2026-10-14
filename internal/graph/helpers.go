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
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/auth"
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

func parseTime[T ~string](value T) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, string(value))
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: timestamp must use RFC3339", ErrBadInput)
	}
	return parsed.UTC(), nil
}

func timeString(value time.Time) model.DateTime {
	return model.DateTime(value.UTC().Format(time.RFC3339Nano))
}

func stringPointer[T ~string](value *T) *string {
	if value == nil {
		return nil
	}
	result := string(*value)
	return &result
}

func datePointer(value *string) *model.Date {
	if value == nil {
		return nil
	}
	result := model.Date(*value)
	return &result
}

func yearMonthPointer(value *model.YearMonth) *string {
	if value == nil {
		return nil
	}
	result := string(*value)
	return &result
}

func newID() string { return uuid.NewV7().String() }

func graphLong(value int64) model.Long { return model.Long(value) }

func loadHouseholdSummaries(ctx context.Context, db *pgxpool.Pool, householdIDs []gofrsuuid.UUID) (map[gofrsuuid.UUID]*model.Household, error) {
	result := make(map[gofrsuuid.UUID]*model.Household, len(householdIDs))
	if len(householdIDs) == 0 {
		return result, nil
	}
	ids := make([]bob.Expression, len(householdIDs))
	for i, id := range householdIDs {
		ids[i] = psql.Arg(id)
	}
	households, err := models.Households.Query(sm.Where(models.Households.Columns.ID.In(ids...))).All(ctx, queryExecutor(db))
	if err != nil {
		return nil, err
	}
	for _, household := range households {
		result[household.ID] = &model.Household{
			ID: household.ID.String(), Name: household.Name, Revision: int(household.Revision),
			CreatedAt: timeString(household.CreatedAt), UpdatedAt: timeString(household.UpdatedAt),
		}
	}
	return result, nil
}

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
			ID: householdMember.MemberID.String(), DisplayName: householdMember.R.Member.DisplayName, Role: model.MemberRole(householdMember.Role),
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
	result.PoolVariants = make([]model.PoolVariant, 0)
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
		result.PoolVariants = append(result.PoolVariants, poolVariant(value))
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
		if storedAccount.CardLast4.Valid {
			cardLast4 := storedAccount.CardLast4.V
			account.CardLast4 = &cardLast4
		}
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
		result.DefaultAllocation = append(result.DefaultAllocation, &model.Allocation{
			PoolID: pool.ID, PoolName: pool.Name, Weight: graphLong(allocation.Weight),
		})
	}
	return &result, nil
}

func loadPurchase(ctx context.Context, db *pgxpool.Pool, purchaseID string) (*model.Purchase, error) {
	id, err := gofrsuuid.FromString(purchaseID)
	if err != nil {
		return nil, ErrNotFound
	}
	purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(id)))).One(ctx, queryExecutor(db))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	loaded, err := loadPurchases(ctx, db, models.PurchaseSlice{purchase})
	if err != nil {
		return nil, err
	}
	return loaded[0], nil
}

func loadPurchases(ctx context.Context, db *pgxpool.Pool, purchases models.PurchaseSlice) ([]*model.Purchase, error) {
	if len(purchases) == 0 {
		return []*model.Purchase{}, nil
	}
	executor := queryExecutor(db)
	if err := purchases.LoadAccount(ctx, executor); err != nil {
		return nil, err
	}
	if err := purchases.LoadEvidenceLinks(ctx, executor); err != nil {
		return nil, err
	}
	if err := purchases.LoadDocumentConfirmations(ctx, executor); err != nil {
		return nil, err
	}
	if err := purchases.LoadPurchaseItems(ctx, executor, sm.OrderBy(models.PurchaseItems.Columns.CreatedAt), sm.OrderBy(models.PurchaseItems.Columns.ID)); err != nil {
		return nil, err
	}
	if err := purchases.LoadPurchasePayments(ctx, executor, sm.OrderBy(models.PurchasePayments.Columns.LineNo)); err != nil {
		return nil, err
	}
	correctionAggregates, err := aggregatePurchaseCorrections(ctx, db, purchases)
	if err != nil {
		return nil, err
	}

	items := make(models.PurchaseItemSlice, 0)
	evidenceLinks := make(models.EvidenceLinkSlice, 0)
	confirmations := make(models.DocumentConfirmationSlice, 0)
	payments := make(models.PurchasePaymentSlice, 0)
	for _, purchase := range purchases {
		items = append(items, purchase.R.PurchaseItems...)
		evidenceLinks = append(evidenceLinks, purchase.R.EvidenceLinks...)
		confirmations = append(confirmations, purchase.R.DocumentConfirmations...)
		payments = append(payments, purchase.R.PurchasePayments...)
	}
	itemCorrectionAggregates, err := aggregatePurchaseItemCorrections(ctx, db, items)
	if err != nil {
		return nil, err
	}
	if err := evidenceLinks.LoadSourceDocument(ctx, executor); err != nil {
		return nil, err
	}
	if err := confirmations.LoadSourceDocument(ctx, executor); err != nil {
		return nil, err
	}
	if err := items.LoadItemAllocations(ctx, executor, sm.OrderBy(models.ItemAllocations.Columns.PoolID)); err != nil {
		return nil, err
	}
	if err := items.LoadPurchaseItemTags(ctx, executor, sm.OrderBy(models.PurchaseItemTags.Columns.TagID)); err != nil {
		return nil, err
	}
	allocations := make(models.ItemAllocationSlice, 0)
	itemTags := make(models.PurchaseItemTagSlice, 0)
	for _, item := range items {
		allocations = append(allocations, item.R.ItemAllocations...)
		itemTags = append(itemTags, item.R.PurchaseItemTags...)
	}
	if err := allocations.LoadPool(ctx, executor); err != nil {
		return nil, err
	}
	if err := itemTags.LoadTag(ctx, executor); err != nil {
		return nil, err
	}
	if err := payments.LoadAccount(ctx, executor); err != nil {
		return nil, err
	}

	result := make([]*model.Purchase, 0, len(purchases))
	for _, purchase := range purchases {
		loaded := &model.Purchase{
			ID: purchase.ID.String(), HouseholdID: purchase.HouseholdID.String(),
			OccurredAt: timeString(purchase.OccurredAt), Merchant: purchase.Merchant,
			Origin: model.PurchaseOrigin(purchase.Origin), SourceKinds: make([]model.SourceKind, 0),
			Revision: int(purchase.Revision), Status: model.PurchaseStatus(purchase.Status), Currency: currencyJPY,
			CreatedAt: timeString(purchase.CreatedAt), UpdatedAt: timeString(purchase.UpdatedAt),
			Items: make([]*model.PurchaseItem, 0), PaymentAllocations: make([]*model.PurchasePayment, 0),
			CorrectionsConnection: &model.PurchaseCorrectionConnection{
				Edges: make([]*model.PurchaseCorrectionEdge, 0), PageInfo: &model.PageInfo{},
			},
		}
		sourceKinds := make(map[model.SourceKind]struct{}, len(purchase.R.EvidenceLinks)+len(purchase.R.DocumentConfirmations))
		for _, link := range purchase.R.EvidenceLinks {
			if link.R.SourceDocument != nil {
				sourceKinds[model.SourceKind(strings.ToUpper(link.R.SourceDocument.SourceKind))] = struct{}{}
			}
		}
		for _, confirmation := range purchase.R.DocumentConfirmations {
			if confirmation.R.SourceDocument != nil {
				sourceKinds[model.SourceKind(strings.ToUpper(confirmation.R.SourceDocument.SourceKind))] = struct{}{}
			}
		}
		for kind := range sourceKinds {
			loaded.SourceKinds = append(loaded.SourceKinds, kind)
		}
		slices.Sort(loaded.SourceKinds)
		loaded.HasEvidence = len(purchase.R.EvidenceLinks)+len(purchase.R.DocumentConfirmations) > 0

		var total int64
		for _, storedItem := range purchase.R.PurchaseItems {
			itemCorrectionAmount := itemCorrectionAggregates[storedItem.ID]
			remainingItemRefundable, ok := checkedAdd(storedItem.AmountYen, itemCorrectionAmount)
			if !ok {
				return nil, fmt.Errorf("purchase item %s refundable total exceeds int64", storedItem.ID)
			}
			item := &model.PurchaseItem{
				ID: storedItem.ID.String(), Name: storedItem.Name, AmountYen: graphLong(storedItem.AmountYen),
				RemainingRefundableAmountYen: graphLong(remainingItemRefundable),
				Revision:                     int(storedItem.Revision), Currency: currencyJPY, Allocations: make([]*model.PurchaseItemAllocation, 0), Tags: make([]*model.Tag, 0),
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
			effectiveNumerator, effectiveDenominator := effectiveUnitPrice(storedItem.AmountYen, measure)
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
			nextTotal, ok := checkedAdd(total, storedItem.AmountYen)
			if !ok {
				return nil, fmt.Errorf("purchase %s total exceeds int64", purchase.ID)
			}
			total = nextTotal
			for _, storedAllocation := range storedItem.R.ItemAllocations {
				if storedAllocation.R.Pool == nil {
					return nil, fmt.Errorf("item allocation for item %s has no pool", storedItem.ID)
				}
				item.Allocations = append(item.Allocations, &model.PurchaseItemAllocation{
					PoolID: storedAllocation.PoolID.String(), PoolName: storedAllocation.R.Pool.Name,
					Weight: graphLong(storedAllocation.Weight), AmountYen: graphLong(storedAllocation.AmountYen),
				})
			}
			for _, tagLink := range storedItem.R.PurchaseItemTags {
				if tagLink.R.Tag == nil {
					return nil, fmt.Errorf("purchase item tag %s has no tag", tagLink.TagID)
				}
				item.Tags = append(item.Tags, tagModel(tagLink.R.Tag))
			}
			loaded.Items = append(loaded.Items, item)
		}
		loaded.TotalYen = graphLong(total)
		aggregate := correctionAggregates[purchase.ID]
		remainingRefundable, ok := checkedAdd(total, aggregate.AmountYen)
		if !ok {
			return nil, fmt.Errorf("purchase %s refundable total exceeds int64", purchase.ID)
		}
		loaded.CorrectionAmountYen = graphLong(aggregate.AmountYen)
		loaded.CorrectionCount = int(aggregate.Count)
		loaded.RemainingRefundableAmountYen = graphLong(remainingRefundable)
		for _, storedPayment := range purchase.R.PurchasePayments {
			loaded.PaymentAllocations = append(loaded.PaymentAllocations, &model.PurchasePayment{
				PaymentAccountID: storedPayment.AccountID.String(), AmountYen: graphLong(storedPayment.AmountYen),
				Account: graphAccount(storedPayment.R.Account),
			})
		}
		if len(loaded.PaymentAllocations) == 0 && purchase.PaymentAccountID.Valid {
			loaded.PaymentAllocations = append(loaded.PaymentAllocations, &model.PurchasePayment{
				PaymentAccountID: purchase.PaymentAccountID.V.String(), Account: graphAccount(purchase.R.Account), AmountYen: graphLong(total),
			})
		}
		result = append(result, loaded)
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
