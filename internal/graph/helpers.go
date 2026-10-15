package graph

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"uuid"

	"github.com/99designs/gqlgen/graphql"
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
		models.HouseholdMembers.Columns.LeftAt.IsNull(),
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
	households, _, err := loadHouseholdSummaryData(ctx, db, householdIDs)
	return households, err
}

func loadHouseholdSummaryData(ctx context.Context, db *pgxpool.Pool, householdIDs []gofrsuuid.UUID) (map[gofrsuuid.UUID]*model.Household, map[gofrsuuid.UUID]int64, error) {
	result := make(map[gofrsuuid.UUID]*model.Household, len(householdIDs))
	allocationVersions := make(map[gofrsuuid.UUID]int64, len(householdIDs))
	if len(householdIDs) == 0 {
		return result, allocationVersions, nil
	}
	ids := make([]bob.Expression, len(householdIDs))
	for i, id := range householdIDs {
		ids[i] = psql.Arg(id)
	}
	households, err := models.Households.Query(sm.Where(models.Households.Columns.ID.In(ids...))).All(ctx, queryExecutor(db))
	if err != nil {
		return nil, nil, err
	}
	for _, household := range households {
		result[household.ID] = &model.Household{
			ID: household.ID.String(), Name: household.Name, Revision: int(household.Revision),
			CreatedAt: timeString(household.CreatedAt), UpdatedAt: timeString(household.UpdatedAt),
		}
		allocationVersions[household.ID] = household.DefaultAllocationVersion
	}
	return result, allocationVersions, nil
}

func loadHousehold(ctx context.Context, db *pgxpool.Pool, householdID string, fields householdFields) (*model.Household, error) {
	id, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	households, allocationVersions, err := loadHouseholdSummaryData(ctx, db, []gofrsuuid.UUID{id})
	if err != nil {
		return nil, err
	}
	result := households[id]
	if result == nil {
		return nil, ErrNotFound
	}
	if err := loadHouseholdDetails(ctx, db, households, allocationVersions, fields); err != nil {
		return nil, err
	}
	return result, nil
}

type householdFields struct {
	members           bool
	accounts          bool
	categories        bool
	pools             bool
	defaultAllocation bool
}

var allHouseholdFields = householdFields{members: true, accounts: true, categories: true, pools: true, defaultAllocation: true}

func householdIDs(ids map[gofrsuuid.UUID]*model.Household) []bob.Expression {
	args := make([]bob.Expression, 0, len(ids))
	for id := range ids {
		args = append(args, psql.Arg(id))
	}
	return args
}

func loadHouseholdMemberIndex(ctx context.Context, db *pgxpool.Pool, householdIDs []gofrsuuid.UUID, historicalMemberIDs map[gofrsuuid.UUID][]gofrsuuid.UUID) (map[gofrsuuid.UUID]map[string]*model.Member, error) {
	result := make(map[gofrsuuid.UUID]map[string]*model.Member, len(householdIDs))
	if len(householdIDs) == 0 {
		return result, nil
	}
	args := make([]bob.Expression, len(householdIDs))
	for index, householdID := range householdIDs {
		args[index] = psql.Arg(householdID)
	}
	memberships, err := models.HouseholdMembers.Query(sm.Where(models.HouseholdMembers.Columns.HouseholdID.In(args...))).All(ctx, queryExecutor(db))
	if err != nil {
		return nil, err
	}
	if err := memberships.LoadMember(ctx, queryExecutor(db)); err != nil {
		return nil, err
	}
	for _, membership := range memberships {
		members := result[membership.HouseholdID]
		if members == nil {
			members = make(map[string]*model.Member)
			result[membership.HouseholdID] = members
		}
		members[membership.MemberID.String()] = &model.Member{
			ID: membership.MemberID.String(), DisplayName: membership.R.Member.DisplayName, Role: model.MemberRole(membership.Role),
		}
	}
	historicalIDs := make([]bob.Expression, 0)
	historicalIDsByHousehold := make(map[gofrsuuid.UUID][]gofrsuuid.UUID, len(historicalMemberIDs))
	for householdID, ids := range historicalMemberIDs {
		for _, id := range ids {
			if result[householdID][id.String()] != nil {
				continue
			}
			historicalIDs = append(historicalIDs, psql.Arg(id))
			historicalIDsByHousehold[householdID] = append(historicalIDsByHousehold[householdID], id)
		}
	}
	if len(historicalIDs) == 0 {
		return result, nil
	}
	historicalMembers, err := models.Members.Query(sm.Where(models.Members.Columns.ID.In(historicalIDs...))).All(ctx, queryExecutor(db))
	if err != nil {
		return nil, err
	}
	membersByID := make(map[gofrsuuid.UUID]*model.Member, len(historicalMembers))
	for _, member := range historicalMembers {
		membersByID[member.ID] = &model.Member{ID: member.ID.String(), DisplayName: member.DisplayName, Role: model.MemberRoleMember}
	}
	for householdID, ids := range historicalIDsByHousehold {
		members := result[householdID]
		if members == nil {
			members = make(map[string]*model.Member)
			result[householdID] = members
		}
		for _, id := range ids {
			if member := membersByID[id]; member != nil {
				members[id.String()] = member
			}
		}
	}
	return result, nil
}

func loadHouseholdDetails(ctx context.Context, db *pgxpool.Pool, households map[gofrsuuid.UUID]*model.Household, allocationVersions map[gofrsuuid.UUID]int64, fields householdFields) error {
	if len(households) == 0 || !(fields.members || fields.accounts || fields.categories || fields.pools || fields.defaultAllocation) {
		return nil
	}
	args := householdIDs(households)
	executor := queryExecutor(db)
	memberNames := make(map[gofrsuuid.UUID]string)
	if fields.members || fields.pools {
		memberships, err := models.HouseholdMembers.Query(
			sm.Where(models.HouseholdMembers.Columns.HouseholdID.In(args...)),
			sm.OrderBy(models.HouseholdMembers.Columns.HouseholdID),
			sm.OrderBy(models.HouseholdMembers.Columns.JoinedAt),
			sm.OrderBy(models.HouseholdMembers.Columns.MemberID),
		).All(ctx, executor)
		if err != nil {
			return err
		}
		if err := memberships.LoadMember(ctx, executor); err != nil {
			return err
		}
		for _, membership := range memberships {
			memberNames[membership.MemberID] = membership.R.Member.DisplayName
		}
		if fields.members {
			for _, household := range households {
				household.Members = make([]*model.Member, 0)
			}
			for _, membership := range memberships {
				if membership.LeftAt.Valid {
					continue
				}
				households[membership.HouseholdID].Members = append(households[membership.HouseholdID].Members, &model.Member{
					ID: membership.MemberID.String(), DisplayName: membership.R.Member.DisplayName, Role: model.MemberRole(membership.Role),
				})
			}
		}
	}
	if fields.accounts {
		for _, household := range households {
			household.Accounts = make([]*model.Account, 0)
		}
		accounts, err := models.Accounts.Query(
			sm.Where(models.Accounts.Columns.HouseholdID.In(args...)),
			sm.OrderBy(models.Accounts.Columns.HouseholdID),
			sm.OrderBy(models.Accounts.Columns.CreatedAt),
			sm.OrderBy(models.Accounts.Columns.ID),
		).All(ctx, executor)
		if err != nil {
			return err
		}
		for _, stored := range accounts {
			account := &model.Account{ID: stored.ID.String(), Name: stored.Name, Kind: model.AccountKind(stored.Kind)}
			if stored.CardLast4.Valid {
				account.CardLast4 = &stored.CardLast4.V
			}
			if stored.PoolID.Valid {
				account.PoolID = new(stored.PoolID.V.String())
			}
			households[stored.HouseholdID].Accounts = append(households[stored.HouseholdID].Accounts, account)
		}
	}
	if fields.categories {
		for _, household := range households {
			household.Categories = make([]*model.Category, 0)
		}
		categories, err := models.Categories.Query(
			sm.Where(models.Categories.Columns.HouseholdID.In(args...)),
			sm.OrderBy(models.Categories.Columns.HouseholdID),
			sm.OrderBy(models.Categories.Columns.Name),
			sm.OrderBy(models.Categories.Columns.ID),
		).All(ctx, executor)
		if err != nil {
			return err
		}
		for _, stored := range categories {
			category := &model.Category{ID: stored.ID.String(), Name: stored.Name, Archived: stored.Archived}
			if stored.ParentID.Valid {
				category.ParentID = new(stored.ParentID.V.String())
			}
			households[stored.HouseholdID].Categories = append(households[stored.HouseholdID].Categories, category)
		}
	}
	if fields.pools || fields.defaultAllocation {
		pools, err := models.Pools.Query(
			sm.Where(models.Pools.Columns.HouseholdID.In(args...)),
			sm.OrderBy(models.Pools.Columns.HouseholdID),
			sm.OrderBy(models.Pools.Columns.CreatedAt),
			sm.OrderBy(models.Pools.Columns.ID),
		).All(ctx, executor)
		if err != nil {
			return err
		}
		poolByID := make(map[gofrsuuid.UUID]*model.Pool, len(pools))
		for _, household := range households {
			if fields.pools {
				household.PoolVariants = make([]model.PoolVariant, 0)
			}
		}
		for _, stored := range pools {
			pool := &model.Pool{ID: stored.ID.String(), Name: stored.Name, Kind: model.PoolKind(stored.Kind), FundingRuleVersion: int(stored.FundingRuleVersion), FundingShares: make([]*model.PoolFundingShare, 0)}
			poolByID[stored.ID] = pool
			if fields.pools {
				households[stored.HouseholdID].PoolVariants = append(households[stored.HouseholdID].PoolVariants, poolVariant(pool))
			}
		}
		defaults, err := models.AllocationDefaults.Query(sm.Where(models.AllocationDefaults.Columns.HouseholdID.In(args...))).All(ctx, executor)
		if err != nil {
			return err
		}
		defaultsByHousehold := make(map[gofrsuuid.UUID][]*models.AllocationDefault)
		defaultWeightByPool := make(map[gofrsuuid.UUID]int64, len(defaults))
		for _, allocation := range defaults {
			if allocation.Version != allocationVersions[allocation.HouseholdID] {
				continue
			}
			defaultsByHousehold[allocation.HouseholdID] = append(defaultsByHousehold[allocation.HouseholdID], allocation)
			defaultWeightByPool[allocation.PoolID] = allocation.Weight
		}
		for id, pool := range poolByID {
			pool.DefaultShareWeight = graphLong(defaultWeightByPool[id])
		}
		if fields.pools {
			fundingRules, err := models.PoolFundingRules.Query(sm.Where(models.PoolFundingRules.Columns.HouseholdID.In(args...))).All(ctx, executor)
			if err != nil {
				return err
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
					pool.OwnerMemberID = new(rule.OwnerMemberID.V.String())
				}
				if rule.SettlementPolicy.Valid {
					pool.SettlementPolicy = new(model.SharedPoolSettlementPolicy(rule.SettlementPolicy.V))
				}
			}
			fundingShares, err := models.PoolFundingShares.Query(sm.Where(models.PoolFundingShares.Columns.HouseholdID.In(args...))).All(ctx, executor)
			if err != nil {
				return err
			}
			slices.SortFunc(fundingShares, func(left, right *models.PoolFundingShare) int {
				if comparison := cmp.Compare(left.PoolID.String(), right.PoolID.String()); comparison != 0 {
					return comparison
				}
				return cmp.Compare(left.MemberID.String(), right.MemberID.String())
			})
			for _, share := range fundingShares {
				rule, pool := activeRules[share.PoolID], poolByID[share.PoolID]
				if rule != nil && pool != nil && share.Version == rule.Version {
					pool.FundingShares = append(pool.FundingShares, &model.PoolFundingShare{MemberID: share.MemberID.String(), MemberName: memberNames[share.MemberID], Weight: graphLong(share.Weight)})
				}
			}
		}
		if fields.defaultAllocation {
			for _, household := range households {
				household.DefaultAllocation = make([]*model.Allocation, 0)
			}
			for householdID, allocations := range defaultsByHousehold {
				for _, allocation := range allocations {
					pool := poolByID[allocation.PoolID]
					if pool != nil {
						households[householdID].DefaultAllocation = append(households[householdID].DefaultAllocation, &model.Allocation{PoolID: pool.ID, PoolName: pool.Name, Weight: graphLong(allocation.Weight)})
					}
				}
			}
		}
	}
	for _, household := range households {
		if fields.members && household.Members == nil {
			household.Members = make([]*model.Member, 0)
		}
	}
	return nil
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
	loadPaymentAllocations := shouldLoadPurchasePaymentAllocations(ctx)
	if loadPaymentAllocations {
		if err := purchases.LoadAccount(ctx, executor); err != nil {
			return nil, err
		}
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
	if loadPaymentAllocations {
		if err := purchases.LoadPurchasePayments(ctx, executor, sm.OrderBy(models.PurchasePayments.Columns.LineNo)); err != nil {
			return nil, err
		}
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
		if loadPaymentAllocations {
			payments = append(payments, purchase.R.PurchasePayments...)
		}
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
	if loadPaymentAllocations {
		if err := payments.LoadAccount(ctx, executor); err != nil {
			return nil, err
		}
	}

	return mapPurchases(purchases, correctionAggregates, itemCorrectionAggregates)
}

func shouldLoadPurchasePaymentAllocations(ctx context.Context) bool {
	if !graphql.HasOperationContext(ctx) {
		return true
	}
	fieldContext := graphql.GetFieldContext(ctx)
	if fieldContext == nil || fieldContext.Field.Name != "purchases" {
		return true
	}
	return graphql.FieldRequested(ctx, "edges.node.paymentAllocations")
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
