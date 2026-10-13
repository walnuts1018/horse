package graph

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
	postgresquery "github.com/walnuts1018/horse/internal/postgres/query"
)

func (r *queryResolver) monthlyCategorySpending(ctx context.Context, householdID, from, to string) ([]*model.MonthlyCategorySpending, error) {
	hhID, start, end, err := monthlySpendingRange(ctx, r.DB, householdID, from, to)
	if err != nil {
		return nil, err
	}
	rows, err := postgresquery.MonthlyCategorySpendings(ctx, queryExecutor(r.DB), hhID, start, end)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(rows, func(a, b postgresquery.MonthlyCategorySpending) int {
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
	rows, hasUnattributed, err := postgresquery.MonthlyMemberSpendings(ctx, queryExecutor(r.DB), hhID, start, end)
	if err != nil {
		return nil, err
	}
	result := &model.MonthlyMemberSpendingReport{Items: make([]*model.MonthlyMemberSpending, 0, len(rows)), HasUnattributedPurchases: hasUnattributed}
	for _, row := range rows {
		amount, err := monthlySpendingAmount(row.AmountYen)
		if err != nil {
			return nil, err
		}
		result.Items = append(result.Items, &model.MonthlyMemberSpending{Period: model.YearMonth(row.Period), MemberID: row.MemberID.String(), MemberName: row.MemberName, AmountYen: graphLong(amount)})
	}
	return result, nil
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

func monthlySpendingAmount(value string) (int64, error) {
	amount, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: monthly spending total exceeds int64", ErrBadInput)
	}
	return amount, nil
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
