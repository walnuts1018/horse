package query

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"math"
	"slices"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
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
	Account        *models.Account
	OpeningBalance int64
	ClosingBalance int64
	Changes        []AccountBalanceChange
}

type accountBalanceJournalChange struct {
	journal *models.Journal
	amount  int64
}

func AccountBalanceAnalysisForPeriod(ctx context.Context, db bob.Executor, householdID, accountID gofrsuuid.UUID, start, end time.Time) (*AccountBalanceAnalysis, error) {
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
	entries, err := models.JournalEntries.Query(sm.Where(models.JournalEntries.Columns.AccountID.EQ(psql.Arg(sql.Null[gofrsuuid.UUID]{V: accountID, Valid: true})))).All(ctx, db)
	if err != nil {
		return nil, err
	}
	if err := entries.LoadJournal(ctx, db); err != nil {
		return nil, err
	}
	changesByJournal := make(map[gofrsuuid.UUID]*accountBalanceJournalChange)
	for _, entry := range entries {
		journal := entry.R.Journal
		if journal == nil || journal.HouseholdID != householdID {
			continue
		}
		change := changesByJournal[journal.ID]
		if change == nil {
			change = &accountBalanceJournalChange{journal: journal}
			changesByJournal[journal.ID] = change
		}
		amount := entry.DebitYen - entry.CreditYen
		if account.Kind == "CARD" {
			amount = entry.CreditYen - entry.DebitYen
		}
		next, ok := checkedAccountBalanceAdd(change.amount, amount)
		if !ok {
			return nil, ErrAccountBalanceOverflow
		}
		change.amount = next
	}
	ordered := make([]*accountBalanceJournalChange, 0, len(changesByJournal))
	for _, change := range changesByJournal {
		ordered = append(ordered, change)
	}
	slices.SortFunc(ordered, func(left, right *accountBalanceJournalChange) int {
		if left.journal.OccurredAt.Before(right.journal.OccurredAt) {
			return -1
		}
		if left.journal.OccurredAt.After(right.journal.OccurredAt) {
			return 1
		}
		if left.journal.SourceKind == accountOpeningBalanceSourceKind && right.journal.SourceKind != accountOpeningBalanceSourceKind {
			return -1
		}
		if right.journal.SourceKind == accountOpeningBalanceSourceKind && left.journal.SourceKind != accountOpeningBalanceSourceKind {
			return 1
		}
		return cmp.Compare(left.journal.ID.String(), right.journal.ID.String())
	})
	var openingBalance int64
	var openingAt *time.Time
	for _, change := range ordered {
		if change.journal.SourceKind == accountOpeningBalanceSourceKind && change.journal.SourceID == accountID {
			occurredAt := change.journal.OccurredAt
			openingAt = &occurredAt
			break
		}
	}
	visible := make([]*accountBalanceJournalChange, 0, len(ordered))
	for _, change := range ordered {
		if openingAt != nil && change.journal.OccurredAt.Before(*openingAt) {
			continue
		}
		if change.journal.OccurredAt.Before(start) {
			var ok bool
			openingBalance, ok = checkedAccountBalanceAdd(openingBalance, change.amount)
			if !ok {
				return nil, ErrAccountBalanceOverflow
			}
			continue
		}
		if !change.journal.OccurredAt.Before(end) {
			continue
		}
		visible = append(visible, change)
	}
	descriptions, err := accountBalanceChangeDescriptions(ctx, db, householdID, account, visible)
	if err != nil {
		return nil, err
	}
	result := &AccountBalanceAnalysis{Account: account, OpeningBalance: openingBalance, Changes: make([]AccountBalanceChange, 0, len(visible))}
	balance := openingBalance
	for _, change := range visible {
		var ok bool
		balance, ok = checkedAccountBalanceAdd(balance, change.amount)
		if !ok {
			return nil, ErrAccountBalanceOverflow
		}
		description := descriptions[accountBalanceSourceKey(change.journal.SourceKind, change.journal.SourceID)]
		if description == "" {
			description = accountBalanceSourceDescription(change.journal.SourceKind)
		}
		result.Changes = append(result.Changes, AccountBalanceChange{Journal: change.journal, Amount: change.amount, Balance: balance, Description: description})
	}
	result.ClosingBalance = balance
	return result, nil
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
