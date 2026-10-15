package query

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	psqldialect "github.com/stephenafamo/bob/dialect/psql/dialect"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/scan"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type PurchaseAnalyticsEntry struct {
	Dimension string
	ID        string
	Name      string
	Period    string
	AmountYen string
	Count     int64
}

type purchaseAnalyticsEntry struct {
	Dimension string `db:"dimension"`
	ID        string `db:"id"`
	Name      string `db:"name"`
	Period    string `db:"period"`
	AmountYen string `db:"amount_yen"`
	Count     int64  `db:"count"`
}

func PurchaseAnalytics(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, start, end time.Time) ([]PurchaseAnalyticsEntry, error) {
	rows := make([]PurchaseAnalyticsEntry, 0)
	for index, query := range purchaseAnalyticsQueries(householdID, start, end) {
		part, err := bob.All(ctx, executor, query, scan.StructMapper[purchaseAnalyticsEntry]())
		if err != nil {
			querySQL, _, _ := bob.Build(ctx, query)
			return nil, fmt.Errorf("purchase analytics query %d (%s): %w", index, querySQL, err)
		}
		for _, row := range part {
			rows = append(rows, PurchaseAnalyticsEntry(row))
		}
	}
	return rows, nil
}

type PurchasePriceObservation struct {
	ItemID                              string
	OccurredAt                          time.Time
	AmountYen                           int64
	RemainingRefundableAmountYen        int64
	QuantityNumerator                   sql.Null[int64]
	QuantityDenominator                 sql.Null[int64]
	QuantityUnit                        sql.Null[string]
	PrintedUnitPriceYen                 sql.Null[int64]
	PrintedUnitPriceQuantityNumerator   sql.Null[int64]
	PrintedUnitPriceQuantityDenominator sql.Null[int64]
	PrintedUnitPriceUnit                sql.Null[string]
}

type purchasePriceObservation struct {
	ItemID                              string           `db:"item_id"`
	OccurredAt                          time.Time        `db:"occurred_at"`
	AmountYen                           int64            `db:"amount_yen"`
	RemainingRefundableAmountYen        int64            `db:"remaining_refundable_amount_yen"`
	QuantityNumerator                   sql.Null[int64]  `db:"quantity_numerator"`
	QuantityDenominator                 sql.Null[int64]  `db:"quantity_denominator"`
	QuantityUnit                        sql.Null[string] `db:"quantity_unit"`
	PrintedUnitPriceYen                 sql.Null[int64]  `db:"printed_unit_price_yen"`
	PrintedUnitPriceQuantityNumerator   sql.Null[int64]  `db:"printed_unit_price_quantity_numerator"`
	PrintedUnitPriceQuantityDenominator sql.Null[int64]  `db:"printed_unit_price_quantity_denominator"`
	PrintedUnitPriceUnit                sql.Null[string] `db:"printed_unit_price_unit"`
}

func PurchasePriceObservations(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, start, end time.Time, itemIDs []string) ([]PurchasePriceObservation, error) {
	if len(itemIDs) == 0 {
		return []PurchasePriceObservation{}, nil
	}
	itemID := psql.F("concat", psql.F("coalesce", psql.Cast(models.PurchaseItems.Columns.ProductVariantID, "text"), models.PurchaseItems.Columns.Name)(), psql.Cast(psql.Arg("\u001f"), "text"), psql.F("coalesce", models.PurchaseItems.Columns.QuantityUnit, psql.Cast(psql.Arg(""), "text"))())()
	itemIDArgs := make([]any, len(itemIDs))
	for i, id := range itemIDs {
		itemIDArgs[i] = id
	}
	itemCorrections := psql.Select(
		sm.Columns(models.Corrections.Columns.ItemID, psql.F("sum", models.Corrections.Columns.AmountDeltaYen)().As("amount_yen")),
		sm.From(models.Corrections.NameExpr(), sm.InnerJoin(models.Purchases.NameExpr()).On(models.Purchases.Columns.ID.EQ(models.Corrections.Columns.PurchaseID))),
		sm.Where(psql.And(
			models.Corrections.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Purchases.Columns.OccurredAt.GTE(psql.Arg(start)),
			models.Purchases.Columns.OccurredAt.LT(psql.Arg(end)),
			models.Purchases.Columns.Status.In(psql.Arg("POSTED"), psql.Arg("CLOSED")),
			models.Corrections.Columns.ItemID.IsNotNull(),
		)),
		sm.GroupBy(models.Corrections.Columns.ItemID),
	)
	rows, err := bob.All(ctx, executor, psql.Select(
		sm.With("item_corrections", "item_id", "amount_yen").As(itemCorrections),
		sm.Columns(itemID.As("item_id"), models.Purchases.Columns.OccurredAt, models.PurchaseItems.Columns.AmountYen, models.PurchaseItems.Columns.AmountYen.Plus(psql.F("coalesce", psql.Quote("item_corrections", "amount_yen"), psql.Arg(int64(0)))()).As("remaining_refundable_amount_yen"), models.PurchaseItems.Columns.QuantityNumerator, models.PurchaseItems.Columns.QuantityDenominator, models.PurchaseItems.Columns.QuantityUnit, models.PurchaseItems.Columns.PrintedUnitPriceYen, models.PurchaseItems.Columns.PrintedUnitPriceQuantityNumerator, models.PurchaseItems.Columns.PrintedUnitPriceQuantityDenominator, models.PurchaseItems.Columns.PrintedUnitPriceUnit),
		sm.From(models.PurchaseItems.NameExpr(), sm.InnerJoin(models.Purchases.NameExpr()).On(models.Purchases.Columns.ID.EQ(models.PurchaseItems.Columns.PurchaseID)), sm.LeftJoin("item_corrections").On(models.PurchaseItems.Columns.ID.EQ(psql.Quote("item_corrections", "item_id")))),
		sm.Where(psql.And(
			models.PurchaseItems.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Purchases.Columns.OccurredAt.GTE(psql.Arg(start)),
			models.Purchases.Columns.OccurredAt.LT(psql.Arg(end)),
			models.Purchases.Columns.Status.In(psql.Arg("POSTED"), psql.Arg("CLOSED")),
			itemID.In(psql.Arg(itemIDArgs...)),
		)),
		sm.OrderBy(models.Purchases.Columns.OccurredAt),
	), scan.StructMapper[purchasePriceObservation]())
	if err != nil {
		return nil, err
	}
	result := make([]PurchasePriceObservation, 0, len(rows))
	for _, row := range rows {
		result = append(result, PurchasePriceObservation(row))
	}
	return result, nil
}

func purchaseAnalyticsQueries(householdID gofrsuuid.UUID, start, end time.Time) []bob.Query {
	filter := psql.And(
		models.Purchases.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Purchases.Columns.OccurredAt.GTE(psql.Arg(start)),
		models.Purchases.Columns.OccurredAt.LT(psql.Arg(end)),
		psql.Or(models.Purchases.Columns.Status.EQ(psql.Arg("POSTED")), models.Purchases.Columns.Status.EQ(psql.Arg("CLOSED"))),
	)
	period := psql.F("to_char", psql.F("date_trunc", psql.Arg("month"), psql.F("timezone", psql.Arg("Asia/Tokyo"), models.Purchases.Columns.OccurredAt)())(), psql.Arg("YYYY-MM"))()
	return []bob.Query{
		purchaseMerchantAnalyticsQuery(filter, period),
		purchaseItemAnalyticsQuery(filter, period),
		purchasePoolAnalyticsQuery(filter, period),
		purchaseAccountAnalyticsQuery(filter, period),
		purchaseCorrectionAnalyticsQuery(filter, period, "item", "購入全体の訂正", true),
		purchaseCorrectionAnalyticsQuery(filter, period, "pool", "プール未配賦の訂正", false),
		purchaseCorrectionAnalyticsQuery(filter, period, "account", "決済元未配賦の訂正", false),
		purchaseLegacyAccountAnalyticsQuery(filter, period),
	}
}

func purchaseCorrectionAnalyticsQuery(filter bob.Expression, period *psqldialect.Function, dimension, name string, wideOnly bool) bob.Query {
	correctionFilter := filter
	if wideOnly {
		correctionFilter = psql.And(filter, models.Corrections.Columns.ItemID.IsNull())
	}
	correctionTotals := psql.Select(
		sm.Columns(models.Corrections.Columns.PurchaseID, psql.F("sum", models.Corrections.Columns.AmountDeltaYen)().As("amount_yen")),
		sm.From(models.Corrections.NameExpr(), sm.InnerJoin(models.Purchases.NameExpr()).On(models.Purchases.Columns.ID.EQ(models.Corrections.Columns.PurchaseID))),
		sm.Where(correctionFilter), sm.GroupBy(models.Corrections.Columns.PurchaseID),
	)
	return psql.Select(
		sm.With("purchase_corrections", "purchase_id", "amount_yen").As(correctionTotals),
		sm.Columns(psql.Arg(dimension).As("dimension"), psql.Arg("__correction__").As("id"), psql.Arg(name).As("name"), period.As("period"), psql.F("sum", psql.Quote("purchase_corrections", "amount_yen"))().As("amount_yen"), psql.F("count", psql.Cast(psql.Arg(int64(1)), "bigint"))().As("count")),
		sm.From("purchase_corrections", sm.InnerJoin(models.Purchases.NameExpr()).On(models.Purchases.Columns.ID.EQ(psql.Quote("purchase_corrections", "purchase_id")))),
		sm.Where(psql.And(filter, psql.Quote("purchase_corrections", "amount_yen").NE(psql.Arg(int64(0))))),
		sm.GroupBy(psql.Quote("period")),
	)
}

func purchaseLegacyAccountAnalyticsQuery(filter bob.Expression, period *psqldialect.Function) bob.Query {
	itemTotals := psql.Select(
		sm.Columns(models.PurchaseItems.Columns.PurchaseID, psql.F("sum", models.PurchaseItems.Columns.AmountYen)().As("amount_yen")),
		sm.From(models.PurchaseItems.NameExpr(), sm.InnerJoin(models.Purchases.NameExpr()).On(models.Purchases.Columns.ID.EQ(models.PurchaseItems.Columns.PurchaseID))),
		sm.Where(filter), sm.GroupBy(models.PurchaseItems.Columns.PurchaseID),
	)
	payments := psql.Select(
		sm.Columns(models.PurchasePayments.Columns.PurchaseID),
		sm.From(models.PurchasePayments.NameExpr()),
		sm.GroupBy(models.PurchasePayments.Columns.PurchaseID),
	)
	return psql.Select(
		sm.With("item_totals", "purchase_id", "amount_yen").As(itemTotals),
		sm.With("purchase_payments", "purchase_id").As(payments),
		sm.Columns(psql.Arg("account").As("dimension"), psql.Cast(models.Purchases.Columns.PaymentAccountID, "text").As("id"), models.Accounts.Columns.Name.As("name"), period.As("period"), psql.F("sum", psql.Quote("item_totals", "amount_yen"))().As("amount_yen"), psql.F("count", psql.Cast(psql.Arg(int64(1)), "bigint"))().As("count")),
		sm.From(models.Purchases.NameExpr(), sm.InnerJoin("item_totals").On(models.Purchases.Columns.ID.EQ(psql.Quote("item_totals", "purchase_id"))), sm.LeftJoin("purchase_payments").On(models.Purchases.Columns.ID.EQ(psql.Quote("purchase_payments", "purchase_id"))), sm.InnerJoin(models.Accounts.NameExpr()).On(models.Accounts.Columns.ID.EQ(models.Purchases.Columns.PaymentAccountID))),
		sm.Where(psql.And(filter, models.Purchases.Columns.PaymentAccountID.IsNotNull(), psql.Quote("purchase_payments", "purchase_id").IsNull())),
		sm.GroupBy(models.Purchases.Columns.PaymentAccountID), sm.GroupBy(models.Accounts.Columns.Name), sm.GroupBy(psql.Quote("period")),
	)
}

func purchaseMerchantAnalyticsQuery(filter bob.Expression, period *psqldialect.Function) bob.Query {
	itemTotals := psql.Select(
		sm.Columns(models.PurchaseItems.Columns.PurchaseID, psql.F("sum", models.PurchaseItems.Columns.AmountYen)().As("amount_yen")),
		sm.From(models.PurchaseItems.NameExpr(), sm.InnerJoin(models.Purchases.NameExpr()).On(models.Purchases.Columns.ID.EQ(models.PurchaseItems.Columns.PurchaseID))),
		sm.Where(filter), sm.GroupBy(models.PurchaseItems.Columns.PurchaseID),
	)
	correctionTotals := psql.Select(
		sm.Columns(models.Corrections.Columns.PurchaseID, psql.F("sum", models.Corrections.Columns.AmountDeltaYen)().As("amount_yen")),
		sm.From(models.Corrections.NameExpr(), sm.InnerJoin(models.Purchases.NameExpr()).On(models.Purchases.Columns.ID.EQ(models.Corrections.Columns.PurchaseID))),
		sm.Where(filter), sm.GroupBy(models.Corrections.Columns.PurchaseID),
	)
	return psql.Select(
		sm.With("item_totals", "purchase_id", "amount_yen").As(itemTotals),
		sm.With("correction_totals", "purchase_id", "amount_yen").As(correctionTotals),
		sm.Columns(
			psql.Arg("merchant").As("dimension"),
			psql.F("coalesce", models.Purchases.Columns.Merchant, psql.Arg("__unknown"))().As("id"),
			psql.F("coalesce", psql.F("nullif", models.Purchases.Columns.Merchant, psql.Arg(""))(), psql.Arg("店舗未入力"))().As("name"),
			period.As("period"),
			psql.F("sum", psql.F("coalesce", psql.Quote("item_totals", "amount_yen"), psql.Arg(int64(0)))().Plus(psql.F("coalesce", psql.Quote("correction_totals", "amount_yen"), psql.Arg(int64(0)))()))().As("amount_yen"),
			psql.F("count", psql.Cast(psql.Arg(int64(1)), "bigint"))().As("count"),
		),
		sm.From(models.Purchases.NameExpr(),
			sm.LeftJoin("item_totals").On(models.Purchases.Columns.ID.EQ(psql.Quote("item_totals", "purchase_id"))),
			sm.LeftJoin("correction_totals").On(models.Purchases.Columns.ID.EQ(psql.Quote("correction_totals", "purchase_id"))),
		),
		sm.Where(filter), sm.GroupBy(models.Purchases.Columns.Merchant), sm.GroupBy(psql.Quote("period")),
	)
}

func purchaseItemAnalyticsQuery(filter bob.Expression, period *psqldialect.Function) bob.Query {
	correctionTotals := psql.Select(
		sm.Columns(models.Corrections.Columns.ItemID, psql.F("sum", models.Corrections.Columns.AmountDeltaYen)().As("amount_yen")),
		sm.From(models.Corrections.NameExpr(), sm.InnerJoin(models.Purchases.NameExpr()).On(models.Purchases.Columns.ID.EQ(models.Corrections.Columns.PurchaseID))),
		sm.Where(psql.And(filter, models.Corrections.Columns.ItemID.IsNotNull())), sm.GroupBy(models.Corrections.Columns.ItemID),
	)
	itemID := psql.F("concat", psql.F("coalesce", psql.Cast(models.PurchaseItems.Columns.ProductVariantID, "text"), models.PurchaseItems.Columns.Name)(), psql.Cast(psql.Arg("\u001f"), "text"), psql.F("coalesce", models.PurchaseItems.Columns.QuantityUnit, psql.Cast(psql.Arg(""), "text"))())()
	itemName := psql.F("concat", models.PurchaseItems.Columns.Name, psql.Cast(psql.Arg(" · "), "text"), psql.F("coalesce", models.PurchaseItems.Columns.QuantityUnit, psql.Cast(psql.Arg("単位不明"), "text"))())()
	return psql.Select(
		sm.With("item_corrections", "item_id", "amount_yen").As(correctionTotals),
		sm.With("item_parts", "id", "name", "period", "amount_yen", "count").As(psql.Select(
			sm.Columns(itemID.As("id"), itemName.As("name"), period.As("period"), psql.F("sum", models.PurchaseItems.Columns.AmountYen.Plus(psql.F("coalesce", psql.Quote("item_corrections", "amount_yen"), psql.Arg(int64(0)))()))().As("amount_yen"), psql.F("count", models.PurchaseItems.Columns.ID)().As("count")),
			sm.From(models.PurchaseItems.NameExpr(), sm.InnerJoin(models.Purchases.NameExpr()).On(models.Purchases.Columns.ID.EQ(models.PurchaseItems.Columns.PurchaseID)), sm.LeftJoin("item_corrections").On(models.PurchaseItems.Columns.ID.EQ(psql.Quote("item_corrections", "item_id")))),
			sm.Where(filter), sm.GroupBy(models.PurchaseItems.Columns.ProductVariantID), sm.GroupBy(models.PurchaseItems.Columns.Name), sm.GroupBy(models.PurchaseItems.Columns.QuantityUnit), sm.GroupBy(psql.Quote("period")),
		)),
		sm.Columns(psql.Arg("item").As("dimension"), psql.Quote("item_parts", "id").As("id"), psql.Quote("item_parts", "name").As("name"), psql.Quote("item_parts", "period").As("period"), psql.F("sum", psql.Quote("item_parts", "amount_yen"))().As("amount_yen"), psql.F("sum", psql.Quote("item_parts", "count"))().As("count")),
		sm.From("item_parts"), sm.GroupBy(psql.Quote("item_parts", "id")), sm.GroupBy(psql.Quote("item_parts", "name")), sm.GroupBy(psql.Quote("item_parts", "period")),
	)
}

func purchasePoolAnalyticsQuery(filter bob.Expression, period *psqldialect.Function) bob.Query {
	return psql.Select(
		sm.Columns(psql.Arg("pool").As("dimension"), psql.Cast(models.ItemAllocations.Columns.PoolID, "text").As("id"), models.Pools.Columns.Name.As("name"), period.As("period"), psql.F("sum", models.ItemAllocations.Columns.AmountYen)().As("amount_yen"), psql.F("count", psql.Cast(psql.Arg(int64(1)), "bigint"))().As("count")),
		sm.From(models.ItemAllocations.NameExpr(), sm.InnerJoin(models.Purchases.NameExpr()).On(models.Purchases.Columns.ID.EQ(models.ItemAllocations.Columns.PurchaseID)), sm.InnerJoin(models.Pools.NameExpr()).On(models.Pools.Columns.ID.EQ(models.ItemAllocations.Columns.PoolID))),
		sm.Where(filter), sm.GroupBy(models.ItemAllocations.Columns.PoolID), sm.GroupBy(models.Pools.Columns.Name), sm.GroupBy(psql.Quote("period")),
	)
}

func purchaseAccountAnalyticsQuery(filter bob.Expression, period *psqldialect.Function) bob.Query {
	return psql.Select(
		sm.Columns(psql.Arg("account").As("dimension"), psql.Cast(models.PurchasePayments.Columns.AccountID, "text").As("id"), models.Accounts.Columns.Name.As("name"), period.As("period"), psql.F("sum", models.PurchasePayments.Columns.AmountYen)().As("amount_yen"), psql.F("count", psql.Cast(psql.Arg(int64(1)), "bigint"))().As("count")),
		sm.From(models.PurchasePayments.NameExpr(), sm.InnerJoin(models.Purchases.NameExpr()).On(models.Purchases.Columns.ID.EQ(models.PurchasePayments.Columns.PurchaseID)), sm.InnerJoin(models.Accounts.NameExpr()).On(models.Accounts.Columns.ID.EQ(models.PurchasePayments.Columns.AccountID))),
		sm.Where(filter), sm.GroupBy(models.PurchasePayments.Columns.AccountID), sm.GroupBy(models.Accounts.Columns.Name), sm.GroupBy(psql.Quote("period")),
	)
}
