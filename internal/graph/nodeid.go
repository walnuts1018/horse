package graph

import (
	"bytes"
	"encoding/base64"
	"fmt"

	"github.com/gofrs/uuid/v5"
)

var nodeTypes = map[string]struct{}{
	"Account": {}, "AccountIncome": {}, "AccountMovement": {}, "AllocationRule": {},
	"AccountOpeningBalance": {}, "Budget": {}, "Category": {}, "FundingSchedule": {}, "Household": {}, "HouseholdInvitation": {}, "LoanBalance": {}, "Member": {},
	"LedgerEntry": {}, "Pool": {}, "ProductAlias": {}, "ProductConcept": {}, "ProductVariant": {}, "Purchase": {},
	"PurchaseCorrection": {}, "PurchaseItem": {}, "RecurringExpense": {}, "Settlement": {}, "Tag": {}, "UnmatchedObservation": {},
}

func encodeNodeID(kind, raw string) (string, error) {
	if kind == "Settlement" {
		return "", fmt.Errorf("Settlement node ID requires household and period")
	}
	if _, ok := nodeTypes[kind]; !ok {
		return "", fmt.Errorf("unknown node type %q", kind)
	}
	id, err := uuid.FromString(raw)
	if err != nil || id == uuid.Nil {
		return "", fmt.Errorf("invalid %s node ID", kind)
	}
	return base64.RawURLEncoding.EncodeToString(append(append([]byte(kind), ':'), id[:]...)), nil
}

func encodeSettlementNodeID(householdID, period string) (string, error) {
	if _, err := uuid.FromString(householdID); err != nil {
		return "", fmt.Errorf("invalid Settlement household ID")
	}
	if _, err := parseAccountingPeriod(period); err != nil {
		return "", fmt.Errorf("invalid Settlement period")
	}
	payload := "Settlement:" + householdID + ":" + period
	return base64.RawURLEncoding.EncodeToString([]byte(payload)), nil
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
	decoded, err := base64.RawURLEncoding.DecodeString(globalID)
	if err != nil {
		return "", fmt.Errorf("invalid node ID: %w", err)
	}
	kindBytes, raw, ok := bytes.Cut(decoded, []byte{':'})
	kind := string(kindBytes)
	if !ok {
		return "", fmt.Errorf("invalid node ID payload")
	}
	if _, ok := nodeTypes[kind]; !ok {
		return "", fmt.Errorf("unknown node type %q", kind)
	}
	if kind != expected {
		return "", fmt.Errorf("expected %s node ID, got %s", expected, kind)
	}
	if len(raw) != 16 {
		return "", fmt.Errorf("invalid %s node ID payload", expected)
	}
	id := uuid.UUID(raw)
	if id == uuid.Nil {
		return "", fmt.Errorf("invalid %s node ID", expected)
	}
	return id.String(), nil
}
