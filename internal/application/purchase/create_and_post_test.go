package purchase

import (
	"errors"
	"strings"
	"testing"
)

func TestCreateAndPostValidatesCommandBeforeOpeningTransaction(t *testing.T) {
	t.Parallel()

	const (
		validUUID        = "018f47d4-9e1a-7cc0-85b8-4f98c3429472"
		idempotencyKey   = "compound-create-post-idempotency-key"
		purchaseItemName = "compound create post item"
	)
	service := &Service{}
	command := CreateCommand{
		ActorID: validUUID, HouseholdID: validUUID, IdempotencyKey: idempotencyKey,
		Items: []Item{{Name: purchaseItemName}},
	}
	tests := []struct {
		name    string
		command CreateCommand
	}{
		{name: "actor", command: CreateCommand{ActorID: "bad", HouseholdID: validUUID, IdempotencyKey: idempotencyKey, Items: []Item{{Name: purchaseItemName}}}},
		{name: "household", command: CreateCommand{ActorID: validUUID, HouseholdID: "bad", IdempotencyKey: idempotencyKey, Items: []Item{{Name: purchaseItemName}}}},
		{name: "idempotency key", command: CreateCommand{ActorID: validUUID, HouseholdID: validUUID, Items: []Item{{Name: purchaseItemName}}}},
		{name: "long idempotency key", command: CreateCommand{ActorID: validUUID, HouseholdID: validUUID, IdempotencyKey: strings.Repeat("k", 201), Items: []Item{{Name: purchaseItemName}}}},
		{name: "long merchant", command: CreateCommand{ActorID: validUUID, HouseholdID: validUUID, IdempotencyKey: idempotencyKey, Merchant: strings.Repeat("m", 241), Items: []Item{{Name: purchaseItemName}}}},
		{name: "blank item name", command: CreateCommand{ActorID: validUUID, HouseholdID: validUUID, IdempotencyKey: idempotencyKey, Items: []Item{{Name: "  "}}}},
		{name: "long item name", command: CreateCommand{ActorID: validUUID, HouseholdID: validUUID, IdempotencyKey: idempotencyKey, Items: []Item{{Name: strings.Repeat("i", 241)}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := service.CreateAndPost(t.Context(), test.command, [32]byte{})
			if !errors.Is(err, ErrBadInput) {
				t.Fatalf("CreateAndPost() error = %v, want ErrBadInput", err)
			}
		})
	}
	if err := validateCreateCommand(command); err != nil {
		t.Fatalf("valid create command was rejected: %v", err)
	}
}
