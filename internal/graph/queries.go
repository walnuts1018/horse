package graph

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func (r *queryResolver) viewer(ctx context.Context) (*model.Viewer, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	result := &model.Viewer{ID: user.MemberID, Subject: user.Subject, DisplayName: user.DisplayName, Households: make([]*model.Household, 0)}
	rows, err := r.DB.Query(ctx, `SELECT household_id::text FROM household_members WHERE member_id = $1 ORDER BY joined_at`, user.MemberID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		household, err := loadHousehold(ctx, r.DB, id)
		if err != nil {
			return nil, err
		}
		result.Households = append(result.Households, household)
	}
	return result, nil
}

func (r *queryResolver) households(ctx context.Context) ([]*model.Household, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := r.DB.Query(ctx, `SELECT household_id::text FROM household_members WHERE member_id = $1 ORDER BY joined_at`, user.MemberID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]*model.Household, 0, len(ids))
	for _, id := range ids {
		household, err := loadHousehold(ctx, r.DB, id)
		if err != nil {
			return nil, err
		}
		result = append(result, household)
	}
	return result, nil
}

func (r *queryResolver) household(ctx context.Context, id string) (*model.Household, error) {
	if _, err := requireMember(ctx, r.DB, id); err != nil {
		return nil, err
	}
	return loadHousehold(ctx, r.DB, id)
}

func (r *queryResolver) allocationRules(ctx context.Context, householdID string) ([]*model.AllocationRule, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	rows, err := r.DB.Query(ctx, `SELECT id::text FROM allocation_rules WHERE household_id = $1 ORDER BY category_id NULLS LAST, merchant_key`, householdID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]*model.AllocationRule, 0, len(ids))
	for _, id := range ids {
		rule, err := loadAllocationRule(ctx, r.DB, id)
		if err != nil {
			return nil, err
		}
		result = append(result, rule)
	}
	return result, nil
}

func loadAllocationRule(ctx context.Context, db *pgxpool.Pool, ruleID string) (*model.AllocationRule, error) {
	var result model.AllocationRule
	var categoryID *string
	var merchant *string
	var revision int64
	if err := db.QueryRow(ctx, `SELECT id::text, household_id::text, category_id::text, merchant_key, revision FROM allocation_rules WHERE id = $1`, ruleID).Scan(&result.ID, &result.HouseholdID, &categoryID, &merchant, &revision); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	result.CategoryID, result.Merchant, result.Revision = categoryID, merchant, int(revision)
	if categoryID != nil {
		result.Scope = model.AllocationRuleScopeCategory
	} else {
		result.Scope = model.AllocationRuleScopeMerchant
	}
	rows, err := db.Query(ctx, `SELECT w.pool_id::text, p.name, w.weight FROM allocation_rule_weights w JOIN pools p ON p.id = w.pool_id WHERE w.rule_id = $1 ORDER BY w.pool_id`, ruleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result.Allocations = make([]*model.Allocation, 0)
	for rows.Next() {
		allocation := &model.Allocation{}
		var weight int64
		if err := rows.Scan(&allocation.PoolID, &allocation.PoolName, &weight); err != nil {
			return nil, err
		}
		allocation.Weight = graphLong(weight)
		result.Allocations = append(result.Allocations, allocation)
	}
	return &result, rows.Err()
}

func (r *queryResolver) purchases(ctx context.Context, householdID string, from, to, after *string, first *int) (*model.PurchaseConnection, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	pageSize := 50
	if first != nil {
		pageSize = *first
	}
	if pageSize < 1 || pageSize > 100 {
		return nil, fmt.Errorf("%w: first must be between 1 and 100", ErrBadInput)
	}
	var fromValue, toValue any
	if from != nil {
		parsed, err := parseTime(*from)
		if err != nil {
			return nil, err
		}
		fromValue = parsed
	}
	if to != nil {
		parsed, err := parseTime(*to)
		if err != nil {
			return nil, err
		}
		toValue = parsed
	}
	var afterValue any
	if after != nil && *after != "" {
		afterValue = *after
	}
	var count int
	if err := r.DB.QueryRow(ctx, `SELECT count(*) FROM purchases p WHERE p.household_id = $1 AND ($2::timestamptz IS NULL OR p.occurred_at >= $2) AND ($3::timestamptz IS NULL OR p.occurred_at < $3)`, householdID, fromValue, toValue).Scan(&count); err != nil {
		return nil, err
	}
	rows, err := r.DB.Query(ctx, `SELECT p.id::text FROM purchases p WHERE p.household_id = $1 AND ($2::timestamptz IS NULL OR p.occurred_at >= $2) AND ($3::timestamptz IS NULL OR p.occurred_at < $3) AND ($4::uuid IS NULL OR (p.occurred_at, p.id) < (SELECT q.occurred_at, q.id FROM purchases q WHERE q.id = $4 AND q.household_id = $1)) ORDER BY p.occurred_at DESC, p.id DESC LIMIT $5`, householdID, fromValue, toValue, afterValue, pageSize+1)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, pageSize+1)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	hasNext := len(ids) > pageSize
	if hasNext {
		ids = ids[:pageSize]
	}
	connection := &model.PurchaseConnection{Edges: make([]*model.PurchaseEdge, 0, len(ids)), TotalCount: count, PageInfo: &model.PageInfo{HasNextPage: hasNext}}
	for _, id := range ids {
		purchase, err := loadPurchase(ctx, r.DB, id)
		if err != nil {
			return nil, err
		}
		connection.Edges = append(connection.Edges, &model.PurchaseEdge{Cursor: id, Node: purchase})
	}
	if len(ids) > 0 {
		end := ids[len(ids)-1]
		connection.PageInfo.EndCursor = &end
	}
	return connection, nil
}

func (r *queryResolver) ledger(ctx context.Context, householdID string, after *string, first *int) (*model.LedgerConnection, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	pageSize := 50
	if first != nil {
		pageSize = *first
	}
	if pageSize < 1 || pageSize > 100 {
		return nil, fmt.Errorf("%w: first must be between 1 and 100", ErrBadInput)
	}
	var cursor any
	if after != nil && *after != "" {
		cursor = *after
	}
	var count int
	if err := r.DB.QueryRow(ctx, `SELECT count(*) FROM ledger_entries WHERE household_id = $1`, householdID).Scan(&count); err != nil {
		return nil, err
	}
	rows, err := r.DB.Query(ctx, `SELECT id::text, kind, occurred_at, description, amount_yen, from_member_id::text, to_member_id::text, pool_id::text, source_purchase_id::text, created_at FROM ledger_entries WHERE household_id = $1 AND ($2::uuid IS NULL OR (occurred_at, id) < (SELECT q.occurred_at, q.id FROM ledger_entries q WHERE q.id = $2 AND q.household_id = $1)) ORDER BY occurred_at DESC, id DESC LIMIT $3`, householdID, cursor, pageSize+1)
	if err != nil {
		return nil, err
	}
	type row struct {
		entry      *model.LedgerEntry
		occurredAt time.Time
	}
	entries := make([]row, 0, pageSize+1)
	for rows.Next() {
		entry := &model.LedgerEntry{Currency: "JPY"}
		var occurredAt, createdAt time.Time
		var amount int64
		if err := rows.Scan(&entry.ID, &entry.Kind, &occurredAt, &entry.Description, &amount, &entry.FromMemberID, &entry.ToMemberID, &entry.PoolID, &entry.SourcePurchaseID, &createdAt); err != nil {
			rows.Close()
			return nil, err
		}
		entry.AmountYen = graphLong(amount)
		entry.OccurredAt, entry.CreatedAt = timeString(occurredAt), timeString(createdAt)
		entries = append(entries, row{entry: entry, occurredAt: occurredAt})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	hasNext := len(entries) > pageSize
	if hasNext {
		entries = entries[:pageSize]
	}
	connection := &model.LedgerConnection{Edges: make([]*model.LedgerEdge, 0, len(entries)), TotalCount: count, PageInfo: &model.PageInfo{HasNextPage: hasNext}}
	for _, entry := range entries {
		connection.Edges = append(connection.Edges, &model.LedgerEdge{Cursor: entry.entry.ID, Node: entry.entry})
	}
	if len(entries) > 0 {
		end := entries[len(entries)-1].entry.ID
		connection.PageInfo.EndCursor = &end
	}
	return connection, nil
}

func (r *queryResolver) settlement(ctx context.Context, householdID, period string) (*model.Settlement, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	if _, err := time.Parse("2006-01", period); err != nil {
		return nil, fmt.Errorf("%w: period must use YYYY-MM", ErrBadInput)
	}
	var closedRevision int64
	var closedAt time.Time
	err := r.DB.QueryRow(ctx, `SELECT revision, created_at FROM settlement_runs WHERE household_id = $1 AND period = $2 AND status = 'CLOSED' ORDER BY revision DESC LIMIT 1`, householdID, period).Scan(&closedRevision, &closedAt)
	if err == nil {
		result := &model.Settlement{Period: period, Status: "CLOSED", Revision: int(closedRevision), Lines: make([]*model.SettlementLine, 0)}
		created := timeString(closedAt)
		result.CreatedAt = &created
		rows, err := r.DB.Query(ctx, `SELECT sl.member_id::text, m.display_name, sl.scheduled_yen, sl.paid_yen, sl.carry_yen, sl.advance_credit_yen, sl.amount_due_yen FROM settlement_lines sl JOIN settlement_runs sr ON sr.id = sl.settlement_run_id JOIN members m ON m.id = sl.member_id WHERE sr.household_id = $1 AND sr.period = $2 AND sr.revision = $3 ORDER BY m.display_name`, householdID, period, closedRevision)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			line := &model.SettlementLine{}
			if err := rows.Scan(&line.MemberID, &line.MemberName, &line.ScheduledYen, &line.PaidYen, &line.CarryYen, &line.AdvanceCreditYen, &line.AmountDueYen); err != nil {
				rows.Close()
				return nil, err
			}
			result.Lines = append(result.Lines, line)
		}
		rows.Close()
		return result, rows.Err()
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	result := &model.Settlement{Period: period, Status: "OPEN", Revision: 0, Lines: make([]*model.SettlementLine, 0)}
	inputs := make([]domain.SettlementInput, 0)
	rows, err := r.DB.Query(ctx, `SELECT m.id::text, m.display_name, COALESCE(cur.scheduled_yen, 0), COALESCE(cur.paid_yen, 0), COALESCE(prev.owed_yen, 0) FROM household_members hm JOIN members m ON m.id = hm.member_id LEFT JOIN (SELECT member_id, sum(amount_yen) scheduled_yen, sum(paid_yen) paid_yen FROM funding_obligations WHERE household_id = $1 AND period = $2 GROUP BY member_id) cur ON cur.member_id = m.id LEFT JOIN (SELECT member_id, sum(amount_yen - paid_yen) owed_yen FROM funding_obligations WHERE household_id = $1 AND period < $2 GROUP BY member_id) prev ON prev.member_id = m.id WHERE hm.household_id = $1 ORDER BY m.display_name`, householdID, period)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var input domain.SettlementInput
		if err := rows.Scan(&input.MemberID, &input.MemberName, &input.ScheduledYen, &input.PaidYen, &input.PreviousOwedYen); err != nil {
			rows.Close()
			return nil, err
		}
		inputs = append(inputs, input)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	settlement, err := domain.CalculateSettlement(inputs)
	if err != nil {
		return nil, err
	}
	for _, line := range settlement {
		result.Lines = append(result.Lines, &model.SettlementLine{MemberID: line.MemberID, MemberName: line.MemberName, ScheduledYen: graphLong(line.ScheduledYen), PaidYen: graphLong(line.PaidYen), CarryYen: graphLong(line.CarryYen), AdvanceCreditYen: graphLong(line.AdvanceCreditYen), AmountDueYen: graphLong(line.AmountDueYen)})
	}
	return result, nil
}

func (r *queryResolver) monthlyCategorySpending(ctx context.Context, householdID, from, to string) ([]*model.MonthlyCategorySpending, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	fromMonth, err := time.Parse("2006-01", from)
	if err != nil {
		return nil, fmt.Errorf("%w: from must use YYYY-MM", ErrBadInput)
	}
	toMonth, err := time.Parse("2006-01", to)
	if err != nil || toMonth.Before(fromMonth) || toMonth.After(fromMonth.AddDate(10, 0, 0)) {
		return nil, fmt.Errorf("%w: to must be within ten years after from", ErrBadInput)
	}
	endExclusive := toMonth.AddDate(0, 1, 0)
	rows, err := r.DB.Query(ctx, `
		WITH spending AS (
			SELECT date_trunc('month', p.occurred_at)::date AS period, i.category_id, sum(i.amount_yen)::bigint AS amount_yen
			FROM purchases p
			JOIN purchase_items i ON i.purchase_id = p.id
			WHERE p.household_id = $1 AND p.status IN ('POSTED', 'CLOSED') AND p.occurred_at >= $2 AND p.occurred_at < $3
			GROUP BY 1, 2
			UNION ALL
			SELECT date_trunc('month', p.occurred_at)::date AS period, i.category_id, sum(c.amount_delta_yen)::bigint AS amount_yen
			FROM corrections c
			JOIN purchases p ON p.id = c.purchase_id
			LEFT JOIN purchase_items i ON i.id = c.item_id
			WHERE c.household_id = $1 AND p.occurred_at >= $2 AND p.occurred_at < $3
			GROUP BY 1, 2
		)
		SELECT to_char(s.period, 'YYYY-MM'), s.category_id::text, c.name, sum(s.amount_yen)::bigint
		FROM spending s
		LEFT JOIN categories c ON c.id = s.category_id
		GROUP BY s.period, s.category_id, c.name
		ORDER BY s.period DESC, c.name NULLS LAST, s.category_id`, householdID, fromMonth, endExclusive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]*model.MonthlyCategorySpending, 0)
	for rows.Next() {
		entry := &model.MonthlyCategorySpending{}
		var amount int64
		if err := rows.Scan(&entry.Period, &entry.CategoryID, &entry.CategoryName, &amount); err != nil {
			return nil, err
		}
		entry.AmountYen = graphLong(amount)
		result = append(result, entry)
	}
	return result, rows.Err()
}
