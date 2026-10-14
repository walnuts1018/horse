package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
	postgresquery "github.com/walnuts1018/horse/internal/postgres/query"
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
	analysis, err := postgresquery.AccountBalanceAnalysisForPeriod(ctx, queryExecutor(r.DB), householdUUID, accountUUID, start, end)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if errors.Is(err, postgresquery.ErrAccountBalanceOverflow) {
		return nil, fmt.Errorf("%w: account balance exceeds int64", ErrBadInput)
	}
	if err != nil {
		return nil, err
	}
	startIndex := 0
	if cursorID, err := cursorID(after, cursorTypeAccountBalanceChange); err != nil {
		return nil, err
	} else if cursorID != nil {
		cursorUUID, err := gofrsuuid.FromString(*cursorID)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid account balance change cursor", ErrBadInput)
		}
		cursorIndex := slices.IndexFunc(analysis.Changes, func(change postgresquery.AccountBalanceChange) bool {
			return change.Journal.ID == cursorUUID
		})
		if cursorIndex < 0 {
			return nil, ErrNotFound
		}
		startIndex = len(analysis.Changes) - cursorIndex
	}
	orderedChanges := slices.Clone(analysis.Changes)
	slices.Reverse(orderedChanges)
	endIndex := min(startIndex+limit, len(orderedChanges))
	connection := &model.AccountBalanceChangeConnection{
		Edges: make([]*model.AccountBalanceChangeEdge, 0, endIndex-startIndex),
		PageInfo: &model.PageInfo{
			HasNextPage:     endIndex < len(orderedChanges),
			HasPreviousPage: startIndex > 0,
		},
		TotalCount: len(orderedChanges),
	}
	for index := startIndex; index < endIndex; index++ {
		change := orderedChanges[index]
		source, err := r.accountBalanceSource(ctx, householdID, analysis.Account, change.Journal)
		if err != nil {
			return nil, err
		}
		connection.Edges = append(connection.Edges, &model.AccountBalanceChangeEdge{
			Cursor: encodeCursor(cursorTypeAccountBalanceChange, change.Journal.ID.String()),
			Node: &model.AccountBalanceChange{
				OccurredAt: timeString(change.Journal.OccurredAt), Source: source,
				Description: change.Description, ChangeYen: graphLong(change.Amount), BalanceYen: graphLong(change.Balance),
			},
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
		OpeningBalanceYen: graphLong(analysis.OpeningBalance), ClosingBalanceYen: graphLong(analysis.ClosingBalance), Changes: connection,
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
		transfer, err := postgresquery.AccountBalanceTransfer(ctx, queryExecutor(r.DB), householdUUID, transferID)
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
