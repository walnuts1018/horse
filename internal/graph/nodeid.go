package graph

import (
	"fmt"

	"github.com/gofrs/uuid/v5"
	"github.com/walnuts1018/horse/internal/relayid"
)

var nodeTypes = map[string]struct{}{
	nodeTypeAccount: {}, nodeTypeAccountIncome: {}, nodeTypeAccountMovement: {}, nodeTypeAllocationRule: {},
	nodeTypeAccountOpeningBalance: {}, nodeTypeBudget: {}, nodeTypeCategory: {}, nodeTypeFundingSchedule: {}, nodeTypeHousehold: {}, nodeTypeHouseholdInvitation: {}, nodeTypeLoanBalance: {}, nodeTypeMember: {},
	nodeTypeLedgerEntry: {}, nodeTypePool: {}, nodeTypeProductAlias: {}, nodeTypeProductConcept: {}, nodeTypeProductVariant: {}, nodeTypePurchase: {},
	nodeTypePurchaseCorrection: {}, nodeTypePurchaseItem: {}, nodeTypeRecurringExpense: {}, nodeTypeSettlement: {}, nodeTypeTag: {}, nodeTypeUnmatchedObservation: {},
}

func encodeNodeID(kind, raw string) (string, error) {
	if kind == nodeTypeSettlement {
		return "", fmt.Errorf("Settlement node ID requires household and period")
	}
	if _, ok := nodeTypes[kind]; !ok {
		return "", fmt.Errorf("unknown node type %q", kind)
	}
	id, err := uuid.FromString(raw)
	if err != nil || id == uuid.Nil {
		return "", fmt.Errorf("invalid %s node ID", kind)
	}
	return relayid.Encode(kind, id[:]), nil
}

func encodeSettlementNodeID(householdID, period string) (string, error) {
	if _, err := uuid.FromString(householdID); err != nil {
		return "", fmt.Errorf("invalid Settlement household ID")
	}
	if _, err := parseAccountingPeriod(period); err != nil {
		return "", fmt.Errorf("invalid Settlement period")
	}
	return relayid.Encode(nodeTypeSettlement, []byte(householdID+":"+period)), nil
}

func encodeNodeIDPtr(kind string, raw *string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	id, err := encodeNodeID(kind, *raw)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func decodeNodeID(globalID, expected string) (string, error) {
	if _, ok := nodeTypes[expected]; !ok {
		return "", fmt.Errorf("unknown node type %q", expected)
	}
	id, err := relayid.DecodeUUID(globalID, expected)
	if err != nil {
		return "", err
	}
	return uuid.UUID(id).String(), nil
}
