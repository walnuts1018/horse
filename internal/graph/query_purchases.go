package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type purchasePagination struct {
	after  *string
	before *string
	first  *int
	last   *int
}

func (r *queryResolver) purchases(ctx context.Context, filter model.PurchaseFilter, page purchasePagination) (*model.PurchaseConnection, error) {
	if _, err := requireMember(ctx, r.DB, filter.HouseholdID); err != nil {
		return nil, err
	}
	if filter.CategoryID != nil && filter.Uncategorized != nil && *filter.Uncategorized {
		return nil, fmt.Errorf("categoryId and uncategorized cannot both be set: %w", ErrBadInput)
	}
	pageSize, backward, err := pageDirection(page.first, page.last)
	if err != nil {
		return nil, err
	}
	hhID, err := gofrsuuid.FromString(filter.HouseholdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	filters := []bob.Expression{models.Purchases.Columns.HouseholdID.EQ(psql.Arg(hhID))}
	filterItemsByCriteria := filter.AmountTarget != nil && *filter.AmountTarget == model.PurchaseAmountTargetItem
	var itemFilters []bob.Expression
	if filter.MinimumAmount != nil && filter.MaximumAmount != nil && *filter.MinimumAmount > *filter.MaximumAmount {
		return nil, fmt.Errorf("minimumAmount cannot exceed maximumAmount: %w", ErrBadInput)
	}
	if filter.MinimumAmount != nil || filter.MaximumAmount != nil {
		target := model.PurchaseAmountTargetPurchase
		if filter.AmountTarget != nil {
			target = *filter.AmountTarget
		}
		var amountFilter []bob.Expression
		if filter.MinimumAmount != nil {
			amountFilter = append(amountFilter, models.PurchaseItems.Columns.AmountYen.GTE(psql.Arg(int64(*filter.MinimumAmount))))
		}
		if filter.MaximumAmount != nil {
			amountFilter = append(amountFilter, models.PurchaseItems.Columns.AmountYen.LTE(psql.Arg(int64(*filter.MaximumAmount))))
		}
		if target == model.PurchaseAmountTargetItem {
			itemFilters = append(itemFilters, amountFilter...)
		} else {
			totalAmount := psql.Select(
				sm.Columns(psql.F("SUM", models.PurchaseItems.Columns.AmountYen)),
				sm.From(models.PurchaseItems.NameAsExpr()),
				sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(models.Purchases.Columns.ID)),
			)
			if filter.MinimumAmount != nil {
				filters = append(filters, psql.Group(totalAmount).GTE(psql.Arg(int64(*filter.MinimumAmount))))
			}
			if filter.MaximumAmount != nil {
				filters = append(filters, psql.Group(totalAmount).LTE(psql.Arg(int64(*filter.MaximumAmount))))
			}
		}
	}
	if filter.Origin != nil {
		filters = append(filters, models.Purchases.Columns.Origin.EQ(psql.Arg(string(*filter.Origin))))
	}
	if filter.HasEvidence != nil {
		evidenceLinkExists := models.SelectWhere.Purchases.R.HasEvidenceLinks().E
		confirmationExists := models.SelectWhere.Purchases.R.HasDocumentConfirmations().E
		if *filter.HasEvidence {
			filters = append(filters, psql.Or(evidenceLinkExists, confirmationExists))
		} else {
			filters = append(filters, psql.Not(psql.Or(evidenceLinkExists, confirmationExists)))
		}
	}
	if filter.From != nil {
		value, err := parseTime(*filter.From)
		if err != nil {
			return nil, err
		}
		filters = append(filters, models.Purchases.Columns.OccurredAt.GTE(psql.Arg(value)))
	}
	if filter.To != nil {
		value, err := parseTime(*filter.To)
		if err != nil {
			return nil, err
		}
		filters = append(filters, models.Purchases.Columns.OccurredAt.LT(psql.Arg(value)))
	}
	if filter.Search != nil && strings.TrimSpace(*filter.Search) != "" {
		pattern := "%" + escapeLikePattern(strings.TrimSpace(*filter.Search)) + "%"
		itemNameMatch := models.SelectWhere.Purchases.R.HasPurchaseItems(sm.Where(
			models.PurchaseItems.Columns.Name.ILike(psql.Arg(pattern)),
		)).E
		filters = append(filters, psql.Or(models.Purchases.Columns.Merchant.ILike(psql.Arg(pattern)), itemNameMatch))
		if filterItemsByCriteria {
			itemFilters = append(itemFilters, psql.Or(
				models.Purchases.Columns.Merchant.ILike(psql.Arg(pattern)),
				models.PurchaseItems.Columns.Name.ILike(psql.Arg(pattern)),
			))
		}
	}
	if filter.CategoryID != nil {
		id, err := gofrsuuid.FromString(*filter.CategoryID)
		if err != nil {
			return nil, fmt.Errorf("parse category id: %w", ErrBadInput)
		}
		categoryFilter := models.PurchaseItems.Columns.CategoryID.EQ(psql.Arg(id))
		if filterItemsByCriteria {
			itemFilters = append(itemFilters, categoryFilter)
		} else {
			filters = append(filters, models.SelectWhere.Purchases.R.HasPurchaseItems(sm.Where(categoryFilter)).E)
		}
	} else if filter.Uncategorized != nil && *filter.Uncategorized {
		categoryFilter := models.PurchaseItems.Columns.CategoryID.IsNull()
		if filterItemsByCriteria {
			itemFilters = append(itemFilters, categoryFilter)
		} else {
			filters = append(filters, models.SelectWhere.Purchases.R.HasPurchaseItems(sm.Where(categoryFilter)).E)
		}
	}
	if filter.TagID != nil {
		id, err := gofrsuuid.FromString(*filter.TagID)
		if err != nil {
			return nil, fmt.Errorf("parse tag id: %w", ErrBadInput)
		}
		tagFilter := models.SelectWhere.PurchaseItems.R.HasPurchaseItemTags(sm.Where(models.PurchaseItemTags.Columns.TagID.EQ(psql.Arg(id)))).E
		if filterItemsByCriteria {
			itemFilters = append(itemFilters, tagFilter)
		} else {
			filters = append(filters, models.SelectWhere.Purchases.R.HasPurchaseItems(sm.Where(tagFilter)).E)
		}
	}
	if filter.PaymentAccountID != nil {
		id, err := gofrsuuid.FromString(*filter.PaymentAccountID)
		if err != nil {
			return nil, fmt.Errorf("parse payment account id: %w", ErrBadInput)
		}
		filters = append(filters, models.SelectWhere.Purchases.R.HasPurchasePayments(sm.Where(models.PurchasePayments.Columns.AccountID.EQ(psql.Arg(id)))).E)
	}
	if filter.PoolID != nil {
		id, err := gofrsuuid.FromString(*filter.PoolID)
		if err != nil {
			return nil, fmt.Errorf("parse pool id: %w", ErrBadInput)
		}
		allocationFilter := models.SelectWhere.PurchaseItems.R.HasItemAllocations(sm.Where(models.ItemAllocations.Columns.PoolID.EQ(psql.Arg(id)))).E
		if filterItemsByCriteria {
			itemFilters = append(itemFilters, allocationFilter)
		} else {
			filters = append(filters, models.SelectWhere.Purchases.R.HasPurchaseItems(sm.Where(allocationFilter)).E)
		}
	}
	if filterItemsByCriteria && len(itemFilters) > 0 {
		filters = append(filters, models.SelectWhere.Purchases.R.HasPurchaseItems(sm.Where(psql.And(itemFilters...))).E)
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
	if cursorID, err := cursorID(page.after, nodeTypePurchase); err != nil {
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
	if cursorID, err := cursorID(page.before, nodeTypePurchase); err != nil {
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
	if !backward && page.after != nil && *page.after != "" {
		pageInfo.HasPreviousPage = true
	}
	if backward && page.before != nil && *page.before != "" {
		pageInfo.HasNextPage = true
	}
	connection := &model.PurchaseConnection{Edges: make([]*model.PurchaseEdge, 0, len(purchases)), TotalCount: int(count), PageInfo: pageInfo, SyncCursor: strconv.FormatInt(syncCursor, 10)}
	purchaseFields := requestedPurchaseLoadFields(ctx)
	if filterItemsByCriteria {
		purchaseFields.items = true
		purchaseFields.itemTags = purchaseFields.itemTags || filter.TagID != nil
		purchaseFields.itemAllocations = purchaseFields.itemAllocations || filter.PoolID != nil
	}
	loadedPurchases, err := loadPurchasesWithFields(ctx, r.DB, purchases, purchaseFields)
	if err != nil {
		return nil, err
	}
	for index, purchase := range loadedPurchases {
		if filterItemsByCriteria && len(itemFilters) > 0 {
			items := purchase.Items[:0]
			for _, item := range purchase.Items {
				if filter.MinimumAmount != nil && int64(item.AmountYen) < int64(*filter.MinimumAmount) {
					continue
				}
				if filter.MaximumAmount != nil && int64(item.AmountYen) > int64(*filter.MaximumAmount) {
					continue
				}
				if filter.CategoryID != nil && (item.CategoryID == nil || *item.CategoryID != *filter.CategoryID) {
					continue
				}
				if filter.Uncategorized != nil && *filter.Uncategorized && item.CategoryID != nil {
					continue
				}
				if filter.TagID != nil && !slices.ContainsFunc(item.Tags, func(tag *model.Tag) bool { return tag.ID == *filter.TagID }) {
					continue
				}
				if filter.PoolID != nil && !slices.ContainsFunc(item.Allocations, func(allocation *model.PurchaseItemAllocation) bool { return allocation.PoolID == *filter.PoolID }) {
					continue
				}
				if filter.Search != nil && strings.TrimSpace(*filter.Search) != "" &&
					!strings.Contains(strings.ToLower(purchase.Merchant), strings.ToLower(strings.TrimSpace(*filter.Search))) &&
					!strings.Contains(strings.ToLower(item.Name), strings.ToLower(strings.TrimSpace(*filter.Search))) {
					continue
				}
				items = append(items, item)
			}
			purchase.Items = items
		}
		row := purchases[index]
		connection.Edges = append(connection.Edges, &model.PurchaseEdge{Cursor: encodeCursor(nodeTypePurchase, row.ID.String()), Node: purchase})
	}
	if len(purchases) > 0 {
		start := encodeCursor(nodeTypePurchase, purchases[0].ID.String())
		end := encodeCursor(nodeTypePurchase, purchases[len(purchases)-1].ID.String())
		connection.PageInfo.StartCursor = &start
		connection.PageInfo.EndCursor = &end
	}
	return connection, nil
}

func escapeLikePattern(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}

func (r *queryResolver) purchaseCorrections(ctx context.Context, purchaseID string, first *int, after *string) (*model.PurchaseCorrectionConnection, error) {
	id, err := gofrsuuid.FromString(purchaseID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid purchase ID", ErrBadInput)
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
	return purchaseCorrectionConnection(ctx, r.DB, id, first, after)
}
