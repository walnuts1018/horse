package graph

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strconv"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	psqldialect "github.com/stephenafamo/bob/dialect/psql/dialect"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/scan"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (r *queryResolver) monthlyCategorySpending(ctx context.Context, householdID, from, to string) ([]*model.MonthlyCategorySpending, error) {
	hhID, start, end, err := monthlySpendingRange(ctx, r.DB, householdID, from, to)
	if err != nil {
		return nil, err
	}
	itemParts := psql.Select(
		sm.Columns(spendingPeriod(models.Purchases.Columns.OccurredAt).As("period"), models.PurchaseItems.Columns.CategoryID.As("category_id"), models.PurchaseItems.Columns.AmountYen.As("amount_yen")),
		sm.From(models.PurchaseItems.NameExpr(), sm.InnerJoin(models.Purchases.NameExpr()).On(models.Purchases.Columns.ID.EQ(models.PurchaseItems.Columns.PurchaseID))),
		sm.Where(spendingPurchaseFilter(hhID, start, end)),
	)
	correctionParts := psql.Select(
		sm.Columns(spendingPeriod(models.Purchases.Columns.OccurredAt).As("period"), models.PurchaseItems.Columns.CategoryID.As("category_id"), models.Corrections.Columns.AmountDeltaYen.As("amount_yen")),
		sm.From(models.Corrections.NameExpr(),
			sm.InnerJoin(models.Purchases.NameExpr()).On(models.Purchases.Columns.ID.EQ(models.Corrections.Columns.PurchaseID)),
			sm.LeftJoin(models.PurchaseItems.NameExpr()).On(models.PurchaseItems.Columns.ID.EQ(models.Corrections.Columns.ItemID)),
		),
		sm.Where(spendingPurchaseFilter(hhID, start, end)),
	)
	parts := psql.Select(
		sm.With("item_parts", "period", "category_id", "amount_yen").As(itemParts),
		sm.Columns(psql.Quote("item_parts", "period"), psql.Quote("item_parts", "category_id"), psql.Quote("item_parts", "amount_yen")),
		sm.From("item_parts"),
		sm.UnionAll(correctionParts),
	)
	rows, err := bob.All(ctx, queryExecutor(r.DB), psql.Select(
		sm.With("spending_parts", "period", "category_id", "amount_yen").As(parts),
		sm.Columns(
			psql.Quote("spending_parts", "period"),
			psql.Quote("spending_parts", "category_id"),
			models.Categories.Columns.Name.As("category_name"),
			psql.F("sum", psql.Quote("spending_parts", "amount_yen"))().As("amount_yen"),
		),
		sm.From("spending_parts", sm.LeftJoin(models.Categories.NameExpr()).On(models.Categories.Columns.ID.EQ(psql.Quote("spending_parts", "category_id")))),
		sm.GroupBy(psql.Quote("spending_parts", "period")),
		sm.GroupBy(psql.Quote("spending_parts", "category_id")),
		sm.GroupBy(models.Categories.Columns.Name),
	), scan.StructMapper[monthlyCategorySpendingRow]())
	if err != nil {
		return nil, err
	}
	slices.SortFunc(rows, func(a, b monthlyCategorySpendingRow) int {
		if order := cmp.Compare(b.Period, a.Period); order != 0 {
			return order
		}
		if !a.CategoryID.Valid && b.CategoryID.Valid {
			return 1
		}
		if a.CategoryID.Valid && !b.CategoryID.Valid {
			return -1
		}
		if !a.CategoryID.Valid {
			return 0
		}
		if order := cmp.Compare(a.CategoryName.V, b.CategoryName.V); order != 0 {
			return order
		}
		return cmp.Compare(a.CategoryID.V.String(), b.CategoryID.V.String())
	})
	result := make([]*model.MonthlyCategorySpending, 0, len(rows))
	for _, row := range rows {
		amount, err := monthlySpendingAmount(row.AmountYen)
		if err != nil {
			return nil, err
		}
		entry := &model.MonthlyCategorySpending{Period: model.YearMonth(row.Period), AmountYen: graphLong(amount)}
		if row.CategoryID.Valid {
			id, name := row.CategoryID.V.String(), row.CategoryName.V
			entry.CategoryID, entry.CategoryName = &id, &name
		}
		result = append(result, entry)
	}
	return result, nil
}

func (r *queryResolver) monthlyMemberSpending(ctx context.Context, householdID, from, to string) (*model.MonthlyMemberSpendingReport, error) {
	hhID, start, end, err := monthlySpendingRange(ctx, r.DB, householdID, from, to)
	if err != nil {
		return nil, err
	}
	purchaseParts := psql.Select(
		sm.Columns(spendingPeriod(models.Purchases.Columns.OccurredAt).As("period"), models.PurchaseMemberBurdens.Columns.MemberID.As("member_id"), models.PurchaseMemberBurdens.Columns.AmountYen.As("amount_yen")),
		sm.From(models.PurchaseMemberBurdens.NameExpr(), sm.InnerJoin(models.Purchases.NameExpr()).On(models.Purchases.Columns.ID.EQ(models.PurchaseMemberBurdens.Columns.PurchaseID))),
		sm.Where(psql.And(spendingPurchaseFilter(hhID, start, end), models.PurchaseMemberBurdens.Columns.HouseholdID.EQ(psql.Arg(hhID)))),
	)
	correctionParts := psql.Select(
		sm.Columns(spendingPeriod(models.Purchases.Columns.OccurredAt).As("period"), models.CorrectionMemberBurdens.Columns.MemberID.As("member_id"), models.CorrectionMemberBurdens.Columns.AmountDeltaYen.As("amount_yen")),
		sm.From(models.CorrectionMemberBurdens.NameExpr(),
			sm.InnerJoin(models.Corrections.NameExpr()).On(models.Corrections.Columns.ID.EQ(models.CorrectionMemberBurdens.Columns.CorrectionID)),
			sm.InnerJoin(models.Purchases.NameExpr()).On(models.Purchases.Columns.ID.EQ(models.Corrections.Columns.PurchaseID)),
		),
		sm.Where(psql.And(spendingPurchaseFilter(hhID, start, end), models.CorrectionMemberBurdens.Columns.HouseholdID.EQ(psql.Arg(hhID)))),
	)
	parts := psql.Select(
		sm.With("purchase_parts", "period", "member_id", "amount_yen").As(purchaseParts),
		sm.Columns(psql.Quote("purchase_parts", "period"), psql.Quote("purchase_parts", "member_id"), psql.Quote("purchase_parts", "amount_yen")),
		sm.From("purchase_parts"),
		sm.UnionAll(correctionParts),
	)
	rows, err := bob.All(ctx, queryExecutor(r.DB), psql.Select(
		sm.With("spending_parts", "period", "member_id", "amount_yen").As(parts),
		sm.Columns(psql.Quote("spending_parts", "period"), models.HouseholdMembers.Columns.MemberID.As("member_id"), models.Members.Columns.DisplayName.As("member_name"), psql.F("sum", psql.Quote("spending_parts", "amount_yen"))().As("amount_yen")),
		sm.From("spending_parts",
			sm.InnerJoin(models.HouseholdMembers.NameExpr()).On(models.HouseholdMembers.Columns.MemberID.EQ(psql.Quote("spending_parts", "member_id"))),
			sm.InnerJoin(models.Members.NameExpr()).On(models.Members.Columns.ID.EQ(models.HouseholdMembers.Columns.MemberID)),
		),
		sm.Where(models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(hhID))),
		sm.GroupBy(psql.Quote("spending_parts", "period")),
		sm.GroupBy(models.HouseholdMembers.Columns.MemberID),
		sm.GroupBy(models.Members.Columns.DisplayName),
		sm.OrderBy(psql.Quote("spending_parts", "period")),
		sm.OrderBy(models.HouseholdMembers.Columns.MemberID),
	), scan.StructMapper[monthlyMemberSpendingRow]())
	if err != nil {
		return nil, err
	}
	result := &model.MonthlyMemberSpendingReport{Items: make([]*model.MonthlyMemberSpending, 0, len(rows))}
	for _, row := range rows {
		amount, err := monthlySpendingAmount(row.AmountYen)
		if err != nil {
			return nil, err
		}
		result.Items = append(result.Items, &model.MonthlyMemberSpending{Period: model.YearMonth(row.Period), MemberID: row.MemberID.String(), MemberName: row.MemberName, AmountYen: graphLong(amount)})
	}
	result.HasUnattributedPurchases, err = hasUnattributedMonthlySpending(ctx, queryExecutor(r.DB), hhID, start, end)
	if err != nil {
		return nil, err
	}
	return result, nil
}

type monthlyCategorySpendingRow struct {
	Period       string                   `db:"period"`
	CategoryID   sql.Null[gofrsuuid.UUID] `db:"category_id"`
	CategoryName sql.Null[string]         `db:"category_name"`
	AmountYen    string                   `db:"amount_yen"`
}

type monthlyMemberSpendingRow struct {
	Period     string         `db:"period"`
	MemberID   gofrsuuid.UUID `db:"member_id"`
	MemberName string         `db:"member_name"`
	AmountYen  string         `db:"amount_yen"`
}

type monthlySpendingMismatch struct {
	Mismatch bool `db:"mismatch"`
}

func monthlySpendingRange(ctx context.Context, db *pgxpool.Pool, householdID, from, to string) (gofrsuuid.UUID, time.Time, time.Time, error) {
	if _, err := requireMember(ctx, db, householdID); err != nil {
		return gofrsuuid.Nil, time.Time{}, time.Time{}, err
	}
	fromMonth, err := parseAccountingPeriod(from)
	if err != nil {
		return gofrsuuid.Nil, time.Time{}, time.Time{}, fmt.Errorf("%w: from must use YYYY-MM", ErrBadInput)
	}
	toMonth, err := parseAccountingPeriod(to)
	if err != nil || toMonth.Before(fromMonth) || toMonth.After(fromMonth.AddDate(10, 0, 0)) {
		return gofrsuuid.Nil, time.Time{}, time.Time{}, fmt.Errorf("%w: to must be within ten years after from", ErrBadInput)
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return gofrsuuid.Nil, time.Time{}, time.Time{}, fmt.Errorf("parse household id: %w", err)
	}
	return hhID, fromMonth.UTC(), toMonth.AddDate(0, 1, 0).UTC(), nil
}

func spendingPeriod(occurredAt bob.Expression) *psqldialect.Function {
	localTime := psql.F("timezone", psql.Arg("Asia/Tokyo"), occurredAt)()
	month := psql.F("date_trunc", psql.Arg("month"), localTime)()
	return psql.F("to_char", month, psql.Arg("YYYY-MM"))()
}

func spendingPurchaseFilter(householdID gofrsuuid.UUID, start, end time.Time) bob.Expression {
	return psql.And(
		models.Purchases.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Purchases.Columns.OccurredAt.GTE(psql.Arg(start)),
		models.Purchases.Columns.OccurredAt.LT(psql.Arg(end)),
		psql.Or(
			models.Purchases.Columns.Status.EQ(psql.Arg(string(model.PurchaseStatusPosted))),
			models.Purchases.Columns.Status.EQ(psql.Arg(string(model.PurchaseStatusClosed))),
		),
	)
}

func monthlySpendingAmount(value string) (int64, error) {
	amount, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: monthly spending total exceeds int64", ErrBadInput)
	}
	return amount, nil
}

func hasUnattributedMonthlySpending(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, start, end time.Time) (bool, error) {
	allocations := psql.Select(
		sm.Columns(
			models.ItemAllocations.Columns.PurchaseID,
			models.ItemAllocations.Columns.ItemID,
			models.ItemAllocations.Columns.PoolID,
			psql.F("sum", models.ItemAllocations.Columns.AmountYen)().As("amount_yen"),
		),
		sm.From(models.ItemAllocations.NameExpr(),
			sm.InnerJoin(models.Purchases.NameExpr()).On(models.Purchases.Columns.ID.EQ(models.ItemAllocations.Columns.PurchaseID)),
		),
		sm.Where(spendingPurchaseFilter(householdID, start, end)),
		sm.GroupBy(models.ItemAllocations.Columns.PurchaseID),
		sm.GroupBy(models.ItemAllocations.Columns.ItemID),
		sm.GroupBy(models.ItemAllocations.Columns.PoolID),
	)
	burdens := psql.Select(
		sm.Columns(
			models.PurchaseMemberBurdens.Columns.PurchaseID,
			models.PurchaseMemberBurdens.Columns.ItemID,
			models.PurchaseMemberBurdens.Columns.PoolID,
			psql.F("sum", models.PurchaseMemberBurdens.Columns.AmountYen)().As("amount_yen"),
		),
		sm.From(models.PurchaseMemberBurdens.NameExpr(),
			sm.InnerJoin(models.Purchases.NameExpr()).On(models.Purchases.Columns.ID.EQ(models.PurchaseMemberBurdens.Columns.PurchaseID)),
		),
		sm.Where(psql.And(spendingPurchaseFilter(householdID, start, end), models.PurchaseMemberBurdens.Columns.HouseholdID.EQ(psql.Arg(householdID)))),
		sm.GroupBy(models.PurchaseMemberBurdens.Columns.PurchaseID),
		sm.GroupBy(models.PurchaseMemberBurdens.Columns.ItemID),
		sm.GroupBy(models.PurchaseMemberBurdens.Columns.PoolID),
	)
	allocatedAmount := psql.Quote(fieldAllocations, "amount_yen")
	burdenAmount := psql.F("coalesce", psql.Quote("burdens", "amount_yen"), psql.Arg(int64(0)))()
	allocationMismatch, err := bob.All(ctx, executor, psql.Select(
		sm.With(fieldAllocations, "purchase_id", "item_id", "pool_id", "amount_yen").As(allocations),
		sm.With("burdens", "purchase_id", "item_id", "pool_id", "amount_yen").As(burdens),
		sm.Columns(monthlySpendingMismatchExpression()),
		sm.From(fieldAllocations, sm.LeftJoin("burdens").On(psql.And(
			psql.Quote(fieldAllocations, "purchase_id").EQ(psql.Quote("burdens", "purchase_id")),
			psql.Quote(fieldAllocations, "item_id").EQ(psql.Quote("burdens", "item_id")),
			psql.Quote(fieldAllocations, "pool_id").EQ(psql.Quote("burdens", "pool_id")),
		))),
		sm.Where(psql.And(allocatedAmount.NE(psql.Arg(int64(0))), allocatedAmount.NE(burdenAmount))),
		sm.Limit(1),
	), scan.StructMapper[monthlySpendingMismatch]())
	if err != nil {
		return false, err
	}
	if len(allocationMismatch) > 0 {
		return true, nil
	}

	correctionBurdens := psql.Select(
		sm.Columns(
			models.CorrectionMemberBurdens.Columns.CorrectionID,
			psql.F("sum", models.CorrectionMemberBurdens.Columns.AmountDeltaYen)().As("amount_delta_yen"),
		),
		sm.From(models.CorrectionMemberBurdens.NameExpr(),
			sm.InnerJoin(models.Corrections.NameExpr()).On(models.Corrections.Columns.ID.EQ(models.CorrectionMemberBurdens.Columns.CorrectionID)),
			sm.InnerJoin(models.Purchases.NameExpr()).On(models.Purchases.Columns.ID.EQ(models.Corrections.Columns.PurchaseID)),
		),
		sm.Where(psql.And(spendingPurchaseFilter(householdID, start, end), models.CorrectionMemberBurdens.Columns.HouseholdID.EQ(psql.Arg(householdID)))),
		sm.GroupBy(models.CorrectionMemberBurdens.Columns.CorrectionID),
	)
	correctionMismatch, err := bob.All(ctx, executor, psql.Select(
		sm.With("correction_burdens", "correction_id", "amount_delta_yen").As(correctionBurdens),
		sm.Columns(monthlySpendingMismatchExpression()),
		sm.From(models.Corrections.NameExpr(),
			sm.InnerJoin(models.Purchases.NameExpr()).On(models.Purchases.Columns.ID.EQ(models.Corrections.Columns.PurchaseID)),
			sm.LeftJoin("correction_burdens").On(psql.Quote("correction_burdens", "correction_id").EQ(models.Corrections.Columns.ID)),
		),
		sm.Where(psql.And(
			spendingPurchaseFilter(householdID, start, end),
			psql.Not(models.Corrections.Columns.AmountDeltaYen.EQ(psql.F("coalesce", psql.Quote("correction_burdens", "amount_delta_yen"), psql.Arg(int64(0)))())),
		)),
		sm.Limit(1),
	), scan.StructMapper[monthlySpendingMismatch]())
	if err != nil {
		return false, err
	}
	return len(correctionMismatch) > 0, nil
}

func monthlySpendingMismatchExpression() bob.Expression {
	return psql.F("coalesce", psql.Cast(psql.Arg(true), "boolean"), psql.Cast(psql.Arg(false), "boolean"))().As("mismatch")
}
