package graph

import (
	"context"
	"fmt"
	"strconv"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/scan"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (r *queryResolver) homeSummary(ctx context.Context, householdID string) (*model.HomeSummary, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	location, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		return nil, fmt.Errorf("load Japan time zone: %w", err)
	}
	now := time.Now().In(location)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	startOfWeek := today.AddDate(0, 0, -6)
	startOfMonth := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, location)
	startOfNextMonth := startOfMonth.AddDate(0, 1, 0)
	executor := queryExecutor(r.DB)

	dateExpression := psql.F("to_char",
		psql.F("date_trunc", psql.Arg("day"), psql.F("timezone", psql.Arg("Asia/Tokyo"), models.Purchases.Columns.OccurredAt)())(),
		psql.Arg("YYYY-MM-DD"),
	)()
	itemParts := psql.Select(
		sm.Columns(dateExpression.As("date"), models.PurchaseItems.Columns.AmountYen.As("amount_yen")),
		sm.From(models.PurchaseItems.NameExpr(),
			sm.InnerJoin(models.Purchases.NameExpr()).On(models.Purchases.Columns.ID.EQ(models.PurchaseItems.Columns.PurchaseID)),
		),
		sm.Where(spendingPurchaseFilter(hhID, startOfWeek.UTC(), today.AddDate(0, 0, 1).UTC())),
	)
	correctionParts := psql.Select(
		sm.Columns(dateExpression.As("date"), models.Corrections.Columns.AmountDeltaYen.As("amount_yen")),
		sm.From(models.Corrections.NameExpr(), sm.InnerJoin(models.Purchases.NameExpr()).On(models.Purchases.Columns.ID.EQ(models.Corrections.Columns.PurchaseID))),
		sm.Where(spendingPurchaseFilter(hhID, startOfWeek.UTC(), today.AddDate(0, 0, 1).UTC())),
	)
	parts := psql.Select(
		sm.With("spending_parts", "date", "amount_yen").As(itemParts),
		sm.Columns(psql.Quote("spending_parts", "date"), psql.Quote("spending_parts", "amount_yen")),
		sm.From("spending_parts"),
		sm.UnionAll(correctionParts),
	)
	dailyRows, err := bob.All(ctx, executor, psql.Select(
		sm.With("all_spending_parts", "date", "amount_yen").As(parts),
		sm.Columns(psql.Quote("all_spending_parts", "date"), psql.F("sum", psql.Quote("all_spending_parts", "amount_yen"))().As("amount_yen")),
		sm.From("all_spending_parts"),
		sm.GroupBy(psql.Quote("all_spending_parts", "date")),
		sm.OrderBy(psql.Quote("all_spending_parts", "date")),
	), scan.StructMapper[homeDailySpendingRow]())
	if err != nil {
		return nil, err
	}
	dailyAmounts := make(map[string]int64, len(dailyRows))
	for _, row := range dailyRows {
		amount, err := strconv.ParseInt(row.AmountYen, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse daily spending amount: %w", err)
		}
		dailyAmounts[row.Date] = amount
	}
	dailySpending := make([]*model.HomeDailySpending, 0, 7)
	for day := startOfWeek; !day.After(today); day = day.AddDate(0, 0, 1) {
		date := day.Format(time.DateOnly)
		dailySpending = append(dailySpending, &model.HomeDailySpending{
			Date:      model.Date(date),
			AmountYen: graphLong(dailyAmounts[date]),
		})
	}

	var itemCountRow homeMonthlyItemCountRow
	itemCountRows, err := bob.All(ctx, executor, psql.Select(
		sm.Columns(psql.F("count", models.PurchaseItems.Columns.ID)().As("item_count")),
		sm.From(models.PurchaseItems.NameExpr(),
			sm.InnerJoin(models.Purchases.NameExpr()).On(models.Purchases.Columns.ID.EQ(models.PurchaseItems.Columns.PurchaseID)),
		),
		sm.Where(spendingPurchaseFilter(hhID, startOfMonth.UTC(), startOfNextMonth.UTC())),
	), scan.StructMapper[homeMonthlyItemCountRow]())
	if err != nil {
		return nil, err
	}
	if len(itemCountRows) > 0 {
		itemCountRow = itemCountRows[0]
	}
	return &model.HomeSummary{
		DailySpending:    dailySpending,
		MonthlyItemCount: graphLong(itemCountRow.ItemCount),
	}, nil
}

type homeDailySpendingRow struct {
	Date      string `db:"date"`
	AmountYen string `db:"amount_yen"`
}

type homeMonthlyItemCountRow struct {
	ItemCount int64 `db:"item_count"`
}
