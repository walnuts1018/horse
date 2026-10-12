package graph

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func queryExecutor(db *pgxpool.Pool) bob.Executor { return bobpgx.NewPool(db) }

func (r *queryResolver) viewer(ctx context.Context) (*model.Viewer, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	result := &model.Viewer{ID: user.MemberID, Subject: user.Subject, DisplayName: user.DisplayName, Households: make([]*model.Household, 0)}
	memberID, err := gofrsuuid.FromString(user.MemberID)
	if err != nil {
		return nil, fmt.Errorf("parse member id: %w", err)
	}
	memberships, err := models.HouseholdMembers.Query(sm.Where(models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberID))), sm.OrderBy(models.HouseholdMembers.Columns.JoinedAt)).All(ctx, queryExecutor(r.DB))
	if err != nil {
		return nil, err
	}
	for _, membership := range memberships {
		household, err := loadHousehold(ctx, r.DB, membership.HouseholdID.String())
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
	memberID, err := gofrsuuid.FromString(user.MemberID)
	if err != nil {
		return nil, fmt.Errorf("parse member id: %w", err)
	}
	memberships, err := models.HouseholdMembers.Query(sm.Where(models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberID))), sm.OrderBy(models.HouseholdMembers.Columns.JoinedAt)).All(ctx, queryExecutor(r.DB))
	if err != nil {
		return nil, err
	}
	result := make([]*model.Household, 0, len(memberships))
	for _, membership := range memberships {
		household, err := loadHousehold(ctx, r.DB, membership.HouseholdID.String())
		if err != nil {
			return nil, err
		}
		result = append(result, household)
	}
	return result, nil
}

func (r *queryResolver) household(ctx context.Context, id string) (*model.Household, error) {
	if _, err := requireMember(ctx, r.DB, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	household, err := loadHousehold(ctx, r.DB, id)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return household, err
}

func (r *queryResolver) allocationRules(ctx context.Context, householdID string) ([]*model.AllocationRule, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	parsedID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	rules, err := models.AllocationRules.Query(sm.Where(models.AllocationRules.Columns.HouseholdID.EQ(psql.Arg(parsedID))), sm.OrderBy(models.AllocationRules.Columns.CategoryID).NullsLast(), sm.OrderBy(models.AllocationRules.Columns.MerchantKey)).All(ctx, queryExecutor(r.DB))
	if err != nil {
		return nil, err
	}
	result := make([]*model.AllocationRule, 0, len(rules))
	for _, rule := range rules {
		loaded, err := loadAllocationRule(ctx, r.DB, rule.ID.String())
		if err != nil {
			return nil, err
		}
		result = append(result, loaded)
	}
	return result, nil
}

func loadAllocationRule(ctx context.Context, db *pgxpool.Pool, ruleID string) (*model.AllocationRule, error) {
	id, err := gofrsuuid.FromString(ruleID)
	if err != nil {
		return nil, fmt.Errorf("parse allocation rule id: %w", err)
	}
	executor := queryExecutor(db)
	rule, err := models.AllocationRules.Query(sm.Where(models.AllocationRules.Columns.ID.EQ(psql.Arg(id)))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	result := &model.AllocationRule{ID: rule.ID.String(), HouseholdID: rule.HouseholdID.String(), Revision: int(rule.Revision)}
	if rule.CategoryID.Valid {
		value := rule.CategoryID.V.String()
		result.CategoryID = &value
	}
	if rule.MerchantKey.Valid {
		result.Merchant = &rule.MerchantKey.V
	}
	if rule.ProductConceptID.Valid {
		value := rule.ProductConceptID.V.String()
		result.ProductConceptID = &value
	}
	if rule.ProductVariantID.Valid {
		value := rule.ProductVariantID.V.String()
		result.ProductVariantID = &value
	}
	switch {
	case result.ProductVariantID != nil:
		result.Scope = model.AllocationRuleScopeProductVariant
	case result.ProductConceptID != nil:
		result.Scope = model.AllocationRuleScopeProduct
	case result.CategoryID != nil:
		result.Scope = model.AllocationRuleScopeCategory
	default:
		result.Scope = model.AllocationRuleScopeMerchant
	}
	weights, err := models.AllocationRuleWeights.Query(sm.Where(models.AllocationRuleWeights.Columns.RuleID.EQ(psql.Arg(id))), sm.OrderBy(models.AllocationRuleWeights.Columns.PoolID)).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	if err := weights.LoadPool(ctx, executor); err != nil {
		return nil, err
	}
	result.Allocations = make([]*model.Allocation, 0, len(weights))
	for _, weight := range weights {
		if weight.R.Pool == nil {
			return nil, fmt.Errorf("allocation rule weight %s has no pool", weight.PoolID)
		}
		poolName := weight.R.Pool.Name
		result.Allocations = append(result.Allocations, &model.Allocation{PoolID: weight.PoolID.String(), PoolName: &poolName, Weight: graphLong(weight.Weight)})
	}
	return result, nil
}

func (r *queryResolver) purchases(ctx context.Context, householdID string, from, to *string, origin *model.PurchaseOrigin, hasEvidence *bool, after, before *string, first, last *int) (*model.PurchaseConnection, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	pageSize, backward, err := pageDirection(after, before, first, last)
	if err != nil {
		return nil, err
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	filters := []bob.Expression{models.Purchases.Columns.HouseholdID.EQ(psql.Arg(hhID))}
	if origin != nil {
		filters = append(filters, models.Purchases.Columns.Origin.EQ(psql.Arg(string(*origin))))
	}
	if hasEvidence != nil {
		links, err := models.EvidenceLinks.Query(sm.Where(models.EvidenceLinks.Columns.HouseholdID.EQ(psql.Arg(hhID)))).All(ctx, queryExecutor(r.DB))
		if err != nil {
			return nil, err
		}
		confirmations, err := models.DocumentConfirmations.Query(sm.Where(models.DocumentConfirmations.Columns.HouseholdID.EQ(psql.Arg(hhID)))).All(ctx, queryExecutor(r.DB))
		if err != nil {
			return nil, err
		}
		purchaseIDSet := make(map[gofrsuuid.UUID]struct{}, len(links)+len(confirmations))
		for _, link := range links {
			purchaseIDSet[link.PurchaseID] = struct{}{}
		}
		for _, confirmation := range confirmations {
			purchaseIDSet[confirmation.PurchaseID] = struct{}{}
		}
		purchaseIDs := make([]bob.Expression, 0, len(purchaseIDSet))
		for purchaseID := range purchaseIDSet {
			purchaseIDs = append(purchaseIDs, psql.Arg(purchaseID))
		}
		if len(purchaseIDs) == 0 {
			if *hasEvidence {
				return &model.PurchaseConnection{Edges: make([]*model.PurchaseEdge, 0), PageInfo: &model.PageInfo{}, TotalCount: 0}, nil
			}
		} else if *hasEvidence {
			filters = append(filters, models.Purchases.Columns.ID.In(purchaseIDs...))
		} else {
			filters = append(filters, psql.Not(models.Purchases.Columns.ID.In(purchaseIDs...)))
		}
	}
	if from != nil {
		value, err := parseTime(*from)
		if err != nil {
			return nil, err
		}
		filters = append(filters, models.Purchases.Columns.OccurredAt.GTE(psql.Arg(value)))
	}
	if to != nil {
		value, err := parseTime(*to)
		if err != nil {
			return nil, err
		}
		filters = append(filters, models.Purchases.Columns.OccurredAt.LT(psql.Arg(value)))
	}
	executor := queryExecutor(r.DB)
	count, err := models.Purchases.Query(sm.Where(psql.And(filters...))).Count(ctx, executor)
	if err != nil {
		return nil, err
	}
	if cursorID, err := cursorID(after, "Purchase"); err != nil {
		return nil, err
	} else if cursorID != nil {
		parsedCursorID, err := gofrsuuid.FromString(*cursorID)
		if err != nil {
			return nil, fmt.Errorf("parse purchase cursor: %w", err)
		}
		cursor, err := models.Purchases.Query(sm.Where(psql.And(models.Purchases.Columns.ID.EQ(psql.Arg(parsedCursorID)), models.Purchases.Columns.HouseholdID.EQ(psql.Arg(hhID))))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		filters = append(filters, psql.Or(models.Purchases.Columns.OccurredAt.LT(psql.Arg(cursor.OccurredAt)), psql.And(models.Purchases.Columns.OccurredAt.EQ(psql.Arg(cursor.OccurredAt)), models.Purchases.Columns.ID.LT(psql.Arg(cursor.ID)))))
	}
	if cursorID, err := cursorID(before, "Purchase"); err != nil {
		return nil, err
	} else if cursorID != nil {
		parsedCursorID, err := gofrsuuid.FromString(*cursorID)
		if err != nil {
			return nil, fmt.Errorf("parse purchase cursor: %w", err)
		}
		cursor, err := models.Purchases.Query(sm.Where(psql.And(models.Purchases.Columns.ID.EQ(psql.Arg(parsedCursorID)), models.Purchases.Columns.HouseholdID.EQ(psql.Arg(hhID))))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		filters = append(filters, psql.Or(models.Purchases.Columns.OccurredAt.GT(psql.Arg(cursor.OccurredAt)), psql.And(models.Purchases.Columns.OccurredAt.EQ(psql.Arg(cursor.OccurredAt)), models.Purchases.Columns.ID.GT(psql.Arg(cursor.ID)))))
	}
	occurredAtOrder := sm.OrderBy(models.Purchases.Columns.OccurredAt).Desc()
	IDOrder := sm.OrderBy(models.Purchases.Columns.ID).Desc()
	if backward {
		occurredAtOrder = sm.OrderBy(models.Purchases.Columns.OccurredAt)
		IDOrder = sm.OrderBy(models.Purchases.Columns.ID)
	}
	purchases, err := models.Purchases.Query(sm.Where(psql.And(filters...)), occurredAtOrder, IDOrder, sm.Limit(pageSize+1)).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	hasExtra := len(purchases) > pageSize
	if hasExtra {
		purchases = purchases[:pageSize]
	}
	if backward {
		slices.Reverse(purchases)
	}
	pageInfo := &model.PageInfo{HasPreviousPage: backward && hasExtra, HasNextPage: !backward && hasExtra}
	if !backward && after != nil && *after != "" {
		pageInfo.HasPreviousPage = true
	}
	if backward && before != nil && *before != "" {
		pageInfo.HasNextPage = true
	}
	connection := &model.PurchaseConnection{Edges: make([]*model.PurchaseEdge, 0, len(purchases)), TotalCount: int(count), PageInfo: pageInfo}
	for _, row := range purchases {
		purchase, err := loadPurchase(ctx, r.DB, row.ID.String())
		if err != nil {
			return nil, err
		}
		connection.Edges = append(connection.Edges, &model.PurchaseEdge{Cursor: encodeCursor("Purchase", row.ID.String()), Node: purchase})
	}
	if len(purchases) > 0 {
		start := encodeCursor("Purchase", purchases[0].ID.String())
		end := encodeCursor("Purchase", purchases[len(purchases)-1].ID.String())
		connection.PageInfo.StartCursor = &start
		connection.PageInfo.EndCursor = &end
	}
	return connection, nil
}

func (r *queryResolver) ledger(ctx context.Context, householdID string, after, before *string, first, last *int) (*model.LedgerConnection, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	pageSize, backward, err := pageDirection(after, before, first, last)
	if err != nil {
		return nil, err
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	executor := queryExecutor(r.DB)
	filters := []bob.Expression{models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(hhID))}
	count, err := models.LedgerEntries.Query(sm.Where(models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(hhID)))).Count(ctx, executor)
	if err != nil {
		return nil, err
	}
	if cursorID, err := cursorID(after, "LedgerEntry"); err != nil {
		return nil, err
	} else if cursorID != nil {
		parsedCursorID, err := gofrsuuid.FromString(*cursorID)
		if err != nil {
			return nil, fmt.Errorf("parse ledger cursor: %w", err)
		}
		cursor, err := models.LedgerEntries.Query(sm.Where(psql.And(models.LedgerEntries.Columns.ID.EQ(psql.Arg(parsedCursorID)), models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(hhID))))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		filters = append(filters, psql.Or(models.LedgerEntries.Columns.OccurredAt.LT(psql.Arg(cursor.OccurredAt)), psql.And(models.LedgerEntries.Columns.OccurredAt.EQ(psql.Arg(cursor.OccurredAt)), models.LedgerEntries.Columns.ID.LT(psql.Arg(cursor.ID)))))
	}
	if cursorID, err := cursorID(before, "LedgerEntry"); err != nil {
		return nil, err
	} else if cursorID != nil {
		parsedCursorID, err := gofrsuuid.FromString(*cursorID)
		if err != nil {
			return nil, fmt.Errorf("parse ledger cursor: %w", err)
		}
		cursor, err := models.LedgerEntries.Query(sm.Where(psql.And(models.LedgerEntries.Columns.ID.EQ(psql.Arg(parsedCursorID)), models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(hhID))))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		filters = append(filters, psql.Or(models.LedgerEntries.Columns.OccurredAt.GT(psql.Arg(cursor.OccurredAt)), psql.And(models.LedgerEntries.Columns.OccurredAt.EQ(psql.Arg(cursor.OccurredAt)), models.LedgerEntries.Columns.ID.GT(psql.Arg(cursor.ID)))))
	}
	occurredAtOrder := sm.OrderBy(models.LedgerEntries.Columns.OccurredAt).Desc()
	IDOrder := sm.OrderBy(models.LedgerEntries.Columns.ID).Desc()
	if backward {
		occurredAtOrder = sm.OrderBy(models.LedgerEntries.Columns.OccurredAt)
		IDOrder = sm.OrderBy(models.LedgerEntries.Columns.ID)
	}
	entries, err := models.LedgerEntries.Query(sm.Where(psql.And(filters...)), occurredAtOrder, IDOrder, sm.Limit(pageSize+1)).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	hasExtra := len(entries) > pageSize
	if hasExtra {
		entries = entries[:pageSize]
	}
	if backward {
		slices.Reverse(entries)
	}
	pageInfo := &model.PageInfo{HasPreviousPage: backward && hasExtra, HasNextPage: !backward && hasExtra}
	if !backward && after != nil && *after != "" {
		pageInfo.HasPreviousPage = true
	}
	if backward && before != nil && *before != "" {
		pageInfo.HasNextPage = true
	}
	connection := &model.LedgerConnection{Edges: make([]*model.LedgerEdge, 0, len(entries)), TotalCount: int(count), PageInfo: pageInfo}
	for _, row := range entries {
		entry := &model.LedgerEntryData{ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), Kind: row.Kind, Currency: "JPY", OccurredAt: timeString(row.OccurredAt), Description: row.Description, AmountYen: graphLong(row.AmountYen), CreatedAt: timeString(row.CreatedAt)}
		if row.FromMemberID.Valid {
			v := row.FromMemberID.V.String()
			entry.FromMemberID = &v
		}
		if row.ToMemberID.Valid {
			v := row.ToMemberID.V.String()
			entry.ToMemberID = &v
		}
		if row.PoolID.Valid {
			v := row.PoolID.V.String()
			entry.PoolID = &v
		}
		if row.SourcePurchaseID.Valid {
			v := row.SourcePurchaseID.V.String()
			entry.SourcePurchaseID = &v
		}
		variant, err := ledgerEntryVariant(ctx, r.DB, entry)
		if err != nil {
			return nil, err
		}
		connection.Edges = append(connection.Edges, &model.LedgerEdge{Cursor: encodeCursor("LedgerEntry", entry.ID), Node: variant})
	}
	if len(entries) > 0 {
		start := encodeCursor("LedgerEntry", entries[0].ID.String())
		end := encodeCursor("LedgerEntry", entries[len(entries)-1].ID.String())
		connection.PageInfo.StartCursor = &start
		connection.PageInfo.EndCursor = &end
	}
	return connection, nil
}

func (r *queryResolver) settlement(ctx context.Context, householdID, period string) (*model.Settlement, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	if _, err := parseAccountingPeriod(period); err != nil {
		return nil, fmt.Errorf("%w: period must use YYYY-MM", ErrBadInput)
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	executor := queryExecutor(r.DB)
	runs, err := models.SettlementRuns.Query(sm.Where(psql.And(models.SettlementRuns.Columns.HouseholdID.EQ(psql.Arg(hhID)), models.SettlementRuns.Columns.Period.EQ(psql.Arg(period)), models.SettlementRuns.Columns.Status.EQ(psql.Arg("CLOSED")))), sm.OrderBy(models.SettlementRuns.Columns.Revision).Desc(), sm.Limit(1)).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	if len(runs) > 0 {
		run := runs[0]
		lines, err := models.SettlementLines.Query(sm.Where(models.SettlementLines.Columns.SettlementRunID.EQ(psql.Arg(run.ID)))).All(ctx, executor)
		if err != nil {
			return nil, err
		}
		members, err := models.HouseholdMembers.Query(sm.Where(models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(hhID)))).All(ctx, executor)
		if err != nil {
			return nil, err
		}
		if err := members.LoadMember(ctx, executor); err != nil {
			return nil, err
		}
		names := make(map[gofrsuuid.UUID]string, len(members))
		for _, member := range members {
			names[member.MemberID] = member.R.Member.DisplayName
		}
		slices.SortFunc(lines, func(a, b *models.SettlementLine) int { return cmp.Compare(names[a.MemberID], names[b.MemberID]) })
		result := &model.Settlement{Period: period, Status: model.SettlementStatusClosed, Revision: int(run.Revision), Lines: make([]*model.SettlementLine, 0, len(lines))}
		created := timeString(run.CreatedAt)
		result.CreatedAt = &created
		for _, line := range lines {
			result.Lines = append(result.Lines, &model.SettlementLine{MemberID: line.MemberID.String(), MemberName: names[line.MemberID], ScheduledYen: graphLong(line.ScheduledYen), PaidYen: graphLong(line.PaidYen), CarryYen: graphLong(line.CarryYen), PriorBalanceYen: graphLong(line.CarryYen), MonthlyAdjustmentYen: graphLong(line.MonthlyAdjustmentYen), AdvanceCreditYen: graphLong(line.AdvanceCreditYen), AmountDueYen: graphLong(line.AmountDueYen)})
		}
		return result, nil
	}
	result := &model.Settlement{Period: period, Status: model.SettlementStatusOpen, Revision: 0, Lines: make([]*model.SettlementLine, 0)}
	settlement, err := calculateSettlementPreview(ctx, queryExecutor(r.DB), householdID, period)
	if err != nil {
		return nil, err
	}
	for _, line := range settlement {
		result.Lines = append(result.Lines, &model.SettlementLine{MemberID: line.MemberID, MemberName: line.MemberName, ScheduledYen: graphLong(line.ScheduledYen), PaidYen: graphLong(line.PaidYen), CarryYen: graphLong(line.CarryYen), PriorBalanceYen: graphLong(line.CarryYen), MonthlyAdjustmentYen: graphLong(line.MonthlyAdjustmentYen), AdvanceCreditYen: graphLong(line.AdvanceCreditYen), AmountDueYen: graphLong(line.AmountDueYen)})
	}
	return result, nil
}

func (r *queryResolver) monthlyCategorySpending(ctx context.Context, householdID, from, to string) ([]*model.MonthlyCategorySpending, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	fromMonth, err := parseAccountingPeriod(from)
	if err != nil {
		return nil, fmt.Errorf("%w: from must use YYYY-MM", ErrBadInput)
	}
	toMonth, err := parseAccountingPeriod(to)
	if err != nil || toMonth.Before(fromMonth) || toMonth.After(fromMonth.AddDate(10, 0, 0)) {
		return nil, fmt.Errorf("%w: to must be within ten years after from", ErrBadInput)
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	start := fromMonth.UTC()
	end := toMonth.AddDate(0, 1, 0).UTC()
	executor := queryExecutor(r.DB)
	purchases, err := models.Purchases.Query(sm.Where(psql.And(models.Purchases.Columns.HouseholdID.EQ(psql.Arg(hhID)), models.Purchases.Columns.OccurredAt.GTE(psql.Arg(start)), models.Purchases.Columns.OccurredAt.LT(psql.Arg(end)))), sm.OrderBy(models.Purchases.Columns.OccurredAt)).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	type spendingKey struct {
		period      string
		categoryID  gofrsuuid.UUID
		hasCategory bool
	}
	amounts := make(map[spendingKey]int64)
	categoryNames := make(map[gofrsuuid.UUID]string)
	for _, purchase := range purchases {
		if purchase.Status != "POSTED" && purchase.Status != "CLOSED" {
			continue
		}
		items, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchase.ID)))).All(ctx, executor)
		if err != nil {
			return nil, err
		}
		itemsByID := make(map[gofrsuuid.UUID]*models.PurchaseItem, len(items))
		for _, item := range items {
			itemsByID[item.ID] = item
			key := spendingKey{period: accountingPeriod(purchase.OccurredAt)}
			if item.CategoryID.Valid {
				key.categoryID = item.CategoryID.V
				key.hasCategory = true
			}
			amounts[key] += item.AmountYen
		}
		corrections, err := models.Corrections.Query(sm.Where(models.Corrections.Columns.PurchaseID.EQ(psql.Arg(purchase.ID)))).All(ctx, executor)
		if err != nil {
			return nil, err
		}
		for _, correction := range corrections {
			key := spendingKey{period: accountingPeriod(purchase.OccurredAt)}
			if correction.ItemID.Valid {
				if item := itemsByID[correction.ItemID.V]; item != nil && item.CategoryID.Valid {
					key.categoryID = item.CategoryID.V
					key.hasCategory = true
				}
			}
			amounts[key] += correction.AmountDeltaYen
		}
	}
	categories, err := models.Categories.Query().All(ctx, executor)
	if err != nil {
		return nil, err
	}
	for _, category := range categories {
		categoryNames[category.ID] = category.Name
	}
	keys := make([]spendingKey, 0, len(amounts))
	for key := range amounts {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b spendingKey) int {
		if n := cmp.Compare(b.period, a.period); n != 0 {
			return n
		}
		if !a.hasCategory && b.hasCategory {
			return 1
		}
		if a.hasCategory && !b.hasCategory {
			return -1
		}
		if !a.hasCategory {
			return 0
		}
		if n := cmp.Compare(categoryNames[a.categoryID], categoryNames[b.categoryID]); n != 0 {
			return n
		}
		return cmp.Compare(a.categoryID.String(), b.categoryID.String())
	})
	result := make([]*model.MonthlyCategorySpending, 0, len(keys))
	for _, key := range keys {
		entry := &model.MonthlyCategorySpending{Period: key.period, AmountYen: graphLong(amounts[key])}
		if key.hasCategory {
			id := key.categoryID.String()
			name := categoryNames[key.categoryID]
			entry.CategoryID = &id
			entry.CategoryName = &name
		}
		result = append(result, entry)
	}
	return result, nil
}

func (r *queryResolver) monthlyMemberSpending(ctx context.Context, householdID, from, to string) (*model.MonthlyMemberSpendingReport, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	fromMonth, err := parseAccountingPeriod(from)
	if err != nil {
		return nil, fmt.Errorf("%w: from must use YYYY-MM", ErrBadInput)
	}
	toMonth, err := parseAccountingPeriod(to)
	if err != nil || toMonth.Before(fromMonth) || toMonth.After(fromMonth.AddDate(10, 0, 0)) {
		return nil, fmt.Errorf("%w: to must be within ten years after from", ErrBadInput)
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	start := fromMonth.UTC()
	end := toMonth.AddDate(0, 1, 0).UTC()
	executor := queryExecutor(r.DB)
	purchases, err := models.Purchases.Query(sm.Where(psql.And(
		models.Purchases.Columns.HouseholdID.EQ(psql.Arg(hhID)),
		models.Purchases.Columns.OccurredAt.GTE(psql.Arg(start)),
		models.Purchases.Columns.OccurredAt.LT(psql.Arg(end)),
	))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	amounts := make(map[string]int64)
	memberNames := make(map[string]string)
	result := &model.MonthlyMemberSpendingReport{Items: make([]*model.MonthlyMemberSpending, 0)}
	for _, purchase := range purchases {
		if purchase.Status != string(model.PurchaseStatusPosted) && purchase.Status != string(model.PurchaseStatusClosed) {
			continue
		}
		period := accountingPeriod(purchase.OccurredAt)
		allocations, err := models.ItemAllocations.Query(sm.Where(models.ItemAllocations.Columns.PurchaseID.EQ(psql.Arg(purchase.ID)))).All(ctx, executor)
		if err != nil {
			return nil, err
		}
		snapshots, err := models.PurchaseMemberBurdens.Query(sm.Where(psql.And(
			models.PurchaseMemberBurdens.Columns.HouseholdID.EQ(psql.Arg(hhID)),
			models.PurchaseMemberBurdens.Columns.PurchaseID.EQ(psql.Arg(purchase.ID)),
		))).All(ctx, executor)
		if err != nil {
			return nil, err
		}
		covered := make(map[[2]gofrsuuid.UUID]struct{}, len(snapshots))
		coveredAmounts := make(map[[2]gofrsuuid.UUID]int64, len(snapshots))
		for _, snapshot := range snapshots {
			snapshotKey := [2]gofrsuuid.UUID{snapshot.ItemID, snapshot.PoolID}
			covered[snapshotKey] = struct{}{}
			nextCoverage, ok := checkedAdd(coveredAmounts[snapshotKey], snapshot.AmountYen)
			if !ok {
				return nil, fmt.Errorf("%w: purchase burden snapshot total is too large", ErrBadInput)
			}
			coveredAmounts[snapshotKey] = nextCoverage
			key := period + ":" + snapshot.MemberID.String()
			next, ok := checkedAdd(amounts[key], snapshot.AmountYen)
			if !ok {
				return nil, fmt.Errorf("%w: monthly member spending total is too large", ErrBadInput)
			}
			amounts[key] = next
		}
		for _, allocation := range allocations {
			if allocation.AmountYen == 0 {
				continue
			}
			allocationKey := [2]gofrsuuid.UUID{allocation.ItemID, allocation.PoolID}
			if _, ok := covered[allocationKey]; !ok || coveredAmounts[allocationKey] != allocation.AmountYen {
				result.HasUnattributedPurchases = true
			}
		}
		corrections, err := models.Corrections.Query(sm.Where(models.Corrections.Columns.PurchaseID.EQ(psql.Arg(purchase.ID)))).All(ctx, executor)
		if err != nil {
			return nil, err
		}
		for _, correction := range corrections {
			correctionRows, err := models.CorrectionMemberBurdens.Query(sm.Where(psql.And(
				models.CorrectionMemberBurdens.Columns.HouseholdID.EQ(psql.Arg(hhID)),
				models.CorrectionMemberBurdens.Columns.CorrectionID.EQ(psql.Arg(correction.ID)),
			))).All(ctx, executor)
			if err != nil {
				return nil, err
			}
			var correctionSnapshotTotal int64
			for _, row := range correctionRows {
				var ok bool
				correctionSnapshotTotal, ok = checkedAdd(correctionSnapshotTotal, row.AmountDeltaYen)
				if !ok {
					return nil, fmt.Errorf("%w: monthly correction total is too large", ErrBadInput)
				}
				key := period + ":" + row.MemberID.String()
				next, ok := checkedAdd(amounts[key], row.AmountDeltaYen)
				if !ok {
					return nil, fmt.Errorf("%w: monthly member spending total is too large", ErrBadInput)
				}
				amounts[key] = next
			}
			if correctionSnapshotTotal != correction.AmountDeltaYen {
				result.HasUnattributedPurchases = true
			}
		}
	}
	members, err := models.HouseholdMembers.Query(sm.Where(models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(hhID)))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	if err := members.LoadMember(ctx, executor); err != nil {
		return nil, err
	}
	for _, member := range members {
		memberNames[member.MemberID.String()] = member.R.Member.DisplayName
	}
	keys := make([]string, 0, len(amounts))
	for key := range amounts {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, value := range keys {
		period, memberID, ok := strings.Cut(value, ":")
		if !ok {
			return nil, fmt.Errorf("invalid member spending key")
		}
		name, exists := memberNames[memberID]
		if !exists {
			continue
		}
		result.Items = append(result.Items, &model.MonthlyMemberSpending{Period: period, MemberID: memberID, MemberName: name, AmountYen: graphLong(amounts[value])})
	}
	return result, nil
}

func (r *queryResolver) purchaseCorrections(ctx context.Context, purchaseID string) ([]*model.PurchaseCorrection, error) {
	id, err := gofrsuuid.FromString(purchaseID)
	if err != nil {
		return nil, fmt.Errorf("parse purchase id: %w", err)
	}
	purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(id)))).One(ctx, queryExecutor(r.DB))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if _, err := requireMember(ctx, r.DB, purchase.HouseholdID.String()); err != nil {
		return nil, err
	}
	loaded, err := loadPurchase(ctx, r.DB, purchaseID)
	if err != nil {
		return nil, err
	}
	return loaded.Corrections, nil
}

func (r *queryResolver) loanBalances(ctx context.Context, householdID string) ([]*model.LoanBalance, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	executor := queryExecutor(r.DB)
	transfers, err := models.Transfers.Query(sm.Where(models.Transfers.Columns.HouseholdID.EQ(psql.Arg(hhID)))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	repaidByLoan := make(map[gofrsuuid.UUID]int64)
	for _, transfer := range transfers {
		if transfer.Kind == "REPAYMENT" && transfer.LoanID.Valid {
			repaidByLoan[transfer.LoanID.V] += transfer.AmountYen
		}
	}
	loans := make([]*models.Transfer, 0)
	for _, transfer := range transfers {
		if transfer.Kind == "LOAN" {
			loans = append(loans, transfer)
		}
	}
	ledgerEntries, err := models.LedgerEntries.Query(sm.Where(models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(hhID)))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	ledgerByID := make(map[gofrsuuid.UUID]*models.LedgerEntry, len(ledgerEntries))
	for _, entry := range ledgerEntries {
		ledgerByID[entry.ID] = entry
	}
	slices.SortFunc(loans, func(a, b *models.Transfer) int {
		left, right := ledgerByID[a.LedgerEntryID], ledgerByID[b.LedgerEntryID]
		if left != nil && right != nil {
			if n := right.CreatedAt.Compare(left.CreatedAt); n != 0 {
				return n
			}
		}
		return cmp.Compare(a.ID.String(), b.ID.String())
	})
	result := make([]*model.LoanBalance, 0, len(loans))
	for _, loan := range loans {
		ledger := ledgerByID[loan.LedgerEntryID]
		if ledger == nil {
			return nil, fmt.Errorf("loan transfer %s has no ledger entry", loan.ID)
		}
		balance := &model.LoanBalance{LoanID: loan.ID.String(), LenderMemberID: loan.FromMemberID.String(), BorrowerMemberID: loan.ToMemberID.String(), PrincipalYen: graphLong(loan.AmountYen), RepaidYen: graphLong(repaidByLoan[loan.ID]), BalanceYen: graphLong(loan.AmountYen - repaidByLoan[loan.ID]), CreatedAt: timeString(ledger.CreatedAt)}
		result = append(result, balance)
	}
	return result, nil
}
