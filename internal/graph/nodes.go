package graph

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (r *queryResolver) node(ctx context.Context, id string) (model.Node, error) {
	kind, rawID, err := decodeGlobalNodeID(id)
	if err != nil {
		return nil, err
	}
	if kind == "UnmatchedObservation" {
		documentID, sourceReference, err := decodeUnmatchedNodeID(rawID)
		if err != nil {
			return nil, err
		}
		return r.findUnmatchedObservation(ctx, documentID, sourceReference)
	}
	return r.findNode(ctx, kind, rawID)
}

func (r *queryResolver) nodes(ctx context.Context, ids []string) ([]model.Node, error) {
	result := make([]model.Node, len(ids))
	for index, id := range ids {
		node, err := r.node(ctx, id)
		if err != nil {
			return nil, err
		}
		result[index] = node
	}
	return result, nil
}

func decodeGlobalNodeID(globalID string) (string, string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(globalID)
	if err != nil {
		return "", "", fmt.Errorf("invalid node ID: %w", err)
	}
	kindBytes, payload, ok := bytes.Cut(decoded, []byte{':'})
	if !ok {
		return "", "", errors.New("invalid node ID payload")
	}
	kind := string(kindBytes)
	if _, ok := nodeTypes[kind]; !ok {
		return "", "", fmt.Errorf("unknown node type %q", kind)
	}
	if kind == "UnmatchedObservation" {
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

func (r *queryResolver) findNode(ctx context.Context, kind, id string) (model.Node, error) {
	households, err := r.households(ctx)
	if err != nil {
		return nil, err
	}
	for _, household := range households {
		if kind == "Household" && household.ID == id {
			return household, nil
		}
		if kind == "Member" {
			if value := findValue(household.Members, func(value *model.Member) bool { return value.ID == id }); value != nil {
				return value, nil
			}
		}
		if kind == "Pool" {
			if value := findValue(household.Pools, func(value *model.Pool) bool { return value.ID == id }); value != nil {
				return value, nil
			}
		}
		if kind == "Account" {
			if value := findValue(household.Accounts, func(value *model.Account) bool { return value.ID == id }); value != nil {
				return value, nil
			}
		}
		if kind == "LoanBalance" {
			parsedID, _ := uuid.FromString(id)
			loan, err := models.Transfers.Query(sm.Where(psql.And(
				models.Transfers.Columns.ID.EQ(psql.Arg(parsedID)),
				models.Transfers.Columns.HouseholdID.EQ(psql.Arg(mustUUID(household.ID))),
				models.Transfers.Columns.Kind.EQ(psql.Arg("LOAN")),
			))).One(ctx, queryExecutor(r.DB))
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, err
			}
			return loadLoanBalance(ctx, queryExecutor(r.DB), loan)
		}
		if kind == "Category" {
			if value := findValue(household.Categories, func(value *model.Category) bool { return value.ID == id }); value != nil {
				return value, nil
			}
		}
		if kind == "Purchase" || kind == "PurchaseItem" || kind == "PurchaseCorrection" {
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
			switch kind {
			case "Purchase":
				return purchase, nil
			case "PurchaseItem":
				if value := findValue(purchase.Items, func(value *model.PurchaseItem) bool { return value.ID == id }); value != nil {
					return value, nil
				}
			case "PurchaseCorrection":
				if value := findValue(purchase.Corrections, func(value *model.PurchaseCorrection) bool { return value.ID == id }); value != nil {
					return value, nil
				}
			}
		}
		if kind == "AllocationRule" {
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
		if kind == "ProductConcept" || kind == "ProductVariant" || kind == "ProductAlias" {
			var value model.Node
			switch kind {
			case "ProductConcept":
				value, err = loadProductConcept(ctx, bobpgx.NewPool(r.DB), id)
			case "ProductVariant":
				value, err = loadProductVariant(ctx, bobpgx.NewPool(r.DB), id)
			case "ProductAlias":
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
		if kind == "HouseholdInvitation" {
			invitations, err := r.householdInvitations(ctx, household.ID)
			if err != nil {
				return nil, err
			}
			if value := findValue(invitations, func(value *model.HouseholdInvitation) bool { return value.ID == id }); value != nil {
				return value, nil
			}
		}
		if kind == "Tag" {
			values, err := r.tags(ctx, household.ID, true)
			if err != nil {
				return nil, err
			}
			if value := findValue(values, func(value *model.Tag) bool { return value.ID == id }); value != nil {
				return value, nil
			}
		}
		if kind == "RecurringExpense" {
			values, err := r.recurringExpenses(ctx, household.ID, true)
			if err != nil {
				return nil, err
			}
			if value := findValue(values, func(value *model.RecurringExpense) bool { return value.ID == id }); value != nil {
				return value, nil
			}
		}
		if kind == "AccountMovement" {
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
		if kind == "AccountIncome" {
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
		if kind == "Budget" {
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
				return edge.Node.DocumentID == documentID && edge.Node.SourceReference == sourceReference
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
