package settlement

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func calculateSettlementPreview(ctx context.Context, db bob.Executor, householdID, period string) ([]domain.SettlementAmount, error) {
	return calculateSettlementPreviewWithRules(ctx, db, householdID, period)
}

func CalculatePreview(ctx context.Context, db bob.Executor, householdID, period string) ([]domain.SettlementAmount, error) {
	return calculateSettlementPreview(ctx, db, householdID, period)
}

func calculateSettlementPreviewWithRules(ctx context.Context, db bob.Executor, householdID, period string) ([]domain.SettlementAmount, error) {
	target, err := parseAccountingPeriod(period)
	if err != nil {
		return nil, fmt.Errorf("%w: period must use YYYY-MM", ErrBadInput)
	}
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	runs, err := models.SettlementRuns.Query(sm.Where(psql.And(
		models.SettlementRuns.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.SettlementRuns.Columns.Status.EQ(psql.Arg("CLOSED")),
		models.SettlementRuns.Columns.Period.LT(psql.Arg(period)),
	))).All(ctx, db)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(runs, func(left, right *models.SettlementRun) int {
		if comparison := cmp.Compare(right.Period, left.Period); comparison != 0 {
			return comparison
		}
		return cmp.Compare(right.Revision, left.Revision)
	})
	var closedPeriod *string
	carry := make(map[string]int64)
	if len(runs) > 0 {
		latestRun := runs[0]
		periodCopy := latestRun.Period
		closedPeriod = &periodCopy
		lines, err := models.SettlementLines.Query(sm.Where(models.SettlementLines.Columns.SettlementRunID.EQ(psql.Arg(latestRun.ID)))).All(ctx, db)
		if err != nil {
			return nil, err
		}
		for _, line := range lines {
			carry[line.MemberID.String()] = line.AmountDueYen
		}
	}
	firstOpen := target
	if closedPeriod != nil {
		closedMonth, err := parseAccountingPeriod(*closedPeriod)
		if err != nil {
			return nil, err
		}
		firstOpen = closedMonth.AddDate(0, 1, 0)
	} else {
		earliest := ""
		obligations, err := models.FundingObligations.Query(sm.Where(models.FundingObligations.Columns.HouseholdID.EQ(psql.Arg(householdUUID)))).All(ctx, db)
		if err != nil {
			return nil, err
		}
		for _, obligation := range obligations {
			earliest = earlierPeriod(earliest, obligation.Period)
		}
		schedules, err := models.FundingSchedules.Query(sm.Where(models.FundingSchedules.Columns.HouseholdID.EQ(psql.Arg(householdUUID)))).All(ctx, db)
		if err != nil {
			return nil, err
		}
		for _, schedule := range schedules {
			earliest = earlierPeriod(earliest, accounting.PeriodFor(schedule.StartsOn))
		}
		purchases, err := models.Purchases.Query(sm.Where(psql.And(
			models.Purchases.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			models.Purchases.Columns.Status.In(psql.Arg(string("POSTED")), psql.Arg(string("CLOSED"))),
		))).All(ctx, db)
		if err != nil {
			return nil, err
		}
		for _, purchase := range purchases {
			earliest = earlierPeriod(earliest, accounting.PeriodFor(purchase.OccurredAt))
		}
		if earliest != "" {
			firstOpen, err = parseAccountingPeriod(earliest)
			if err != nil {
				return nil, err
			}
		}
	}
	if firstOpen.After(target) {
		firstOpen = target
	}
	periodInputs, err := calculatePeriodsAccounting(ctx, db, householdID, firstOpen, target.AddDate(0, 1, 0))
	if err != nil {
		return nil, err
	}
	for month := firstOpen; month.Before(target); month = month.AddDate(0, 1, 0) {
		monthInputs := periodInputs[month.Format("2006-01")]
		for _, line := range monthInputs {
			value, ok := checkedAdd(carry[line.MemberID], line.AmountDueYen)
			if !ok {
				return nil, domain.ErrInvalidSettlement
			}
			carry[line.MemberID] = value
		}
	}
	current := periodInputs[period]
	for index := range current {
		current[index].CarryYen = carry[current[index].MemberID]
		amountDue, ok := checkedAdd(current[index].AmountDueYen, current[index].CarryYen)
		if !ok {
			return nil, domain.ErrInvalidSettlement
		}
		current[index].AmountDueYen = amountDue
	}
	return current, nil
}

func earlierPeriod(current, candidate string) string {
	if current == "" || candidate < current {
		return candidate
	}
	return current
}

func parseAccountingPeriod(value string) (time.Time, error) {
	return parsePeriod(value)
}
