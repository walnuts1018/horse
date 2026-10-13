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

func (r *queryResolver) accountBalanceAnalysis(ctx context.Context, householdID, accountID, from, to string, first *int, after *string) (*model.AccountBalanceAnalysis, error) {
	limit, _, err := pageDirection(first, nil)
	if err != nil {
		return nil, err
	}
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
		if left.journal.SourceKind == sourceKindAccountOpeningBalance && right.journal.SourceKind != sourceKindAccountOpeningBalance {
			return -1
		}
		if right.journal.SourceKind == sourceKindAccountOpeningBalance && left.journal.SourceKind != sourceKindAccountOpeningBalance {
			return 1
		}
		return cmp.Compare(left.journal.ID.String(), right.journal.ID.String())
	})
	var openingBalance int64
	var visible []*accountBalanceJournalChange
	var openingAt *time.Time
	for _, change := range ordered {
		if change.journal.SourceKind == sourceKindAccountOpeningBalance && change.journal.SourceID == accountUUID {
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
		source, err := r.accountBalanceSource(ctx, householdID, account, change.journal)
		if err != nil {
			return nil, err
		}
		description := descriptions[accountSourceKey(sourceKind, change.journal.SourceID)]
		if description == "" {
			description = accountSourceDescription(sourceKind)
		}
		result = append(result, &model.AccountBalanceChange{
			OccurredAt: timeString(change.journal.OccurredAt), Source: source,
			Description: description, ChangeYen: graphLong(change.amount), BalanceYen: graphLong(balance),
		})
	}
	startIndex := 0
	if cursorID, err := cursorID(after, cursorTypeAccountBalanceChange); err != nil {
		return nil, err
	} else if cursorID != nil {
		cursorUUID, err := gofrsuuid.FromString(*cursorID)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid account balance change cursor", ErrBadInput)
		}
		cursorIndex := slices.IndexFunc(visible, func(change *accountBalanceJournalChange) bool {
			return change.journal.ID == cursorUUID
		})
		if cursorIndex < 0 {
			return nil, ErrNotFound
		}
		startIndex = len(visible) - cursorIndex
	}
	slices.Reverse(visible)
	slices.Reverse(result)
	endIndex := min(startIndex+limit, len(result))
	connection := &model.AccountBalanceChangeConnection{
		Edges: make([]*model.AccountBalanceChangeEdge, 0, endIndex-startIndex),
		PageInfo: &model.PageInfo{
			HasNextPage:     endIndex < len(result),
			HasPreviousPage: startIndex > 0,
		},
		TotalCount: len(result),
	}
	for index := startIndex; index < endIndex; index++ {
		connection.Edges = append(connection.Edges, &model.AccountBalanceChangeEdge{
			Cursor: encodeCursor(cursorTypeAccountBalanceChange, visible[index].journal.ID.String()),
			Node:   result[index],
		})
	}
	if len(connection.Edges) > 0 {
		startCursor := connection.Edges[0].Cursor
		endCursor := connection.Edges[len(connection.Edges)-1].Cursor
		connection.PageInfo.StartCursor = &startCursor
		connection.PageInfo.EndCursor = &endCursor
	}
	return &model.AccountBalanceAnalysis{
		AccountID: accountUUID.String(), Currency: currencyJPY, From: model.YearMonth(from), To: model.YearMonth(to),
		OpeningBalanceYen: graphLong(openingBalance), ClosingBalanceYen: graphLong(balance), Changes: connection,
	}, nil
}

func (r *queryResolver) accountBalanceSource(ctx context.Context, householdID string, account *models.Account, journal *models.Journal) (model.AccountBalanceSource, error) {
	kind, id := "", journal.SourceID.String()
	switch journal.SourceKind {
	case sourceKindAccountOpeningBalance:
		globalID, err := encodeNodeID(nodeTypeAccountOpeningBalance, account.ID.String())
		if err != nil {
			return nil, err
		}
		return &model.AccountOpeningBalance{
			ID:      globalID,
			Account: &model.Account{ID: account.ID.String(), Name: account.Name, Kind: model.AccountKind(account.Kind)},
		}, nil
	case sourceKindPurchase:
		kind = nodeTypePurchase
	case sourceKindCorrection:
		kind = nodeTypePurchaseCorrection
	case sourceKindFunding:
		kind = nodeTypeLedgerEntry
	case sourceKindTransfer:
		transferID := journal.SourceID
		householdUUID, err := gofrsuuid.FromString(householdID)
		if err != nil {
			return nil, ErrNotFound
		}
		transfer, err := models.Transfers.Query(sm.Where(psql.And(
			models.Transfers.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			models.Transfers.Columns.ID.EQ(psql.Arg(transferID)),
		))).One(ctx, queryExecutor(r.DB))
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		kind, id = nodeTypeLedgerEntry, transfer.LedgerEntryID.String()
	case sourceKindAccountMovement:
		kind = nodeTypeAccountMovement
	case sourceKindAccountIncome:
		kind = nodeTypeAccountIncome
	default:
		return nil, fmt.Errorf("unknown account balance source kind %q", journal.SourceKind)
	}
	globalID, err := encodeNodeID(kind, id)
	if err != nil {
		return nil, err
	}
	node, err := r.node(ctx, globalID)
	if err != nil {
		return nil, err
	}
	source, ok := node.(model.AccountBalanceSource)
	if !ok {
		return nil, fmt.Errorf("node type %T cannot be an account balance source", node)
	}
	return source, nil
}

func accountSourceDescription(kind string) string {
	switch kind {
	case sourceKindAccountOpeningBalance:
		return "初期残高"
	case sourceKindPurchase:
		return "購入"
	case sourceKindCorrection:
		return "訂正"
	case sourceKindFunding:
		return "資金拠出"
	case sourceKindTransfer:
		return "送金"
	case sourceKindAccountMovement:
		return "口座振替"
	case sourceKindAccountIncome:
		return "外部入金"
	default:
		return "仕訳"
	}
}

type accountBalanceJournalChange struct {
	journal *models.Journal
	amount  int64
}

func accountChangeDescriptions(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, account *models.Account, changes []*accountBalanceJournalChange) (map[string]string, error) {
	idsByKind := make(map[string]map[gofrsuuid.UUID]struct{})
	for _, change := range changes {
		kind := change.journal.SourceKind
		if kind == sourceKindAccountOpeningBalance {
			continue
		}
		if idsByKind[kind] == nil {
			idsByKind[kind] = make(map[gofrsuuid.UUID]struct{})
		}
		idsByKind[kind][change.journal.SourceID] = struct{}{}
	}
	result := make(map[string]string)
	if account.Name != "" {
		result[accountSourceKey(sourceKindAccountOpeningBalance, account.ID)] = "期首残高: " + account.Name
	}
	for kind, ids := range idsByKind {
		args := make([]bob.Expression, 0, len(ids))
		for id := range ids {
			args = append(args, psql.Arg(id))
		}
		switch kind {
		case sourceKindPurchase:
			rows, err := models.Purchases.Query(sm.Where(psql.And(models.Purchases.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.Purchases.Columns.ID.In(args...)))).All(ctx, executor)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				result[accountSourceKey(kind, row.ID)] = row.Merchant
			}
		case sourceKindCorrection:
			rows, err := models.Corrections.Query(sm.Where(psql.And(models.Corrections.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.Corrections.Columns.ID.In(args...)))).All(ctx, executor)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				result[accountSourceKey(kind, row.ID)] = row.Reason
			}
		case sourceKindFunding:
			rows, err := models.LedgerEntries.Query(sm.Where(psql.And(models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.LedgerEntries.Columns.ID.In(args...)))).All(ctx, executor)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				result[accountSourceKey(kind, row.ID)] = row.Description
			}
		case sourceKindTransfer:
			rows, err := models.Transfers.Query(sm.Where(psql.And(models.Transfers.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.Transfers.Columns.ID.In(args...)))).All(ctx, executor)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				result[accountSourceKey(kind, row.ID)] = row.Description
			}
		case sourceKindAccountMovement:
			rows, err := models.AccountMovements.Query(sm.Where(psql.And(models.AccountMovements.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.AccountMovements.Columns.ID.In(args...)))).All(ctx, executor)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				result[accountSourceKey(kind, row.ID)] = row.Description
			}
		case sourceKindAccountIncome:
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
