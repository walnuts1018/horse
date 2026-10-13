package graph

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
	"github.com/walnuts1018/horse/internal/relayid"
)

func (r *queryResolver) node(ctx context.Context, id string) (model.Node, error) {
	kind, rawID, err := decodeGlobalNodeID(id)
	if err != nil {
		return nil, err
	}
	if kind == nodeTypeUnmatchedObservation {
		documentID, sourceReference, err := decodeUnmatchedNodeID(rawID)
		if err != nil {
			return nil, err
		}
		return r.findUnmatchedObservation(ctx, documentID, sourceReference)
	}
	if kind == nodeTypeSettlement {
		householdID, period, err := decodeSettlementNodeID(rawID)
		if err != nil {
			return nil, err
		}
		return r.settlement(ctx, householdID, period)
	}
	return r.findNode(ctx, kind, rawID)
}

func (r *queryResolver) nodes(ctx context.Context, ids []string) ([]model.Node, error) {
	result := make([]model.Node, len(ids))
	decoded := make([]decodedNodeID, len(ids))
	var households []*model.Household
	idsByKind := make(map[string][]uuid.UUID)
	indexesByKind := make(map[string]map[uuid.UUID][]int)
	for index, id := range ids {
		kind, rawID, err := decodeGlobalNodeID(id)
		if err != nil {
			return nil, err
		}
		decoded[index] = decodedNodeID{kind: kind, rawID: rawID}
		if kind != nodeTypeUnmatchedObservation && kind != nodeTypeSettlement {
			parsedID, err := uuid.FromString(rawID)
			if err != nil {
				return nil, fmt.Errorf("invalid %s node ID: %w", kind, err)
			}
			if indexesByKind[kind] == nil {
				indexesByKind[kind] = make(map[uuid.UUID][]int)
			}
			if _, exists := indexesByKind[kind][parsedID]; !exists {
				idsByKind[kind] = append(idsByKind[kind], parsedID)
			}
			indexesByKind[kind][parsedID] = append(indexesByKind[kind][parsedID], index)
		}
	}

	if len(idsByKind) > 0 {
		var err error
		households, err = r.households(ctx)
		if err != nil {
			return nil, err
		}
		resolved, err := r.loadBatchNodesByDomain(ctx, households, idsByKind, indexesByKind)
		if err != nil {
			return nil, err
		}
		for index, node := range resolved {
			result[index] = node
		}
		for index, nodeID := range decoded {
			if isBatchLoadedNodeKind(nodeID.kind) || nodeID.kind == nodeTypeUnmatchedObservation || nodeID.kind == nodeTypeSettlement {
				continue
			}
			node, err := r.findNodeInHouseholds(ctx, households, nodeID.kind, nodeID.rawID)
			if err != nil {
				return nil, err
			}
			result[index] = node
		}
	}

	for index, nodeID := range decoded {
		if nodeID.kind != nodeTypeUnmatchedObservation && nodeID.kind != nodeTypeSettlement {
			continue
		}
		node, err := r.node(ctx, ids[index])
		if err != nil {
			return nil, err
		}
		result[index] = node
	}
	return result, nil
}

func isBatchLoadedNodeKind(kind string) bool {
	switch kind {
	case nodeTypePurchase, nodeTypePurchaseItem, nodeTypePurchaseCorrection, nodeTypeFundingSchedule, nodeTypeAccountMovement, nodeTypeAccountIncome, nodeTypeHouseholdInvitation, nodeTypeTag, nodeTypeRecurringExpense, nodeTypeLoanBalance, nodeTypeAllocationRule, nodeTypeProductConcept, nodeTypeProductVariant, nodeTypeProductAlias, nodeTypeBudget, nodeTypeLedgerEntry:
		return true
	default:
		return false
	}
}

type decodedNodeID struct {
	kind  string
	rawID string
}

func decodeGlobalNodeID(globalID string) (string, string, error) {
	kind, payload, err := relayid.Decode(globalID)
	if err != nil {
		return "", "", err
	}
	if _, ok := nodeTypes[kind]; !ok {
		return "", "", fmt.Errorf("unknown node type %q", kind)
	}
	if kind == nodeTypeUnmatchedObservation || kind == nodeTypeSettlement {
		return kind, string(payload), nil
	}
	if len(payload) != 16 {
		return "", "", fmt.Errorf("invalid %s node ID payload", kind)
	}
	id := uuid.UUID(payload)
	if id == uuid.Nil {
		return "", "", fmt.Errorf("invalid %s node ID", kind)
	}
	return kind, id.String(), nil
}

func decodeUnmatchedNodeID(payload string) (string, string, error) {
	documentID, rest, ok := bytes.Cut([]byte(payload), []byte{0})
	if !ok || len(documentID) != 16 {
		return "", "", errors.New("invalid UnmatchedObservation node ID payload")
	}
	if len(rest) >= 8 && binary.BigEndian.Uint64(rest[:8]) == uint64(len(rest[8:])) {
		return uuid.UUID(documentID).String(), string(rest[8:]), nil
	}
	if len(rest) >= 4 && uint64(binary.BigEndian.Uint32(rest[:4])) == uint64(len(rest[4:])) {
		return uuid.UUID(documentID).String(), string(rest[4:]), nil
	}
	return "", "", errors.New("invalid UnmatchedObservation node ID payload")
}

func decodeSettlementNodeID(payload string) (string, string, error) {
	householdID, period, ok := strings.Cut(payload, ":")
	if !ok {
		return "", "", errors.New("invalid Settlement node ID payload")
	}
	if _, err := uuid.FromString(householdID); err != nil {
		return "", "", errors.New("invalid Settlement household ID")
	}
	if _, err := parseAccountingPeriod(period); err != nil {
		return "", "", errors.New("invalid Settlement period")
	}
	return householdID, period, nil
}

func (r *queryResolver) findNode(ctx context.Context, kind, id string) (model.Node, error) {
	households, err := r.households(ctx)
	if err != nil {
		return nil, err
	}
	return r.findNodeInHouseholds(ctx, households, kind, id)
}

func (r *queryResolver) findNodeInHouseholds(ctx context.Context, households []*model.Household, kind, id string) (model.Node, error) {
	var err error
	for _, household := range households {
		if kind == nodeTypeHousehold && household.ID == id {
			return household, nil
		}
		if kind == nodeTypeAccountOpeningBalance {
			account := findValue(household.Accounts, func(value *model.Account) bool { return value.ID == id })
			if account == nil {
				continue
			}
			openingBalance, err := models.Journals.Query(sm.Where(psql.And(
				models.Journals.Columns.HouseholdID.EQ(psql.Arg(mustUUID(household.ID))),
				models.Journals.Columns.SourceKind.EQ(psql.Arg(sourceKindAccountOpeningBalance)),
				models.Journals.Columns.SourceID.EQ(psql.Arg(mustUUID(id))),
			))).One(ctx, queryExecutor(r.DB))
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, err
			}
			globalID, err := encodeNodeID(nodeTypeAccountOpeningBalance, openingBalance.SourceID.String())
			if err != nil {
				return nil, err
			}
			return &model.AccountOpeningBalance{ID: globalID, Account: account}, nil
		}
		if kind == nodeTypeMember {
			if value := findValue(household.Members, func(value *model.Member) bool { return value.ID == id }); value != nil {
				return value, nil
			}
		}
		if kind == nodeTypePool {
			if value := findValue(household.PoolVariants, func(value model.PoolVariant) bool { return poolData(value).ID == id }); value != nil {
				return value, nil
			}
		}
		if kind == nodeTypeAccount {
			if value := findValue(household.Accounts, func(value *model.Account) bool { return value.ID == id }); value != nil {
				return value, nil
			}
		}
		if kind == nodeTypeLoanBalance {
			parsedID, _ := uuid.FromString(id)
			loan, err := models.Transfers.Query(sm.Where(psql.And(
				models.Transfers.Columns.ID.EQ(psql.Arg(parsedID)),
				models.Transfers.Columns.HouseholdID.EQ(psql.Arg(mustUUID(household.ID))),
				models.Transfers.Columns.Kind.EQ(psql.Arg(transferKindLoan)),
			))).One(ctx, queryExecutor(r.DB))
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, err
			}
			return loadLoanBalance(ctx, queryExecutor(r.DB), loan)
		}
		if kind == nodeTypeFundingSchedule {
			parsedID, _ := uuid.FromString(id)
			row, err := models.FundingSchedules.Query(sm.Where(psql.And(
				models.FundingSchedules.Columns.ID.EQ(psql.Arg(parsedID)),
				models.FundingSchedules.Columns.HouseholdID.EQ(psql.Arg(mustUUID(household.ID))),
			))).One(ctx, queryExecutor(r.DB))
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, err
			}
			return fundingScheduleModel(row), nil
		}
		if kind == nodeTypeCategory {
			if value := findValue(household.Categories, func(value *model.Category) bool { return value.ID == id }); value != nil {
				return value, nil
			}
		}
		if kind == nodeTypePurchase {
			purchase, err := loadPurchase(ctx, r.DB, id)
			if errors.Is(err, ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if purchase.HouseholdID != household.ID {
				continue
			}
			return purchase, nil
		}
		if kind == nodeTypePurchaseItem || kind == nodeTypePurchaseCorrection {
			parsedID, err := uuid.FromString(id)
			if err != nil {
				continue
			}
			var purchaseID uuid.UUID
			if kind == nodeTypePurchaseItem {
				row, err := models.PurchaseItems.Query(sm.Where(psql.And(
					models.PurchaseItems.Columns.ID.EQ(psql.Arg(parsedID)),
					models.PurchaseItems.Columns.HouseholdID.EQ(psql.Arg(mustUUID(household.ID))),
				))).One(ctx, queryExecutor(r.DB))
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				if err != nil {
					return nil, err
				}
				purchaseID = row.PurchaseID
			} else {
				row, err := models.Corrections.Query(sm.Where(psql.And(
					models.Corrections.Columns.ID.EQ(psql.Arg(parsedID)),
					models.Corrections.Columns.HouseholdID.EQ(psql.Arg(mustUUID(household.ID))),
				))).One(ctx, queryExecutor(r.DB))
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				if err != nil {
					return nil, err
				}
				purchaseID = row.PurchaseID
			}
			purchase, err := loadPurchase(ctx, r.DB, purchaseID.String())
			if errors.Is(err, ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if purchase.HouseholdID != household.ID {
				continue
			}
			if kind == nodeTypePurchaseItem {
				if value := findValue(purchase.Items, func(value *model.PurchaseItem) bool { return value.ID == id }); value != nil {
					return value, nil
				}
			} else if value := findValue(purchaseCorrectionNodes(purchase), func(value model.PurchaseCorrection) bool { return value.GetID() == id }); value != nil {
				return value, nil
			}
		}
		if kind == nodeTypeAllocationRule {
			value, err := loadAllocationRule(ctx, r.DB, id)
			if errors.Is(err, ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if value.HouseholdID == household.ID {
				return value, nil
			}
		}
		if kind == nodeTypeProductConcept || kind == nodeTypeProductVariant || kind == nodeTypeProductAlias {
			var value model.Node
			switch kind {
			case nodeTypeProductConcept:
				value, err = loadProductConcept(ctx, bobpgx.NewPool(r.DB), id)
			case nodeTypeProductVariant:
				value, err = loadProductVariant(ctx, bobpgx.NewPool(r.DB), id)
			case nodeTypeProductAlias:
				value, err = loadProductAlias(ctx, bobpgx.NewPool(r.DB), id)
			}
			if errors.Is(err, ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if nodeHouseholdID(value) == household.ID {
				return value, nil
			}
		}
		if kind == nodeTypeHouseholdInvitation {
			user, err := principal(ctx)
			if err != nil {
				return nil, err
			}
			executor := bobpgx.NewPool(r.DB)
			if err := requireHouseholdOwner(ctx, executor, household.ID, user.MemberID); err != nil {
				return nil, err
			}
			invitationID, err := uuid.FromString(id)
			if err != nil {
				return nil, ErrNotFound
			}
			row, err := models.HouseholdInvitations.Query(sm.Where(psql.And(
				models.HouseholdInvitations.Columns.ID.EQ(psql.Arg(invitationID)),
				models.HouseholdInvitations.Columns.HouseholdID.EQ(psql.Arg(mustUUID(household.ID))),
			))).One(ctx, executor)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, err
			}
			return householdInvitationModel(row, time.Now()), nil
		}
		if kind == nodeTypeTag {
			values, err := r.tags(ctx, household.ID, true)
			if err != nil {
				return nil, err
			}
			if value := findValue(values, func(value *model.Tag) bool { return value.ID == id }); value != nil {
				return value, nil
			}
		}
		if kind == nodeTypeRecurringExpense {
			values, err := r.recurringExpenses(ctx, household.ID, true)
			if err != nil {
				return nil, err
			}
			if value := findValue(values, func(value *model.RecurringExpense) bool { return value.ID == id }); value != nil {
				return value, nil
			}
		}
		if kind == nodeTypeAccountMovement {
			parsedID, _ := uuid.FromString(id)
			row, err := models.AccountMovements.Query(sm.Where(psql.And(models.AccountMovements.Columns.ID.EQ(psql.Arg(parsedID)), models.AccountMovements.Columns.HouseholdID.EQ(psql.Arg(mustUUID(household.ID)))))).One(ctx, queryExecutor(r.DB))
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, err
			}
			return accountMovementModel(row), nil
		}
		if kind == nodeTypeLedgerEntry {
			parsedID, _ := uuid.FromString(id)
			row, err := models.LedgerEntries.Query(sm.Where(psql.And(models.LedgerEntries.Columns.ID.EQ(psql.Arg(parsedID)), models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(mustUUID(household.ID)))))).One(ctx, queryExecutor(r.DB))
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, err
			}
			entry, err := loadLedgerEntry(ctx, r.DB, row.ID.String())
			if err != nil {
				return nil, err
			}
			return ledgerEntryVariant(ctx, r.DB, entry)
		}
		if kind == nodeTypeAccountIncome {
			parsedID, _ := uuid.FromString(id)
			row, err := models.AccountIncomes.Query(sm.Where(psql.And(models.AccountIncomes.Columns.ID.EQ(psql.Arg(parsedID)), models.AccountIncomes.Columns.HouseholdID.EQ(psql.Arg(mustUUID(household.ID)))))).One(ctx, queryExecutor(r.DB))
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, err
			}
			return accountIncomeModel(row), nil
		}
		if kind == nodeTypeBudget {
			parsedID, _ := uuid.FromString(id)
			row, err := models.Budgets.Query(sm.Where(psql.And(models.Budgets.Columns.ID.EQ(psql.Arg(parsedID)), models.Budgets.Columns.HouseholdID.EQ(psql.Arg(mustUUID(household.ID)))))).One(ctx, queryExecutor(r.DB))
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, err
			}
			month := time.Now().UTC()
			return budgetModel(ctx, r.DB, row, &month)
		}
	}
	return nil, nil
}

func nodeHouseholdID(node model.Node) string {
	switch value := node.(type) {
	case *model.ProductConcept:
		return value.HouseholdID
	case *model.ProductVariant:
		return value.HouseholdID
	case *model.ProductAlias:
		return value.HouseholdID
	default:
		return ""
	}
}

func findValue[T any, S ~[]T](values S, match func(T) bool) T {
	for _, value := range values {
		if match(value) {
			return value
		}
	}
	var empty T
	return empty
}

func mustUUID(value string) uuid.UUID {
	id, err := uuid.FromString(value)
	if err != nil {
		panic(err)
	}
	return id
}

func (r *queryResolver) findUnmatchedObservation(ctx context.Context, documentID, sourceReference string) (model.Node, error) {
	households, err := r.households(ctx)
	if err != nil {
		return nil, err
	}
	first := 100
	for _, household := range households {
		var after *string
		for {
			connection, err := r.unmatchedObservations(ctx, household.ID, nil, nil, nil, nil, after, nil, &first, nil)
			if err != nil {
				return nil, err
			}
			if value := findValue(connection.Edges, func(edge *model.UnmatchedObservationEdge) bool {
				return edge.Node.GetDocumentID() == documentID && edge.Node.GetSourceReference() == sourceReference
			}); value != nil {
				return value.Node, nil
			}
			if !connection.PageInfo.HasNextPage || connection.PageInfo.EndCursor == nil {
				break
			}
			after = connection.PageInfo.EndCursor
		}
	}
	return nil, nil
}
