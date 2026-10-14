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

type purchaseAmountFilter struct {
	minimum *model.Long
	maximum *model.Long
	target  *model.PurchaseAmountTarget
}

func (r *queryResolver) purchases(ctx context.Context, householdID string, from, to *string, origin *model.PurchaseOrigin, hasEvidence *bool, search *string, categoryID *string, uncategorized *bool, tagID *string, paymentAccountID *string, poolID *string, after, before *string, first, last *int, amountFilters ...purchaseAmountFilter) (*model.PurchaseConnection, error) {
	var minimumAmount, maximumAmount *model.Long
	var amountTarget *model.PurchaseAmountTarget
	if len(amountFilters) > 0 {
		minimumAmount = amountFilters[0].minimum
		maximumAmount = amountFilters[0].maximum
		amountTarget = amountFilters[0].target
	}
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	if categoryID != nil && uncategorized != nil && *uncategorized {
		return nil, fmt.Errorf("categoryId and uncategorized cannot both be set: %w", ErrBadInput)
	}
	pageSize, backward, err := pageDirection(first, last)
	if err != nil {
		return nil, err
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	filters := []bob.Expression{models.Purchases.Columns.HouseholdID.EQ(psql.Arg(hhID))}
	filterItemsByCriteria := amountTarget != nil && *amountTarget == model.PurchaseAmountTargetItem
	var itemFilters []bob.Expression
	if minimumAmount != nil && maximumAmount != nil && *minimumAmount > *maximumAmount {
		return nil, fmt.Errorf("minimumAmount cannot exceed maximumAmount: %w", ErrBadInput)
	}
	if minimumAmount != nil || maximumAmount != nil {
		target := model.PurchaseAmountTargetPurchase
		if amountTarget != nil {
			target = *amountTarget
		}
		var amountFilter []bob.Expression
		if minimumAmount != nil {
			amountFilter = append(amountFilter, models.PurchaseItems.Columns.AmountYen.GTE(psql.Arg(int64(*minimumAmount))))
		}
		if maximumAmount != nil {
			amountFilter = append(amountFilter, models.PurchaseItems.Columns.AmountYen.LTE(psql.Arg(int64(*maximumAmount))))
		}
		if target == model.PurchaseAmountTargetItem {
			itemFilters = append(itemFilters, amountFilter...)
		} else {
			totalAmount := psql.Select(
				sm.Columns(psql.F("SUM", models.PurchaseItems.Columns.AmountYen)),
				sm.From(models.PurchaseItems.NameAsExpr()),
				sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(models.Purchases.Columns.ID)),
			)
			if minimumAmount != nil {
				filters = append(filters, psql.Group(totalAmount).GTE(psql.Arg(int64(*minimumAmount))))
			}
			if maximumAmount != nil {
				filters = append(filters, psql.Group(totalAmount).LTE(psql.Arg(int64(*maximumAmount))))
			}
		}
	}
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
	if search != nil && strings.TrimSpace(*search) != "" {
		pattern := "%" + escapeLikePattern(strings.TrimSpace(*search)) + "%"
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
	if categoryID != nil {
		id, err := gofrsuuid.FromString(*categoryID)
		if err != nil {
			return nil, fmt.Errorf("parse category id: %w", ErrBadInput)
		}
		categoryFilter := models.PurchaseItems.Columns.CategoryID.EQ(psql.Arg(id))
		if filterItemsByCriteria {
			itemFilters = append(itemFilters, categoryFilter)
		} else {
			filters = append(filters, models.SelectWhere.Purchases.R.HasPurchaseItems(sm.Where(categoryFilter)).E)
		}
	} else if uncategorized != nil && *uncategorized {
		categoryFilter := models.PurchaseItems.Columns.CategoryID.IsNull()
		if filterItemsByCriteria {
			itemFilters = append(itemFilters, categoryFilter)
		} else {
			filters = append(filters, models.SelectWhere.Purchases.R.HasPurchaseItems(sm.Where(categoryFilter)).E)
		}
	}
	if tagID != nil {
		id, err := gofrsuuid.FromString(*tagID)
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
	if paymentAccountID != nil {
		id, err := gofrsuuid.FromString(*paymentAccountID)
		if err != nil {
			return nil, fmt.Errorf("parse payment account id: %w", ErrBadInput)
		}
		filters = append(filters, models.SelectWhere.Purchases.R.HasPurchasePayments(sm.Where(models.PurchasePayments.Columns.AccountID.EQ(psql.Arg(id)))).E)
	}
	if poolID != nil {
		id, err := gofrsuuid.FromString(*poolID)
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
	if cursorID, err := cursorID(after, nodeTypePurchase); err != nil {
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
	if cursorID, err := cursorID(before, nodeTypePurchase); err != nil {
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
		if filterItemsByCriteria && len(itemFilters) > 0 {
			items := purchase.Items[:0]
			for _, item := range purchase.Items {
				if minimumAmount != nil && int64(item.AmountYen) < int64(*minimumAmount) {
					continue
				}
				if maximumAmount != nil && int64(item.AmountYen) > int64(*maximumAmount) {
					continue
				}
				if categoryID != nil && (item.CategoryID == nil || *item.CategoryID != *categoryID) {
					continue
				}
				if uncategorized != nil && *uncategorized && item.CategoryID != nil {
					continue
				}
				if tagID != nil && !slices.ContainsFunc(item.Tags, func(tag *model.Tag) bool { return tag.ID == *tagID }) {
					continue
				}
				if poolID != nil && !slices.ContainsFunc(item.Allocations, func(allocation *model.PurchaseItemAllocation) bool { return allocation.PoolID == *poolID }) {
					continue
				}
				if search != nil && strings.TrimSpace(*search) != "" &&
					!strings.Contains(strings.ToLower(purchase.Merchant), strings.ToLower(strings.TrimSpace(*search))) &&
					!strings.Contains(strings.ToLower(item.Name), strings.ToLower(strings.TrimSpace(*search))) {
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
	decodedID, err := decodeNodeID(purchaseID, nodeTypePurchase)
	if err != nil {
		return nil, err
	}
	id, err := gofrsuuid.FromString(decodedID)
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
	return purchaseCorrectionConnection(ctx, r.DB, id, first, after)
}
