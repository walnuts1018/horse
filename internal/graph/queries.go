package graph

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"

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

func (r *queryResolver) allocationRules(ctx context.Context, householdID string, after, before *string, first, last *int) (*model.AllocationRuleConnection, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	pageSize, backward, err := pageDirection(after, before, first, last)
	if err != nil {
		return nil, err
	}
	parsedID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	executor := queryExecutor(r.DB)
	filters := []bob.Expression{models.AllocationRules.Columns.HouseholdID.EQ(psql.Arg(parsedID))}
	count, err := models.AllocationRules.Query(sm.Where(filters[0])).Count(ctx, executor)
	if err != nil {
		return nil, err
	}
	for _, boundary := range []struct {
		cursor   *string
		isBefore bool
	}{{cursor: after}, {cursor: before, isBefore: true}} {
		id, err := cursorID(boundary.cursor, "AllocationRule")
		if err != nil {
			return nil, err
		}
		if id == nil {
			continue
		}
		parsedCursorID, err := gofrsuuid.FromString(*id)
		if err != nil {
			return nil, fmt.Errorf("parse allocation rule cursor: %w", err)
		}
		cursor, err := models.AllocationRules.Query(sm.Where(psql.And(
			models.AllocationRules.Columns.ID.EQ(psql.Arg(parsedCursorID)),
			models.AllocationRules.Columns.HouseholdID.EQ(psql.Arg(parsedID)),
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		filters = append(filters, allocationRuleCursorFilter(cursor, boundary.isBefore))
	}
	var rules []*models.AllocationRule
	if backward {
		rules, err = models.AllocationRules.Query(
			sm.Where(psql.And(filters...)),
			sm.OrderBy(models.AllocationRules.Columns.CategoryID).Desc().NullsFirst(),
			sm.OrderBy(models.AllocationRules.Columns.MerchantKey).Desc().NullsFirst(),
			sm.OrderBy(models.AllocationRules.Columns.ID).Desc(),
			sm.Limit(pageSize+1),
		).All(ctx, executor)
	} else {
		rules, err = models.AllocationRules.Query(
			sm.Where(psql.And(filters...)),
			sm.OrderBy(models.AllocationRules.Columns.CategoryID).NullsLast(),
			sm.OrderBy(models.AllocationRules.Columns.MerchantKey).NullsLast(),
			sm.OrderBy(models.AllocationRules.Columns.ID),
			sm.Limit(pageSize+1),
		).All(ctx, executor)
	}
	if err != nil {
		return nil, err
	}
	hasNextPage := len(rules) > pageSize
	if hasNextPage {
		rules = rules[:pageSize]
	}
	if backward {
		slices.Reverse(rules)
	}
	connection := &model.AllocationRuleConnection{Edges: make([]*model.AllocationRuleEdge, 0, len(rules)), TotalCount: int(count), PageInfo: &model.PageInfo{HasNextPage: backward && before != nil && *before != "" || !backward && hasNextPage, HasPreviousPage: backward && hasNextPage || !backward && after != nil && *after != ""}}
	for _, rule := range rules {
		loaded, err := loadAllocationRule(ctx, r.DB, rule.ID.String())
		if err != nil {
			return nil, err
		}
		connection.Edges = append(connection.Edges, &model.AllocationRuleEdge{Cursor: encodeCursor("AllocationRule", rule.ID.String()), Node: loaded})
	}
	if len(rules) > 0 {
		start, end := encodeCursor("AllocationRule", rules[0].ID.String()), encodeCursor("AllocationRule", rules[len(rules)-1].ID.String())
		connection.PageInfo.StartCursor, connection.PageInfo.EndCursor = &start, &end
	}
	return connection, nil
}

func allocationRuleCursorFilter(cursor *models.AllocationRule, isBefore bool) bob.Expression {
	if cursor.CategoryID.Valid {
		if isBefore {
			return psql.Or(
				models.AllocationRules.Columns.CategoryID.LT(psql.Arg(cursor.CategoryID.V)),
				psql.And(models.AllocationRules.Columns.CategoryID.EQ(psql.Arg(cursor.CategoryID.V)), models.AllocationRules.Columns.ID.LT(psql.Arg(cursor.ID))),
			)
		}
		return psql.Or(
			models.AllocationRules.Columns.CategoryID.GT(psql.Arg(cursor.CategoryID.V)),
			psql.And(models.AllocationRules.Columns.CategoryID.EQ(psql.Arg(cursor.CategoryID.V)), models.AllocationRules.Columns.ID.GT(psql.Arg(cursor.ID))),
			models.AllocationRules.Columns.CategoryID.IsNull(),
		)
	}
	if cursor.MerchantKey.Valid {
		if isBefore {
			return psql.And(
				models.AllocationRules.Columns.CategoryID.IsNull(),
				models.AllocationRules.Columns.MerchantKey.IsNotNull(),
				psql.Or(
					models.AllocationRules.Columns.MerchantKey.LT(psql.Arg(cursor.MerchantKey.V)),
					psql.And(models.AllocationRules.Columns.MerchantKey.EQ(psql.Arg(cursor.MerchantKey.V)), models.AllocationRules.Columns.ID.LT(psql.Arg(cursor.ID))),
				),
			)
		}
		return psql.And(
			models.AllocationRules.Columns.CategoryID.IsNull(),
			psql.Or(
				models.AllocationRules.Columns.MerchantKey.GT(psql.Arg(cursor.MerchantKey.V)),
				psql.And(models.AllocationRules.Columns.MerchantKey.EQ(psql.Arg(cursor.MerchantKey.V)), models.AllocationRules.Columns.ID.GT(psql.Arg(cursor.ID))),
				models.AllocationRules.Columns.MerchantKey.IsNull(),
			),
		)
	}
	if isBefore {
		return psql.And(
			models.AllocationRules.Columns.CategoryID.IsNull(),
			models.AllocationRules.Columns.MerchantKey.IsNull(),
			models.AllocationRules.Columns.ID.LT(psql.Arg(cursor.ID)),
		)
	}
	return psql.And(
		models.AllocationRules.Columns.CategoryID.IsNull(),
		models.AllocationRules.Columns.MerchantKey.IsNull(),
		models.AllocationRules.Columns.ID.GT(psql.Arg(cursor.ID)),
	)
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
		category, err := models.Categories.Query(sm.Where(psql.And(
			models.Categories.Columns.ID.EQ(psql.Arg(rule.CategoryID.V)),
			models.Categories.Columns.HouseholdID.EQ(psql.Arg(rule.HouseholdID)),
		))).One(ctx, executor)
		if err != nil {
			return nil, err
		}
		var parentID *string
		if category.ParentID.Valid {
			parentID = new(category.ParentID.V.String())
		}
		result.Target = &model.Category{ID: category.ID.String(), Name: category.Name, ParentID: parentID, Archived: category.Archived}
	} else if rule.ProductConceptID.Valid {
		concept, err := models.ProductConcepts.Query(sm.Where(psql.And(
			models.ProductConcepts.Columns.ID.EQ(psql.Arg(rule.ProductConceptID.V)),
			models.ProductConcepts.Columns.HouseholdID.EQ(psql.Arg(rule.HouseholdID)),
		))).One(ctx, executor)
		if err != nil {
			return nil, err
		}
		var categoryID *string
		if concept.CategoryID.Valid {
			categoryID = new(concept.CategoryID.V.String())
		}
		result.Target = &model.ProductConcept{
			ID: concept.ID.String(), HouseholdID: concept.HouseholdID.String(), Name: concept.Name,
			NormalizedName: concept.NormalizedName, CategoryID: categoryID, Archived: concept.Archived,
			Revision: int(concept.Revision),
		}
	} else if rule.ProductVariantID.Valid {
		variant, err := models.ProductVariants.Query(sm.Where(psql.And(
			models.ProductVariants.Columns.ID.EQ(psql.Arg(rule.ProductVariantID.V)),
			models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(rule.HouseholdID)),
		))).One(ctx, executor)
		if err != nil {
			return nil, err
		}
		var barcode *string
		if variant.Barcode.Valid {
			barcode = new(variant.Barcode.V)
		}
		result.Target = &model.ProductVariant{
			ID: variant.ID.String(), HouseholdID: variant.HouseholdID.String(), ConceptID: variant.ConceptID.String(),
			Name: variant.Name, NormalizedName: variant.NormalizedName, Brand: new(variant.Brand),
			PackageSize: new(variant.PackageSize), Barcode: barcode, Archived: variant.Archived,
			Revision: int(variant.Revision),
		}
	} else if rule.MerchantKey.Valid {
		result.Target = model.MerchantAllocationRuleTarget{Name: rule.MerchantKey.V}
	} else {
		return nil, fmt.Errorf("allocation rule %s has no target", rule.ID)
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
		result.Allocations = append(result.Allocations, &model.Allocation{PoolID: weight.PoolID.String(), PoolName: weight.R.Pool.Name, Weight: graphLong(weight.Weight)})
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
		evidenceLinkExists := models.SelectWhere.Purchases.R.HasEvidenceLinks().E
		confirmationExists := models.SelectWhere.Purchases.R.HasDocumentConfirmations().E
		if *hasEvidence {
			filters = append(filters, psql.Or(evidenceLinkExists, confirmationExists))
		} else {
			filters = append(filters, psql.Not(psql.Or(evidenceLinkExists, confirmationExists)))
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
	syncCursor, err := latestHouseholdEventCursor(ctx, r.DB, hhID)
	if err != nil {
		return nil, err
	}
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
	connection := &model.PurchaseConnection{Edges: make([]*model.PurchaseEdge, 0, len(purchases)), TotalCount: int(count), PageInfo: pageInfo, SyncCursor: strconv.FormatInt(syncCursor, 10)}
	loadedPurchases, err := loadPurchases(ctx, r.DB, purchases)
	if err != nil {
		return nil, err
	}
	for index, purchase := range loadedPurchases {
		row := purchases[index]
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
		entry := &model.LedgerEntryData{ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), Kind: row.Kind, Currency: "JPY", OccurredAt: string(timeString(row.OccurredAt)), Description: row.Description, AmountYen: graphLong(row.AmountYen), CreatedAt: string(timeString(row.CreatedAt))}
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
		result := &model.Settlement{HouseholdID: householdID, Period: model.YearMonth(period), Status: model.SettlementStatusClosed, Revision: int(run.Revision), Lines: make([]*model.SettlementLine, 0, len(lines))}
		created := timeString(run.CreatedAt)
		result.CreatedAt = &created
		for _, line := range lines {
			result.Lines = append(result.Lines, &model.SettlementLine{MemberID: line.MemberID.String(), MemberName: names[line.MemberID], ScheduledYen: graphLong(line.ScheduledYen), PaidYen: graphLong(line.PaidYen), CarryYen: graphLong(line.CarryYen), PriorBalanceYen: graphLong(line.CarryYen), MonthlyAdjustmentYen: graphLong(line.MonthlyAdjustmentYen), AdvanceCreditYen: graphLong(line.AdvanceCreditYen), AmountDueYen: graphLong(line.AmountDueYen)})
		}
		return result, nil
	}
	result := &model.Settlement{HouseholdID: householdID, Period: model.YearMonth(period), Status: model.SettlementStatusOpen, Revision: 0, Lines: make([]*model.SettlementLine, 0)}
	settlement, err := calculateSettlementPreview(ctx, queryExecutor(r.DB), householdID, period)
	if err != nil {
		return nil, err
	}
	for _, line := range settlement {
		result.Lines = append(result.Lines, &model.SettlementLine{MemberID: line.MemberID, MemberName: line.MemberName, ScheduledYen: graphLong(line.ScheduledYen), PaidYen: graphLong(line.PaidYen), CarryYen: graphLong(line.CarryYen), PriorBalanceYen: graphLong(line.CarryYen), MonthlyAdjustmentYen: graphLong(line.MonthlyAdjustmentYen), AdvanceCreditYen: graphLong(line.AdvanceCreditYen), AmountDueYen: graphLong(line.AmountDueYen)})
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

func (r *queryResolver) loanBalances(ctx context.Context, householdID string, after, before *string, first, last *int) (*model.LoanBalanceConnection, error) {
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
	transfers, err := models.Transfers.Query(sm.Where(models.Transfers.Columns.HouseholdID.EQ(psql.Arg(hhID)))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	repaidByLoan := make(map[gofrsuuid.UUID]int64)
	loans := make([]*models.Transfer, 0)
	for _, transfer := range transfers {
		if transfer.Kind == "REPAYMENT" && transfer.LoanID.Valid {
			total, ok := checkedAdd(repaidByLoan[transfer.LoanID.V], transfer.AmountYen)
			if !ok {
				return nil, fmt.Errorf("loan %s repayment total overflow", transfer.LoanID.V)
			}
			repaidByLoan[transfer.LoanID.V] = total
		}
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
	loanCount := len(loans)
	start, end := 0, len(loans)
	if id, err := cursorID(after, "LoanBalance"); err != nil {
		return nil, err
	} else if id != nil {
		cursorIndex := slices.IndexFunc(loans, func(loan *models.Transfer) bool { return loan.ID.String() == *id })
		if cursorIndex < 0 {
			return nil, ErrNotFound
		}
		start = cursorIndex + 1
	}
	if id, err := cursorID(before, "LoanBalance"); err != nil {
		return nil, err
	} else if id != nil {
		cursorIndex := slices.IndexFunc(loans, func(loan *models.Transfer) bool { return loan.ID.String() == *id })
		if cursorIndex < 0 {
			return nil, ErrNotFound
		}
		end = cursorIndex
	}
	if start > end {
		return nil, fmt.Errorf("%w: cursor range is empty", ErrBadInput)
	}
	pageStart, pageEnd := start, end
	if backward {
		pageStart = max(start, end-pageSize)
	} else {
		pageEnd = min(end, start+pageSize)
	}
	pageLoans := loans[pageStart:pageEnd]
	connection := &model.LoanBalanceConnection{Edges: make([]*model.LoanBalanceEdge, 0, len(pageLoans)), TotalCount: loanCount, PageInfo: &model.PageInfo{HasNextPage: pageEnd < loanCount, HasPreviousPage: pageStart > 0}}
	for _, loan := range pageLoans {
		ledger := ledgerByID[loan.LedgerEntryID]
		if ledger == nil {
			return nil, fmt.Errorf("loan transfer %s has no ledger entry", loan.ID)
		}
		globalID, err := encodeNodeID("LoanBalance", loan.ID.String())
		if err != nil {
			return nil, err
		}
		balance := &model.LoanBalance{ID: globalID, LenderMemberID: loan.FromMemberID.String(), BorrowerMemberID: loan.ToMemberID.String(), PrincipalYen: graphLong(loan.AmountYen), RepaidYen: graphLong(repaidByLoan[loan.ID]), BalanceYen: graphLong(loan.AmountYen - repaidByLoan[loan.ID]), CreatedAt: timeString(ledger.CreatedAt)}
		connection.Edges = append(connection.Edges, &model.LoanBalanceEdge{Cursor: encodeCursor("LoanBalance", loan.ID.String()), Node: balance})
	}
	if len(pageLoans) > 0 {
		startCursor, endCursor := encodeCursor("LoanBalance", pageLoans[0].ID.String()), encodeCursor("LoanBalance", pageLoans[len(pageLoans)-1].ID.String())
		connection.PageInfo.StartCursor, connection.PageInfo.EndCursor = &startCursor, &endCursor
	}
	return connection, nil
}
