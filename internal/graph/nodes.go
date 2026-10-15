package graph

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
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
	if isBatchLoadedNodeKind(kind) {
		nodes, err := r.nodes(ctx, []string{id})
		if err != nil {
			return nil, err
		}
		return nodes[0], nil
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

	hasBatchLoadedNode := false
	hasUnbatchedNode := false
	hasLoadedHouseholdNode := false
	hasUnmatchedObservationNode := false
	for _, nodeID := range decoded {
		if nodeID.kind == nodeTypeUnmatchedObservation {
			hasUnmatchedObservationNode = true
			continue
		}
		if nodeID.kind == nodeTypeSettlement {
			continue
		}
		if isBatchLoadedNodeKind(nodeID.kind) {
			hasBatchLoadedNode = true
			if requiresLoadedHouseholdNodeData(nodeID.kind) {
				hasLoadedHouseholdNode = true
			}
		} else {
			hasUnbatchedNode = true
		}
	}

	if hasBatchLoadedNode || hasUnmatchedObservationNode || hasSettlementNode(decoded) {
		var err error
		if hasBatchLoadedNode && hasLoadedHouseholdNode {
			households, err = r.households(ctx)
		} else {
			households, err = r.householdReferences(ctx)
		}
		if err != nil {
			return nil, err
		}
	}
	if hasBatchLoadedNode {
		resolved, err := r.loadBatchNodesByDomain(ctx, households, idsByKind, indexesByKind)
		if err != nil {
			return nil, err
		}
		for index, node := range resolved {
			result[index] = node
		}
	}
	if hasUnbatchedNode {
		for index, nodeID := range decoded {
			if isBatchLoadedNodeKind(nodeID.kind) || nodeID.kind == nodeTypeUnmatchedObservation || nodeID.kind == nodeTypeSettlement {
				continue
			}
			node, err := r.findNode(ctx, nodeID.kind, nodeID.rawID)
			if err != nil {
				return nil, err
			}
			result[index] = node
		}
	}
	if hasUnmatchedObservationNode {
		resolved, err := r.loadUnmatchedObservationNodes(ctx, ids, households)
		if err != nil {
			return nil, err
		}
		for index, node := range resolved {
			result[index] = node
		}
	}
	if hasSettlementNode(decoded) {
		resolved, err := r.loadSettlementNodes(ctx, decoded, households)
		if err != nil {
			return nil, err
		}
		for index, node := range resolved {
			if decoded[index].kind == nodeTypeSettlement {
				result[index] = node
			}
		}
	}
	return result, nil
}

func hasSettlementNode(nodes []decodedNodeID) bool {
	for _, node := range nodes {
		if node.kind == nodeTypeSettlement {
			return true
		}
	}
	return false
}

func requiresLoadedHouseholdNodeData(kind string) bool {
	return kind == nodeTypeLedgerEntry
}

func (r *queryResolver) householdReferences(ctx context.Context) ([]*model.Household, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	memberID, err := uuid.FromString(user.MemberID)
	if err != nil {
		return nil, fmt.Errorf("parse member id: %w", err)
	}
	memberships, err := models.HouseholdMembers.Query(
		sm.Where(psql.And(
			models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberID)),
			models.HouseholdMembers.Columns.LeftAt.IsNull(),
		)),
		sm.OrderBy(models.HouseholdMembers.Columns.JoinedAt),
	).All(ctx, queryExecutor(r.DB))
	if err != nil {
		return nil, err
	}
	households := make([]*model.Household, 0, len(memberships))
	for _, membership := range memberships {
		households = append(households, &model.Household{ID: membership.HouseholdID.String()})
	}
	return households, nil
}

func isBatchLoadedNodeKind(kind string) bool {
	switch kind {
	case nodeTypePurchase, nodeTypePurchaseItem, nodeTypePurchaseCorrection, nodeTypeFundingSchedule, nodeTypeAccountMovement, nodeTypeAccountIncome, nodeTypeAccountOpeningBalance, nodeTypeHouseholdInvitation, nodeTypeTag, nodeTypeRecurringExpense, nodeTypeLoanBalance, nodeTypeAllocationRule, nodeTypeProductConcept, nodeTypeProductVariant, nodeTypeProductAlias, nodeTypeBudget, nodeTypeLedgerEntry, nodeTypeMember, nodeTypeAccount, nodeTypeCategory, nodeTypeHousehold, nodeTypePool:
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
	if !ok {
		return "", "", errors.New("invalid UnmatchedObservation node ID payload")
	}
	parsedDocumentID := ""
	if len(documentID) == 16 {
		parsedDocumentID = uuid.UUID(documentID).String()
	} else if len(documentID) == 36 {
		parsedID, err := uuid.FromString(string(documentID))
		if err != nil {
			return "", "", errors.New("invalid UnmatchedObservation node ID payload")
		}
		parsedDocumentID = parsedID.String()
	} else {
		return "", "", errors.New("invalid UnmatchedObservation node ID payload")
	}
	if len(rest) >= 8 && binary.BigEndian.Uint64(rest[:8]) == uint64(len(rest[8:])) {
		return parsedDocumentID, string(rest[8:]), nil
	}
	if len(rest) >= 4 && uint64(binary.BigEndian.Uint32(rest[:4])) == uint64(len(rest[4:])) {
		return parsedDocumentID, string(rest[4:]), nil
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
	households, err := r.householdReferences(ctx)
	if err != nil {
		return nil, err
	}
	householdIDs := make([]bob.Expression, 0, len(households))
	for _, household := range households {
		householdIDs = append(householdIDs, psql.Arg(mustUUID(household.ID)))
	}
	if len(householdIDs) == 0 {
		return nil, nil
	}
	executor := queryExecutor(r.DB)
	parsedID, err := uuid.FromString(id)
	if err != nil {
		return nil, nil
	}
	switch kind {
	case nodeTypeHousehold:
		for _, household := range households {
			if household.ID == id {
				rows, err := loadHouseholdSummaries(ctx, r.DB, []uuid.UUID{mustUUID(id)})
				if err != nil {
					return nil, err
				}
				return rows[mustUUID(id)], nil
			}
		}
	case nodeTypeAccount:
		row, err := models.Accounts.Query(sm.Where(psql.And(
			models.Accounts.Columns.ID.EQ(psql.Arg(parsedID)),
			models.Accounts.Columns.HouseholdID.In(householdIDs...),
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return graphAccount(row), nil
	case nodeTypeCategory:
		row, err := models.Categories.Query(sm.Where(psql.And(
			models.Categories.Columns.ID.EQ(psql.Arg(parsedID)),
			models.Categories.Columns.HouseholdID.In(householdIDs...),
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		category := &model.Category{ID: row.ID.String(), Name: row.Name, Archived: row.Archived}
		if row.ParentID.Valid {
			parentID := row.ParentID.V.String()
			category.ParentID = &parentID
		}
		return category, nil
	case nodeTypeMember:
		memberships, err := models.HouseholdMembers.Query(sm.Where(psql.And(
			models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(parsedID)),
			models.HouseholdMembers.Columns.HouseholdID.In(householdIDs...),
		)), sm.OrderBy(models.HouseholdMembers.Columns.JoinedAt)).All(ctx, executor)
		if err != nil {
			return nil, err
		}
		if len(memberships) == 0 {
			return nil, nil
		}
		membership := memberships[0]
		if err := membership.LoadMember(ctx, executor); err != nil {
			return nil, err
		}
		return &model.Member{ID: membership.MemberID.String(), DisplayName: membership.R.Member.DisplayName, Role: model.MemberRole(membership.Role)}, nil
	case nodeTypePool:
		row, err := models.Pools.Query(sm.Where(psql.And(
			models.Pools.Columns.ID.EQ(psql.Arg(parsedID)),
			models.Pools.Columns.HouseholdID.In(householdIDs...),
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		household, err := loadHousehold(ctx, r.DB, row.HouseholdID.String(), householdFields{pools: true})
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return findValue(household.PoolVariants, func(value model.PoolVariant) bool { return value.GetID() == id }), nil
	case nodeTypeAccountOpeningBalance:
		account, err := models.Accounts.Query(sm.Where(psql.And(
			models.Accounts.Columns.ID.EQ(psql.Arg(parsedID)),
			models.Accounts.Columns.HouseholdID.In(householdIDs...),
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		journal, err := models.Journals.Query(sm.Where(psql.And(
			models.Journals.Columns.HouseholdID.EQ(psql.Arg(account.HouseholdID)),
			models.Journals.Columns.SourceKind.EQ(psql.Arg(sourceKindAccountOpeningBalance)),
			models.Journals.Columns.SourceID.EQ(psql.Arg(parsedID)),
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		globalID, err := encodeNodeID(nodeTypeAccountOpeningBalance, journal.SourceID.String())
		if err != nil {
			return nil, err
		}
		return &model.AccountOpeningBalance{ID: globalID, Account: graphAccount(account)}, nil
	default:
		return nil, nil
	}
	return nil, nil
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
	households, err := r.householdReferences(ctx)
	if err != nil {
		return nil, err
	}
	documentUUID, err := uuid.FromString(documentID)
	if err != nil {
		return nil, nil
	}
	householdIDs := make([]bob.Expression, 0, len(households))
	for _, household := range households {
		householdIDs = append(householdIDs, psql.Arg(mustUUID(household.ID)))
	}
	if len(householdIDs) == 0 {
		return nil, nil
	}
	executor := queryExecutor(r.DB)
	observation, err := models.SourceObservations.Query(sm.Where(psql.And(
		models.SourceObservations.Columns.SourceDocumentID.EQ(psql.Arg(documentUUID)),
		models.SourceObservations.Columns.SourceReference.EQ(psql.Arg(sourceReference)),
		models.SourceObservations.Columns.HouseholdID.In(householdIDs...),
	))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	kind := model.SourceKind(strings.ToUpper(observation.SourceKind))
	fields := &model.ObservationFields{
		DocumentID: observation.SourceDocumentID.String(), SourceReference: observation.SourceReference, SourceKind: kind,
		Merchant: observation.Merchant, AmountYen: graphLong(observation.AmountYen),
		OrderID: observation.OrderID, TransactionID: observation.TransactionID,
	}
	if observation.OccurredAt.Valid {
		occurredAt := timeString(observation.OccurredAt.V)
		fields.OccurredAt = &occurredAt
	}
	confirmation, err := models.DocumentConfirmations.Query(sm.Where(psql.And(
		models.DocumentConfirmations.Columns.HouseholdID.EQ(psql.Arg(observation.HouseholdID)),
		models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(observation.SourceDocumentID)),
		models.DocumentConfirmations.Columns.SourceReference.EQ(psql.Arg(sourceReference)),
	))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return &model.UnmatchedObservation{ObservationFields: fields}, nil
	}
	if err != nil {
		return nil, err
	}
	purchaseID := ""
	if confirmation.PurchaseID.Valid {
		purchaseID = confirmation.PurchaseID.V.String()
	} else if confirmation.CorrectionID.Valid {
		correction, err := models.Corrections.Query(sm.Where(psql.And(
			models.Corrections.Columns.ID.EQ(psql.Arg(confirmation.CorrectionID.V)),
			models.Corrections.Columns.HouseholdID.EQ(psql.Arg(observation.HouseholdID)),
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		purchaseID = correction.PurchaseID.String()
	} else {
		return &model.UnmatchedObservation{ObservationFields: fields}, nil
	}
	return &model.ConfirmedObservation{ObservationFields: fields, PurchaseID: purchaseID}, nil
}
