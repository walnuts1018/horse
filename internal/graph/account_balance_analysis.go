package graph

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (r *queryResolver) accountBalanceAnalysis(ctx context.Context, householdID, accountID, from, to string) (*model.AccountBalanceAnalysis, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, ErrNotFound
	}
	accountUUID, err := gofrsuuid.FromString(accountID)
	if err != nil {
		return nil, ErrNotFound
	}
	fromMonth, err := parseAccountingPeriod(from)
	if err != nil {
		return nil, fmt.Errorf("%w: from must use YYYY-MM", ErrBadInput)
	}
	toMonth, err := parseAccountingPeriod(to)
	if err != nil || toMonth.Before(fromMonth) || toMonth.After(fromMonth.AddDate(10, 0, 0)) {
		return nil, fmt.Errorf("%w: to must be within ten years after from", ErrBadInput)
	}
	start, end := fromMonth.UTC(), toMonth.AddDate(0, 1, 0).UTC()
	executor := bobpgx.NewPool(r.DB)
	account, err := models.Accounts.Query(sm.Where(psql.And(
		models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.Accounts.Columns.ID.EQ(psql.Arg(accountUUID)),
	))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	entries, err := models.JournalEntries.Query(sm.Where(models.JournalEntries.Columns.AccountID.EQ(psql.Arg(sql.Null[gofrsuuid.UUID]{V: accountUUID, Valid: true})))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	if err := entries.LoadJournal(ctx, executor); err != nil {
		return nil, err
	}
	changesByJournal := make(map[gofrsuuid.UUID]*accountBalanceJournalChange)
	for _, entry := range entries {
		journal := entry.R.Journal
		if journal == nil || journal.HouseholdID != householdUUID {
			continue
		}
		change := changesByJournal[journal.ID]
		if change == nil {
			change = &accountBalanceJournalChange{journal: journal}
			changesByJournal[journal.ID] = change
		}
		next, ok := checkedAdd(change.amount, accountEntryBalance(account.Kind, entry.DebitYen, entry.CreditYen))
		if !ok {
			return nil, fmt.Errorf("%w: account journal change exceeds int64", ErrBadInput)
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
		if left.journal.SourceKind == "ACCOUNT_OPENING_BALANCE" && right.journal.SourceKind != "ACCOUNT_OPENING_BALANCE" {
			return -1
		}
		if right.journal.SourceKind == "ACCOUNT_OPENING_BALANCE" && left.journal.SourceKind != "ACCOUNT_OPENING_BALANCE" {
			return 1
		}
		return cmp.Compare(left.journal.ID.String(), right.journal.ID.String())
	})
	var openingBalance int64
	var visible []*accountBalanceJournalChange
	var openingAt *time.Time
	for _, change := range ordered {
		if change.journal.SourceKind == "ACCOUNT_OPENING_BALANCE" && change.journal.SourceID == accountUUID {
			occurredAt := change.journal.OccurredAt
			openingAt = &occurredAt
			break
		}
	}
	for _, change := range ordered {
		if openingAt != nil && change.journal.OccurredAt.Before(*openingAt) {
			continue
		}
		if change.journal.OccurredAt.Before(start) {
			var ok bool
			openingBalance, ok = checkedAdd(openingBalance, change.amount)
			if !ok {
				return nil, fmt.Errorf("%w: account opening balance exceeds int64", ErrBadInput)
			}
			continue
		}
		if !change.journal.OccurredAt.Before(end) {
			continue
		}
		visible = append(visible, change)
	}
	descriptions, err := accountChangeDescriptions(ctx, executor, householdUUID, account, visible)
	if err != nil {
		return nil, err
	}
	balance := openingBalance
	result := make([]*model.AccountBalanceChange, 0, len(visible))
	for _, change := range visible {
		var ok bool
		balance, ok = checkedAdd(balance, change.amount)
		if !ok {
			return nil, fmt.Errorf("%w: account running balance exceeds int64", ErrBadInput)
		}
		sourceKind := change.journal.SourceKind
		sourceID := change.journal.SourceID.String()
		description := descriptions[accountSourceKey(sourceKind, change.journal.SourceID)]
		if description == "" {
			description = sourceKind
		}
		result = append(result, &model.AccountBalanceChange{
			OccurredAt: timeString(change.journal.OccurredAt), SourceKind: sourceKind, SourceID: sourceID,
			Description: description, ChangeYen: graphLong(change.amount), BalanceYen: graphLong(balance),
		})
	}
	return &model.AccountBalanceAnalysis{
		AccountID: accountUUID.String(), Currency: "JPY", From: from, To: to,
		OpeningBalanceYen: graphLong(openingBalance), ClosingBalanceYen: graphLong(balance), Changes: result,
	}, nil
}

type accountBalanceJournalChange struct {
	journal *models.Journal
	amount  int64
}

func accountChangeDescriptions(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, account *models.Account, changes []*accountBalanceJournalChange) (map[string]string, error) {
	idsByKind := make(map[string]map[gofrsuuid.UUID]struct{})
	for _, change := range changes {
		kind := change.journal.SourceKind
		if kind == "ACCOUNT_OPENING_BALANCE" {
			continue
		}
		if idsByKind[kind] == nil {
			idsByKind[kind] = make(map[gofrsuuid.UUID]struct{})
		}
		idsByKind[kind][change.journal.SourceID] = struct{}{}
	}
	result := make(map[string]string)
	if account.Name != "" {
		result[accountSourceKey("ACCOUNT_OPENING_BALANCE", account.ID)] = "期首残高: " + account.Name
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
				result[accountSourceKey(kind, row.ID)] = row.Merchant
			}
		case "CORRECTION":
			rows, err := models.Corrections.Query(sm.Where(psql.And(models.Corrections.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.Corrections.Columns.ID.In(args...)))).All(ctx, executor)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				result[accountSourceKey(kind, row.ID)] = row.Reason
			}
		case "FUNDING":
			rows, err := models.LedgerEntries.Query(sm.Where(psql.And(models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.LedgerEntries.Columns.ID.In(args...)))).All(ctx, executor)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				result[accountSourceKey(kind, row.ID)] = row.Description
			}
		case "TRANSFER":
			rows, err := models.Transfers.Query(sm.Where(psql.And(models.Transfers.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.Transfers.Columns.ID.In(args...)))).All(ctx, executor)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				result[accountSourceKey(kind, row.ID)] = row.Description
			}
		case "ACCOUNT_MOVEMENT":
			rows, err := models.AccountMovements.Query(sm.Where(psql.And(models.AccountMovements.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.AccountMovements.Columns.ID.In(args...)))).All(ctx, executor)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				result[accountSourceKey(kind, row.ID)] = row.Description
			}
		case "ACCOUNT_INCOME":
			rows, err := models.AccountIncomes.Query(sm.Where(psql.And(models.AccountIncomes.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.AccountIncomes.Columns.ID.In(args...)))).All(ctx, executor)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				result[accountSourceKey(kind, row.ID)] = row.Description
			}
		}
	}
	return result, nil
}

func accountSourceKey(kind string, id gofrsuuid.UUID) string { return kind + ":" + id.String() }
