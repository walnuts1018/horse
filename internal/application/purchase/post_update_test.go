package purchase

import (
	"context"
	"errors"
	"testing"
)

func TestPostAndUpdateRejectMalformedUUIDsAsBadInput(t *testing.T) {
	t.Parallel()

	const validUUID = "018f47d4-9e1a-7cc0-85b8-4f98c3429472"
	const invalidUUID = "not-a-uuid"
	merchant := "shop"
	tests := []struct {
		name string
		run  func(context.Context, *Service) error
	}{
		{
			name: "post purchase id",
			run: func(ctx context.Context, service *Service) error {
				_, err := service.Post(ctx, PostCommand{PurchaseID: invalidUUID, ActorID: validUUID, IdempotencyKey: "key"}, [32]byte{})
				return err
			},
		},
		{
			name: "post actor id",
			run: func(ctx context.Context, service *Service) error {
				_, err := service.Post(ctx, PostCommand{PurchaseID: validUUID, ActorID: invalidUUID, IdempotencyKey: "key"}, [32]byte{})
				return err
			},
		},
		{
			name: "update purchase id",
			run: func(ctx context.Context, service *Service) error {
				_, err := service.Update(ctx, UpdateCommand{PurchaseID: invalidUUID, ActorID: validUUID, ChangedFields: []string{updateFieldMerchant}, Merchant: &merchant})
				return err
			},
		},
		{
			name: "update actor id",
			run: func(ctx context.Context, service *Service) error {
				_, err := service.Update(ctx, UpdateCommand{PurchaseID: validUUID, ActorID: invalidUUID, ChangedFields: []string{updateFieldMerchant}, Merchant: &merchant})
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service := &Service{}
			if err := test.run(t.Context(), service); !errors.Is(err, ErrBadInput) {
				t.Fatalf("error = %v, want ErrBadInput", err)
			}
		})
	}
}
