package graph

import (
	"context"

	gofrsuuid "github.com/gofrs/uuid/v5"
	settlementapp "github.com/walnuts1018/horse/internal/application/settlement"
	"github.com/walnuts1018/horse/internal/graph/model"
)

type settlementNodeKey struct {
	householdID string
	period      string
}

func (r *queryResolver) loadSettlementNodes(ctx context.Context, nodeIDs []decodedNodeID, households []*model.Household) ([]model.Node, error) {
	result := make([]model.Node, len(nodeIDs))
	keys := make([]settlementNodeKey, len(nodeIDs))
	uniqueKeys := make(map[settlementNodeKey]struct{})
	householdUUIDs := make(map[gofrsuuid.UUID]struct{})
	for index, nodeID := range nodeIDs {
		if nodeID.kind != nodeTypeSettlement {
			continue
		}
		householdID, period, err := decodeSettlementNodeID(nodeID.rawID)
		if err != nil {
			return nil, err
		}
		householdUUID, _ := gofrsuuid.FromString(householdID)
		keys[index] = settlementNodeKey{householdID: householdID, period: period}
		uniqueKeys[keys[index]] = struct{}{}
		householdUUIDs[householdUUID] = struct{}{}
	}
	if len(uniqueKeys) == 0 {
		return result, nil
	}
	accessible := make(map[gofrsuuid.UUID]struct{}, len(households))
	for _, household := range households {
		accessible[mustUUID(household.ID)] = struct{}{}
	}
	for householdID := range householdUUIDs {
		if _, ok := accessible[householdID]; !ok {
			return nil, ErrNotFound
		}
	}
	requests := make([]settlementapp.GetRequest, 0, len(uniqueKeys))
	orderedKeys := make([]settlementNodeKey, 0, len(uniqueKeys))
	for key := range uniqueKeys {
		requests = append(requests, settlementapp.GetRequest{HouseholdID: key.householdID, Period: key.period})
		orderedKeys = append(orderedKeys, key)
	}
	values, err := settlementapp.New(r.DB).GetMany(ctx, requests)
	if err != nil {
		return nil, mapSettlementError(err)
	}
	settlements := make(map[settlementNodeKey]model.Node, len(values))
	for index, value := range values {
		node, err := settlementModel(value)
		if err != nil {
			return nil, err
		}
		settlements[orderedKeys[index]] = node
	}
	for index, nodeID := range nodeIDs {
		if nodeID.kind == nodeTypeSettlement {
			result[index] = settlements[keys[index]]
		}
	}
	return result, nil
}
