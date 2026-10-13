package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

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

func (r *queryResolver) purchaseCorrections(ctx context.Context, purchaseID string, first *int, after *string) (*model.PurchaseCorrectionConnection, error) {
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
	return paginatePurchaseCorrections(loaded.CorrectionsConnection.Edges, first, after)
}

func paginatePurchaseCorrections(edges []*model.PurchaseCorrectionEdge, first *int, after *string) (*model.PurchaseCorrectionConnection, error) {
	pageSize, _, err := pageDirection(after, nil, first, nil)
	if err != nil {
		return nil, err
	}
	startIndex := 0
	if cursor, err := cursorID(after, "PurchaseCorrection"); err != nil {
		return nil, err
	} else if cursor != nil {
		found := false
		for index, edge := range edges {
			if edge.Node.GetID() == *cursor {
				startIndex = index + 1
				found = true
				break
			}
		}
		if !found {
			return nil, ErrNotFound
		}
	}
	remaining := edges[startIndex:]
	hasNextPage := len(remaining) > pageSize
	if hasNextPage {
		remaining = remaining[:pageSize]
	}
	result := &model.PurchaseCorrectionConnection{
		Edges:      remaining,
		PageInfo:   &model.PageInfo{HasNextPage: hasNextPage, HasPreviousPage: startIndex > 0},
		TotalCount: len(edges),
	}
	if len(remaining) > 0 {
		startCursor, endCursor := remaining[0].Cursor, remaining[len(remaining)-1].Cursor
		result.PageInfo.StartCursor = &startCursor
		result.PageInfo.EndCursor = &endCursor
	}
	return result, nil
}
