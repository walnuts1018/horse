package query

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/fm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/wm"
	"github.com/stephenafamo/scan"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const accountOpeningBalanceSourceKind = "ACCOUNT_OPENING_BALANCE"

var ErrAccountBalanceOverflow = errors.New("account balance exceeds int64")

type AccountBalanceChange struct {
	Journal     *models.Journal
	Amount      int64
	Balance     int64
	Description string
}

type AccountBalanceAnalysis struct {
	Account         *models.Account
	OpeningBalance  int64
	ClosingBalance  int64
	Changes         []AccountBalanceChange
	TotalCount      int
	HasNextPage     bool
	HasPreviousPage bool
}

type accountBalanceJournalChange struct {
	journal *models.Journal
	amount  int64
	balance int64
}

type accountBalanceSummaryRow struct {
	OpeningBalance string              `db:"opening_balance"`
	ClosingBalance string              `db:"closing_balance"`
	OpeningAt      sql.Null[time.Time] `db:"opening_at"`
	TotalCount     int64               `db:"total_count"`
}

type accountBalancePageRow struct {
	JournalID   gofrsuuid.UUID `db:"journal_id"`
	HouseholdID gofrsuuid.UUID `db:"household_id"`
	SourceKind  string         `db:"source_kind"`
	SourceID    gofrsuuid.UUID `db:"source_id"`
	OccurredAt  time.Time      `db:"occurred_at"`
	OpeningRank int32          `db:"opening_rank"`
	Amount      string         `db:"amount"`
	Balance     string         `db:"balance"`
}

func AccountBalanceAnalysisForPeriod(ctx context.Context, db bob.Executor, householdID, accountID gofrsuuid.UUID, start, end time.Time, after *gofrsuuid.UUID, pageSize int) (*AccountBalanceAnalysis, error) {
	account, err := models.Accounts.Query(sm.Where(psql.And(
		models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Accounts.Columns.ID.EQ(psql.Arg(accountID)),
	))).One(ctx, db)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, err
	}
	if pageSize < 1 {
		return nil, errors.New("account balance analysis page size must be positive")
	}
	changesQuery, openingQuery, eligibleQuery := accountBalanceAnalysisQueries(householdID, accountID, account.Kind)
	openingBalanceQuery := psql.Select(
		sm.With("journal_changes", "journal_id", "household_id", "source_kind", "source_id", "occurred_at", "amount", "opening_rank").As(changesQuery),
		sm.With("opening_marker", "opened_at").As(openingQuery),
		sm.With("eligible_changes", "journal_id", "household_id", "source_kind", "source_id", "occurred_at", "amount", "opening_rank", "opened_at").As(eligibleQuery),
		sm.With("opening_balance", "balance").As(psql.Select(
			sm.Columns(psql.F("coalesce", psql.F("sum", psql.Quote("eligible_changes", "amount"))(), psql.Arg(int64(0)))().As("balance")),
			sm.From("eligible_changes"),
			sm.Where(psql.Quote("eligible_changes", "occurred_at").LT(psql.Arg(start))),
		)),
		sm.With("period_totals", "balance", "total_count").As(psql.Select(
			sm.Columns(
				psql.F("coalesce", psql.F("sum", psql.Quote("eligible_changes", "amount"))(), psql.Arg(int64(0)))().As("balance"),
				psql.F("count", psql.Quote("eligible_changes", "journal_id"))().As("total_count"),
			),
			sm.From("eligible_changes"),
			sm.Where(psql.And(
				psql.Quote("eligible_changes", "occurred_at").GTE(psql.Arg(start)),
				psql.Quote("eligible_changes", "occurred_at").LT(psql.Arg(end)),
			)),
		)),
		sm.Columns(
			psql.Quote("opening_balance", "balance").As("opening_balance"),
			psql.Quote("opening_balance", "balance").Plus(psql.Quote("period_totals", "balance")).As("closing_balance"),
			psql.Quote("opening_marker", "opened_at").As("opening_at"),
			psql.Quote("period_totals", "total_count"),
		),
		sm.From("opening_balance", sm.CrossJoin("period_totals"), sm.CrossJoin("opening_marker")),
	)
	summaryRows, err := bob.All(ctx, db, openingBalanceQuery, scan.StructMapper[accountBalanceSummaryRow]())
	if err != nil {
		return nil, fmt.Errorf("load account balance analysis summary: %w", err)
	}
	if len(summaryRows) != 1 {
		return nil, fmt.Errorf("account balance analysis summary returned %d rows", len(summaryRows))
	}
	summary := summaryRows[0]
	openingBalance, err := parseAccountBalanceInt(summary.OpeningBalance)
	if err != nil {
		return nil, err
	}
	closingBalance, err := parseAccountBalanceInt(summary.ClosingBalance)
	if err != nil {
		return nil, err
	}
	var cursor *models.Journal
	if after != nil {
		cursor, err = models.Journals.Query(sm.Where(psql.And(
			models.Journals.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Journals.Columns.ID.EQ(psql.Arg(*after)),
		))).One(ctx, db)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		if err != nil {
			return nil, err
		}
		hasAccountEntry, err := models.JournalEntries.Query(sm.Where(psql.And(
			models.JournalEntries.Columns.JournalID.EQ(psql.Arg(cursor.ID)),
			models.JournalEntries.Columns.AccountID.EQ(psql.Arg(sql.Null[gofrsuuid.UUID]{V: accountID, Valid: true})),
		))).Exists(ctx, db)
		if err != nil {
			return nil, err
		}
		if !hasAccountEntry || cursor.OccurredAt.Before(start) || !cursor.OccurredAt.Before(end) || summary.OpeningAt.Valid && cursor.OccurredAt.Before(summary.OpeningAt.V) {
			return nil, sql.ErrNoRows
		}
	}

	periodChanges := psql.Select(
		sm.With("journal_changes", "journal_id", "household_id", "source_kind", "source_id", "occurred_at", "amount", "opening_rank").As(changesQuery),
		sm.With("opening_marker", "opened_at").As(openingQuery),
		sm.With("eligible_changes", "journal_id", "household_id", "source_kind", "source_id", "occurred_at", "amount", "opening_rank", "opened_at").As(eligibleQuery),
		sm.With("opening_balance", "balance").As(psql.Select(
			sm.Columns(psql.F("coalesce", psql.F("sum", psql.Quote("eligible_changes", "amount"))(), psql.Arg(int64(0)))().As("balance")),
			sm.From("eligible_changes"),
			sm.Where(psql.Quote("eligible_changes", "occurred_at").LT(psql.Arg(start))),
		)),
		sm.With("period_changes", "journal_id", "household_id", "source_kind", "source_id", "occurred_at", "amount", "opening_rank", "balance").As(psql.Select(
			sm.Columns(
				psql.Quote("eligible_changes", "journal_id"),
				psql.Quote("eligible_changes", "household_id"),
				psql.Quote("eligible_changes", "source_kind"),
				psql.Quote("eligible_changes", "source_id"),
				psql.Quote("eligible_changes", "occurred_at"),
				psql.Quote("eligible_changes", "amount"),
				psql.Quote("eligible_changes", "opening_rank"),
				psql.Quote("opening_balance", "balance").Plus(psql.F("sum", psql.Quote("eligible_changes", "amount"))(fm.Over(
					wm.OrderBy(psql.Quote("eligible_changes", "occurred_at")),
					wm.OrderBy(psql.Quote("eligible_changes", "opening_rank")),
					wm.OrderBy(psql.Quote("eligible_changes", "journal_id")),
					wm.Rows(), wm.FromUnboundedPreceding(), wm.ToCurrentRow(),
				))).As("balance"),
			),
			sm.From("eligible_changes", sm.CrossJoin("opening_balance")),
			sm.Where(psql.And(
				psql.Quote("eligible_changes", "occurred_at").GTE(psql.Arg(start)),
				psql.Quote("eligible_changes", "occurred_at").LT(psql.Arg(end)),
			)),
		)),
		sm.Columns(
			psql.Quote("period_changes", "journal_id"),
			psql.Quote("period_changes", "household_id"),
			psql.Quote("period_changes", "source_kind"),
			psql.Quote("period_changes", "source_id"),
			psql.Quote("period_changes", "occurred_at"),
			psql.Quote("period_changes", "opening_rank"),
			psql.Quote("period_changes", "amount"),
			psql.Quote("period_changes", "balance"),
		),
		sm.From("period_changes"),
		sm.Where(accountBalanceCursorFilter(cursor)),
		sm.OrderBy(psql.Quote("period_changes", "occurred_at")).Desc(),
		sm.OrderBy(psql.Quote("period_changes", "opening_rank")).Desc(),
		sm.OrderBy(psql.Quote("period_changes", "journal_id")).Desc(),
		sm.Limit(pageSize+1),
	)
	pageRows, err := bob.All(ctx, db, periodChanges, scan.StructMapper[accountBalancePageRow]())
	if err != nil {
		return nil, fmt.Errorf("load account balance analysis page: %w", err)
	}
	hasNextPage := len(pageRows) > pageSize
	if hasNextPage {
		pageRows = pageRows[:pageSize]
	}
	page := make([]*accountBalanceJournalChange, 0, len(pageRows))
	for _, row := range pageRows {
		amount, err := parseAccountBalanceInt(row.Amount)
		if err != nil {
			return nil, err
		}
		balance, err := parseAccountBalanceInt(row.Balance)
		if err != nil {
			return nil, err
		}
		page = append(page, &accountBalanceJournalChange{
			journal: &models.Journal{ID: row.JournalID, HouseholdID: row.HouseholdID, SourceKind: row.SourceKind, SourceID: row.SourceID, OccurredAt: row.OccurredAt},
			amount:  amount, balance: balance,
		})
	}
	descriptions, err := accountBalanceChangeDescriptions(ctx, db, householdID, account, page)
	if err != nil {
		return nil, fmt.Errorf("load account balance analysis descriptions: %w", err)
	}
	changes := make([]AccountBalanceChange, 0, len(page))
	for _, change := range page {
		description := descriptions[accountBalanceSourceKey(change.journal.SourceKind, change.journal.SourceID)]
		if description == "" {
			description = accountBalanceSourceDescription(change.journal.SourceKind)
		}
		changes = append(changes, AccountBalanceChange{Journal: change.journal, Amount: change.amount, Balance: change.balance, Description: description})
	}
	result := &AccountBalanceAnalysis{
		Account: account, OpeningBalance: openingBalance, ClosingBalance: closingBalance, Changes: changes,
		TotalCount: int(summary.TotalCount), HasNextPage: hasNextPage, HasPreviousPage: after != nil,
	}
	return result, nil
}

func accountBalanceAnalysisQueries(householdID, accountID gofrsuuid.UUID, accountKind string) (bob.Query, bob.Query, bob.Query) {
	accountRef := sql.Null[gofrsuuid.UUID]{V: accountID, Valid: true}
	signedAmount := models.JournalEntries.Columns.DebitYen.Minus(models.JournalEntries.Columns.CreditYen)
	if accountKind == "CARD" {
		signedAmount = models.JournalEntries.Columns.CreditYen.Minus(models.JournalEntries.Columns.DebitYen)
	}
	openingRank := psql.Case().When(
		models.Journals.Columns.SourceKind.EQ(psql.Arg(accountOpeningBalanceSourceKind)), psql.Cast(psql.Arg(int32(0)), "integer"),
	).Else(psql.Cast(psql.Arg(int32(1)), "integer")).As("opening_rank")
	journalChanges := psql.Select(
		sm.Columns(
			models.Journals.Columns.ID.As("journal_id"),
			models.Journals.Columns.HouseholdID.As("household_id"),
			models.Journals.Columns.SourceKind.As("source_kind"),
			models.Journals.Columns.SourceID.As("source_id"),
			models.Journals.Columns.OccurredAt.As("occurred_at"),
			psql.F("sum", signedAmount)().As("amount"),
			openingRank,
		),
		sm.From(models.JournalEntries.NameExpr(), sm.InnerJoin(models.Journals.NameExpr()).On(models.JournalEntries.Columns.JournalID.EQ(models.Journals.Columns.ID))),
		sm.Where(psql.And(
			models.Journals.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.JournalEntries.Columns.AccountID.EQ(psql.Arg(accountRef)),
		)),
		sm.GroupBy(models.Journals.Columns.ID),
		sm.GroupBy(models.Journals.Columns.HouseholdID),
		sm.GroupBy(models.Journals.Columns.SourceKind),
		sm.GroupBy(models.Journals.Columns.SourceID),
		sm.GroupBy(models.Journals.Columns.OccurredAt),
	)
	openingMarker := psql.Select(
		sm.Columns(psql.F("min", psql.Quote("journal_changes", "occurred_at"))().As("opened_at")),
		sm.From("journal_changes"),
		sm.Where(psql.And(
			psql.Quote("journal_changes", "source_kind").EQ(psql.Arg(accountOpeningBalanceSourceKind)),
			psql.Quote("journal_changes", "source_id").EQ(psql.Arg(accountID)),
		)),
	)
	openingAt := psql.Quote("opening_marker", "opened_at")
	eligibleChanges := psql.Select(
		sm.Columns(
			psql.Quote("journal_changes", "journal_id"),
			psql.Quote("journal_changes", "household_id"),
			psql.Quote("journal_changes", "source_kind"),
			psql.Quote("journal_changes", "source_id"),
			psql.Quote("journal_changes", "occurred_at"),
			psql.Quote("journal_changes", "amount"),
			psql.Quote("journal_changes", "opening_rank"),
			openingAt.As("opened_at"),
		),
		sm.From("journal_changes", sm.CrossJoin("opening_marker")),
		sm.Where(psql.Or(openingAt.IsNull(), psql.Quote("journal_changes", "occurred_at").GTE(openingAt))),
	)

	return journalChanges, openingMarker, eligibleChanges
}

func accountBalanceCursorFilter(cursor *models.Journal) bob.Expression {
	if cursor == nil {
		return psql.Arg(true)
	}
	occurredAt := psql.Quote("period_changes", "occurred_at")
	openingRank := psql.Quote("period_changes", "opening_rank")
	journalID := psql.Quote("period_changes", "journal_id")
	cursorRank := int32(1)
	if cursor.SourceKind == accountOpeningBalanceSourceKind {
		cursorRank = 0
	}

	return psql.Or(
		occurredAt.LT(psql.Arg(cursor.OccurredAt)),
		psql.And(occurredAt.EQ(psql.Arg(cursor.OccurredAt)), openingRank.LT(psql.Arg(cursorRank))),
		psql.And(occurredAt.EQ(psql.Arg(cursor.OccurredAt)), openingRank.EQ(psql.Arg(cursorRank)), journalID.LT(psql.Arg(cursor.ID))),
	)
}

func parseAccountBalanceInt(value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, ErrAccountBalanceOverflow
	}

	return parsed, nil
}

func AccountBalanceTransfer(ctx context.Context, executor bob.Executor, householdID, transferID gofrsuuid.UUID) (*models.Transfer, error) {
	return models.Transfers.Query(sm.Where(psql.And(
		models.Transfers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Transfers.Columns.ID.EQ(psql.Arg(transferID)),
	))).One(ctx, executor)
}

func accountBalanceChangeDescriptions(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, account *models.Account, changes []*accountBalanceJournalChange) (map[string]string, error) {
	idsByKind := make(map[string]map[gofrsuuid.UUID]struct{})
	for _, change := range changes {
		kind := change.journal.SourceKind
		if kind == accountOpeningBalanceSourceKind {
			continue
		}
		if idsByKind[kind] == nil {
			idsByKind[kind] = make(map[gofrsuuid.UUID]struct{})
		}
		idsByKind[kind][change.journal.SourceID] = struct{}{}
	}
	result := make(map[string]string)
	if account.Name != "" {
		result[accountBalanceSourceKey(accountOpeningBalanceSourceKind, account.ID)] = "期首残高: " + account.Name
	}
	for kind, ids := range idsByKind {
		args := make([]bob.Expression, 0, len(ids))
		for id := range ids {
			args = append(args, psql.Arg(id))
		}
		switch kind {
		case "PURCHASE":
			rows, err := models.Purchases.Query(sm.Where(psql.And(models.Purchases.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.Purchases.Columns.ID.In(args...)))).All(ctx, executor)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				result[accountBalanceSourceKey(kind, row.ID)] = row.Merchant
			}
		case "CORRECTION":
			rows, err := models.Corrections.Query(sm.Where(psql.And(models.Corrections.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.Corrections.Columns.ID.In(args...)))).All(ctx, executor)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				result[accountBalanceSourceKey(kind, row.ID)] = row.Reason
			}
		case "FUNDING":
			rows, err := models.LedgerEntries.Query(sm.Where(psql.And(models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.LedgerEntries.Columns.ID.In(args...)))).All(ctx, executor)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				result[accountBalanceSourceKey(kind, row.ID)] = row.Description
			}
		case "TRANSFER":
			rows, err := models.Transfers.Query(sm.Where(psql.And(models.Transfers.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.Transfers.Columns.ID.In(args...)))).All(ctx, executor)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				result[accountBalanceSourceKey(kind, row.ID)] = row.Description
			}
		case "ACCOUNT_MOVEMENT":
			rows, err := models.AccountMovements.Query(sm.Where(psql.And(models.AccountMovements.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.AccountMovements.Columns.ID.In(args...)))).All(ctx, executor)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				result[accountBalanceSourceKey(kind, row.ID)] = row.Description
			}
		case "ACCOUNT_INCOME":
			rows, err := models.AccountIncomes.Query(sm.Where(psql.And(models.AccountIncomes.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.AccountIncomes.Columns.ID.In(args...)))).All(ctx, executor)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				result[accountBalanceSourceKey(kind, row.ID)] = row.Description
			}
		}
	}
	return result, nil
}

func accountBalanceSourceKey(kind string, id gofrsuuid.UUID) string { return kind + ":" + id.String() }

func accountBalanceSourceDescription(kind string) string {
	switch kind {
	case accountOpeningBalanceSourceKind:
		return "初期残高"
	case "PURCHASE":
		return "購入"
	case "CORRECTION":
		return "訂正"
	case "FUNDING":
		return "資金拠出"
	case "TRANSFER":
		return "送金"
	case "ACCOUNT_MOVEMENT":
		return "口座振替"
	case "ACCOUNT_INCOME":
		return "外部入金"
	default:
		return "仕訳"
	}
}

func checkedAccountBalanceAdd(left, right int64) (int64, bool) {
	if right > 0 && left > math.MaxInt64-right || right < 0 && left < math.MinInt64-right {
		return 0, false
	}
	return left + right, true
}

func checkedAccountBalanceSubtract(left, right int64) (int64, bool) {
	if right > 0 && left < math.MinInt64+right || right < 0 && left > math.MaxInt64+right {
		return 0, false
	}
	return left - right, true
}
