package graph

import (
	"context"
	"errors"
	"fmt"
	"time"

	"uuid"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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
	var member bool
	err = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM household_members WHERE household_id = $1 AND member_id = $2)`, householdID, user.MemberID).Scan(&member)
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
	if errors.Is(err, pgx.ErrNoRows) {
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
	result.Members = make([]*model.Member, 0)
	rows, err := db.Query(ctx, `SELECT m.id::text, m.display_name, hm.role FROM household_members hm JOIN members m ON m.id = hm.member_id WHERE hm.household_id = $1 ORDER BY hm.joined_at, m.id`, householdID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		member := &model.Member{}
		if err := rows.Scan(&member.ID, &member.DisplayName, &member.Role); err != nil {
			rows.Close()
			return nil, err
		}
		result.Members = append(result.Members, member)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result.Pools = make([]*model.Pool, 0)
	rows, err = db.Query(ctx, `SELECT p.id::text, p.name, p.kind, COALESCE(d.weight, 0), p.funding_rule_version FROM pools p LEFT JOIN allocation_defaults d ON d.household_id = p.household_id AND d.pool_id = p.id AND d.version = $2 WHERE p.household_id = $1 ORDER BY p.created_at, p.id`, householdID, allocationVersion)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		pool := &model.Pool{}
		if err := rows.Scan(&pool.ID, &pool.Name, &pool.Kind, &pool.DefaultShareWeight, &pool.FundingRuleVersion); err != nil {
			rows.Close()
			return nil, err
		}
		result.Pools = append(result.Pools, pool)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	fundingRows, err := db.Query(ctx, `SELECT r.pool_id::text,r.owner_member_id::text,r.settlement_policy,s.member_id::text,s.weight,m.display_name FROM pool_funding_rules r LEFT JOIN pool_funding_shares s ON s.household_id=r.household_id AND s.pool_id=r.pool_id AND s.version=r.version LEFT JOIN members m ON m.id=s.member_id JOIN pools p ON p.household_id=r.household_id AND p.id=r.pool_id AND p.funding_rule_version=r.version WHERE r.household_id=$1 ORDER BY r.pool_id,s.member_id`, householdID)
	if err != nil {
		return nil, err
	}
	poolByID := make(map[string]*model.Pool, len(result.Pools))
	for _, pool := range result.Pools {
		pool.FundingShares = make([]*model.PoolFundingShare, 0)
		poolByID[pool.ID] = pool
	}
	for fundingRows.Next() {
		var poolID string
		var owner, policy, memberID, memberName *string
		var weight *int64
		if err := fundingRows.Scan(&poolID, &owner, &policy, &memberID, &weight, &memberName); err != nil {
			fundingRows.Close()
			return nil, err
		}
		pool := poolByID[poolID]
		if owner != nil {
			pool.OwnerMemberID = owner
		}
		if policy != nil {
			value := model.SharedPoolSettlementPolicy(*policy)
			pool.SettlementPolicy = &value
		}
		if memberID != nil {
			pool.FundingShares = append(pool.FundingShares, &model.PoolFundingShare{MemberID: *memberID, MemberName: *memberName, Weight: graphLong(*weight)})
		}
	}
	fundingRows.Close()
	if err := fundingRows.Err(); err != nil {
		return nil, err
	}
	result.Accounts = make([]*model.Account, 0)
	rows, err = db.Query(ctx, `SELECT id::text, name, kind, pool_id::text FROM accounts WHERE household_id = $1 ORDER BY created_at, id`, householdID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		account := &model.Account{}
		if err := rows.Scan(&account.ID, &account.Name, &account.Kind, &account.PoolID); err != nil {
			rows.Close()
			return nil, err
		}
		result.Accounts = append(result.Accounts, account)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result.Categories = make([]*model.Category, 0)
	rows, err = db.Query(ctx, `SELECT id::text, name, parent_id::text, archived FROM categories WHERE household_id = $1 ORDER BY name, id`, householdID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		category := &model.Category{}
		if err := rows.Scan(&category.ID, &category.Name, &category.ParentID, &category.Archived); err != nil {
			rows.Close()
			return nil, err
		}
		result.Categories = append(result.Categories, category)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result.DefaultAllocation = make([]*model.Allocation, 0)
	rows, err = db.Query(ctx, `SELECT d.pool_id::text, p.name, d.weight FROM allocation_defaults d JOIN pools p ON p.id = d.pool_id WHERE d.household_id = $1 AND d.version = $2 ORDER BY p.id`, householdID, allocationVersion)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		allocation := &model.Allocation{}
		if err := rows.Scan(&allocation.PoolID, &allocation.PoolName, &allocation.Weight); err != nil {
			rows.Close()
			return nil, err
		}
		result.DefaultAllocation = append(result.DefaultAllocation, allocation)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &result, nil
}

func loadPurchase(ctx context.Context, db *pgxpool.Pool, purchaseID string) (*model.Purchase, error) {
	var result model.Purchase
	var occurredAt, createdAt, updatedAt time.Time
	var revision int64
	var total int64
	err := db.QueryRow(ctx, `SELECT p.id::text, p.household_id::text, p.occurred_at, p.merchant, p.payment_account_id::text, p.revision, p.status, p.created_at, p.updated_at, COALESCE(sum(i.amount_yen), 0) FROM purchases p LEFT JOIN purchase_items i ON i.purchase_id = p.id WHERE p.id = $1 GROUP BY p.id`, purchaseID).Scan(&result.ID, &result.HouseholdID, &occurredAt, &result.Merchant, &result.PaymentAccountID, &revision, &result.Status, &createdAt, &updatedAt, &total)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	result.OccurredAt, result.CreatedAt, result.UpdatedAt = timeString(occurredAt), timeString(createdAt), timeString(updatedAt)
	result.Revision, result.Currency = int(revision), "JPY"
	result.TotalYen = graphLong(total)
	result.Items = make([]*model.PurchaseItem, 0)
	rows, err := db.Query(ctx, `SELECT id::text,name,amount_yen,category_id::text,product_variant_id::text,revision FROM purchase_items WHERE purchase_id=$1 ORDER BY created_at,id`, purchaseID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		item := &model.PurchaseItem{Currency: "JPY", Allocations: make([]*model.Allocation, 0)}
		var amount, itemRevision int64
		if err := rows.Scan(&item.ID, &item.Name, &amount, &item.CategoryID, &item.ProductVariantID, &itemRevision); err != nil {
			rows.Close()
			return nil, err
		}
		item.AmountYen = graphLong(amount)
		item.Revision = int(itemRevision)
		allocations, err := db.Query(ctx, `SELECT a.pool_id::text, p.name, a.weight, a.amount_yen FROM item_allocations a JOIN pools p ON p.id = a.pool_id WHERE a.item_id = $1 ORDER BY p.id`, item.ID)
		if err != nil {
			rows.Close()
			return nil, err
		}
		for allocations.Next() {
			allocation := &model.Allocation{}
			var weight, allocated int64
			if err := allocations.Scan(&allocation.PoolID, &allocation.PoolName, &weight, &allocated); err != nil {
				allocations.Close()
				rows.Close()
				return nil, err
			}
			allocation.Weight = graphLong(weight)
			amount := graphLong(allocated)
			allocation.AmountYen = &amount
			item.Allocations = append(item.Allocations, allocation)
		}
		allocations.Close()
		if err := allocations.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		result.Items = append(result.Items, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if result.PaymentAccountID != nil {
		account := &model.Account{}
		if err := db.QueryRow(ctx, `SELECT id::text, name, kind, pool_id::text FROM accounts WHERE id = $1`, *result.PaymentAccountID).Scan(&account.ID, &account.Name, &account.Kind, &account.PoolID); err != nil {
			return nil, err
		}
		result.PaymentAccount = account
	}
	return &result, nil
}

func loadWeights(ctx context.Context, tx pgx.Tx, householdID string, version int64) ([]domain.Weight, error) {
	rows, err := tx.Query(ctx, `SELECT pool_id::text, weight FROM allocation_defaults WHERE household_id = $1 AND version = $2 ORDER BY pool_id`, householdID, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	weights := make([]domain.Weight, 0)
	for rows.Next() {
		var weight domain.Weight
		if err := rows.Scan(&weight.PoolID, &weight.Value); err != nil {
			return nil, err
		}
		weights = append(weights, weight)
	}
	return weights, rows.Err()
}
