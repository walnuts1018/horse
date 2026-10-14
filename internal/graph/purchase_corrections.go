package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/scan"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type purchaseCorrectionAggregate struct {
	PurchaseID gofrsuuid.UUID `db:"purchase_id"`
	AmountYen  int64          `db:"-"`
	Count      int64          `db:"correction_count"`
}

type purchaseCorrectionAggregateRow struct {
	PurchaseID gofrsuuid.UUID `db:"purchase_id"`
	AmountYen  string         `db:"amount_yen"`
	Count      int64          `db:"correction_count"`
}

type purchaseItemCorrectionAggregate struct {
	ItemID    gofrsuuid.UUID `db:"item_id"`
	AmountYen string         `db:"amount_yen"`
}

type purchaseCorrectionCursor struct {
	CreatedAt time.Time
	ID        gofrsuuid.UUID
	Legacy    bool
}

func aggregatePurchaseCorrections(ctx context.Context, db *pgxpool.Pool, purchases models.PurchaseSlice) (map[gofrsuuid.UUID]purchaseCorrectionAggregate, error) {
	if len(purchases) == 0 {
		return map[gofrsuuid.UUID]purchaseCorrectionAggregate{}, nil
	}
	purchaseIDArgs := make([]bob.Expression, 0, len(purchases))
	for _, purchase := range purchases {
		purchaseIDArgs = append(purchaseIDArgs, psql.Arg(purchase.ID))
	}
	rows, err := bob.All(ctx, bobpgx.NewPool(db), psql.Select(
		sm.Columns(
			models.Corrections.Columns.PurchaseID.As("purchase_id"),
			psql.F("sum", models.Corrections.Columns.AmountDeltaYen)().As("amount_yen"),
			psql.F("count", models.Corrections.Columns.ID)().As("correction_count"),
		),
		sm.From(models.Corrections.NameExpr()),
		sm.Where(models.Corrections.Columns.PurchaseID.In(purchaseIDArgs...)),
		sm.GroupBy(models.Corrections.Columns.PurchaseID),
	), scan.StructMapper[purchaseCorrectionAggregateRow]())
	if err != nil {
		return nil, err
	}
	result := make(map[gofrsuuid.UUID]purchaseCorrectionAggregate, len(rows))
	for _, row := range rows {
		amount, err := strconv.ParseInt(row.AmountYen, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("purchase %s correction total exceeds int64: %w", row.PurchaseID, err)
		}
		result[row.PurchaseID] = purchaseCorrectionAggregate{PurchaseID: row.PurchaseID, AmountYen: amount, Count: row.Count}
	}
	return result, nil
}

func aggregatePurchaseItemCorrections(ctx context.Context, db *pgxpool.Pool, items models.PurchaseItemSlice) (map[gofrsuuid.UUID]int64, error) {
	if len(items) == 0 {
		return map[gofrsuuid.UUID]int64{}, nil
	}
	itemIDArgs := make([]bob.Expression, 0, len(items))
	for _, item := range items {
		itemIDArgs = append(itemIDArgs, psql.Arg(item.ID))
	}
	rows, err := bob.All(ctx, bobpgx.NewPool(db), psql.Select(
		sm.Columns(
			models.Corrections.Columns.ItemID.As("item_id"),
			psql.F("sum", models.Corrections.Columns.AmountDeltaYen)().As("amount_yen"),
		),
		sm.From(models.Corrections.NameExpr()),
		sm.Where(psql.And(
			models.Corrections.Columns.ItemID.In(itemIDArgs...),
			models.Corrections.Columns.ItemID.IsNotNull(),
		)),
		sm.GroupBy(models.Corrections.Columns.ItemID),
	), scan.StructMapper[purchaseItemCorrectionAggregate]())
	if err != nil {
		return nil, err
	}
	result := make(map[gofrsuuid.UUID]int64, len(rows))
	for _, row := range rows {
		amount, err := strconv.ParseInt(row.AmountYen, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("purchase item %s correction total exceeds int64: %w", row.ItemID, err)
		}
		result[row.ItemID] = amount
	}
	return result, nil
}

func parsePurchaseCorrectionCursor(value string) (purchaseCorrectionCursor, error) {
	createdAtValue, correctionIDValue, ok := strings.Cut(value, "|")
	if !ok {
		correctionID, err := gofrsuuid.FromString(value)
		if err != nil {
			return purchaseCorrectionCursor{}, fmt.Errorf("%w: invalid purchase correction cursor", ErrBadInput)
		}
		return purchaseCorrectionCursor{ID: correctionID, Legacy: true}, nil
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtValue)
	if err != nil {
		return purchaseCorrectionCursor{}, fmt.Errorf("%w: invalid purchase correction cursor", ErrBadInput)
	}
	correctionID, err := gofrsuuid.FromString(correctionIDValue)
	if err != nil {
		return purchaseCorrectionCursor{}, fmt.Errorf("%w: invalid purchase correction cursor", ErrBadInput)
	}
	return purchaseCorrectionCursor{CreatedAt: createdAt, ID: correctionID}, nil
}

func purchaseCorrectionEdges(ctx context.Context, db *pgxpool.Pool, purchaseID gofrsuuid.UUID, corrections models.CorrectionSlice) ([]*model.PurchaseCorrectionEdge, error) {
	executor := bobpgx.NewPool(db)
	if err := corrections.LoadJournal(ctx, executor); err != nil {
		return nil, err
	}
	if err := corrections.LoadCorrectionPayments(ctx, executor, sm.OrderBy(models.CorrectionPayments.Columns.LineNo)); err != nil {
		return nil, err
	}
	payments := make(models.CorrectionPaymentSlice, 0)
	for _, correction := range corrections {
		payments = append(payments, correction.R.CorrectionPayments...)
	}
	if err := payments.LoadAccount(ctx, executor); err != nil {
		return nil, err
	}
	purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseID)))).One(ctx, executor)
	if err != nil {
		return nil, err
	}
	edges := make([]*model.PurchaseCorrectionEdge, 0, len(corrections))
	for _, stored := range corrections {
		if stored.R.Journal == nil {
			return nil, fmt.Errorf("correction %s has no journal", stored.ID)
		}
		correctionID := stored.ID.String()
		settlementMonth := model.YearMonth(accountingPeriod(purchase.OccurredAt))
		if stored.EffectivePeriod.Valid {
			settlementMonth = model.YearMonth(stored.EffectivePeriod.V)
		}
		allocations := make([]*model.PurchasePayment, 0, len(stored.R.CorrectionPayments))
		for _, payment := range stored.R.CorrectionPayments {
			allocations = append(allocations, &model.PurchasePayment{
				PaymentAccountID: payment.AccountID.String(), AmountYen: graphLong(payment.AmountYen),
				Account: graphAccount(payment.R.Account),
			})
		}
		var node model.PurchaseCorrection
		if !stored.ItemID.Valid {
			node = &model.PurchaseWideCorrection{
				ID: correctionID, AmountDeltaYen: graphLong(stored.AmountDeltaYen), Reason: stored.Reason,
				OccurredAt: timeString(stored.R.Journal.OccurredAt), SettlementMonth: settlementMonth,
				PaymentAllocations: allocations,
			}
		} else {
			node = &model.PurchaseItemCorrection{
				ID: correctionID, ItemID: stored.ItemID.V.String(), AmountDeltaYen: graphLong(stored.AmountDeltaYen),
				Reason: stored.Reason, OccurredAt: timeString(stored.R.Journal.OccurredAt),
				SettlementMonth: settlementMonth, PaymentAllocations: allocations,
			}
		}
		cursor := encodeCursor(nodeTypePurchaseCorrection, stored.CreatedAt.UTC().Format(time.RFC3339Nano)+"|"+correctionID)
		edges = append(edges, &model.PurchaseCorrectionEdge{Cursor: cursor, Node: node})
	}
	return edges, nil
}

func purchaseCorrectionConnection(ctx context.Context, db *pgxpool.Pool, purchaseID gofrsuuid.UUID, first *int, after *string) (*model.PurchaseCorrectionConnection, error) {
	pageSize, _, err := pageDirection(first, nil)
	if err != nil {
		return nil, err
	}
	executor := bobpgx.NewPool(db)
	filters := []bob.Expression{models.Corrections.Columns.PurchaseID.EQ(psql.Arg(purchaseID))}
	var cursor *purchaseCorrectionCursor
	if value, err := cursorID(after, nodeTypePurchaseCorrection); err != nil {
		return nil, err
	} else if value != nil {
		parsed, err := parsePurchaseCorrectionCursor(*value)
		if err != nil {
			return nil, err
		}
		row, err := models.Corrections.Query(sm.Where(psql.And(
			models.Corrections.Columns.ID.EQ(psql.Arg(parsed.ID)),
			models.Corrections.Columns.PurchaseID.EQ(psql.Arg(purchaseID)),
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) || err == nil && !parsed.Legacy && !row.CreatedAt.Equal(parsed.CreatedAt) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if parsed.Legacy {
			parsed.CreatedAt = row.CreatedAt
		}
		cursor = &parsed
		filters = append(filters, psql.Or(
			models.Corrections.Columns.CreatedAt.GT(psql.Arg(cursor.CreatedAt)),
			psql.And(
				models.Corrections.Columns.CreatedAt.EQ(psql.Arg(cursor.CreatedAt)),
				models.Corrections.Columns.ID.GT(psql.Arg(cursor.ID)),
			),
		))
	}
	baseFilter := models.Corrections.Columns.PurchaseID.EQ(psql.Arg(purchaseID))
	totalCount, err := models.Corrections.Query(sm.Where(baseFilter)).Count(ctx, executor)
	if err != nil {
		return nil, err
	}
	rows, err := models.Corrections.Query(
		sm.Where(psql.And(filters...)),
		sm.OrderBy(models.Corrections.Columns.CreatedAt),
		sm.OrderBy(models.Corrections.Columns.ID),
		sm.Limit(pageSize+1),
	).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	hasNextPage := len(rows) > pageSize
	if hasNextPage {
		rows = rows[:pageSize]
	}
	edges, err := purchaseCorrectionEdges(ctx, db, purchaseID, rows)
	if err != nil {
		return nil, err
	}
	connection := &model.PurchaseCorrectionConnection{
		Edges: edges, PageInfo: &model.PageInfo{HasNextPage: hasNextPage, HasPreviousPage: cursor != nil},
		TotalCount: int(totalCount),
	}
	if len(edges) > 0 {
		start, end := edges[0].Cursor, edges[len(edges)-1].Cursor
		connection.PageInfo.StartCursor = &start
		connection.PageInfo.EndCursor = &end
	}
	return connection, nil
}
