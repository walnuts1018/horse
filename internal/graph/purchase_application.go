package graph

import (
	"context"

	"github.com/walnuts1018/horse/internal/application/purchase"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func (r *mutationResolver) createPurchase(ctx context.Context, input model.CreatePurchaseInput) (*model.Purchase, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	fingerprint, err := mutationRequestFingerprint(input)
	if err != nil {
		return nil, err
	}
	command, err := createPurchaseCommand(input, user.MemberID)
	if err != nil {
		return nil, err
	}
	result, err := purchase.NewService(r.DB).Create(ctx, command, fingerprint)
	if err != nil {
		return nil, err
	}
	return loadPurchase(ctx, r.DB, result.PurchaseID)
}

func (r *mutationResolver) createAndPostPurchase(ctx context.Context, input model.CreatePurchaseInput) (*model.Purchase, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	fingerprint, err := mutationRequestFingerprint(input)
	if err != nil {
		return nil, err
	}
	command, err := createPurchaseCommand(input, user.MemberID)
	if err != nil {
		return nil, err
	}
	result, err := purchase.NewService(r.DB).CreateAndPost(ctx, command, fingerprint)
	if err != nil {
		return nil, err
	}
	return loadPurchase(ctx, r.DB, result.PurchaseID)
}

func createPurchaseCommand(input model.CreatePurchaseInput, actorID string) (purchase.CreateCommand, error) {
	occurredAt, err := parseTime(input.OccurredAt)
	if err != nil {
		return purchase.CreateCommand{}, err
	}
	command := purchase.CreateCommand{
		ActorID: actorID, HouseholdID: input.HouseholdID, OccurredAt: occurredAt, DatePrecision: "DATE_TIME",
		IdempotencyKey: input.IdempotencyKey, SourceDocumentID: input.SourceDocumentID,
		SourceReference: input.SourceReference, ReceiptCandidateID: input.ReceiptCandidateID,
		ManualAmountOverrideReason: input.ManualAmountOverrideReason, Merchant: input.Merchant,
	}
	if input.DatePrecision != nil {
		command.DatePrecision = string(*input.DatePrecision)
	}
	if input.Origin != nil {
		command.Origin = string(*input.Origin)
	}
	command.Items = make([]purchase.Item, 0, len(input.Items))
	for _, item := range input.Items {
		if item == nil {
			command.Items = append(command.Items, purchase.Item{})
			continue
		}
		converted := purchase.Item{
			Name: item.Name, AmountYen: int64(item.AmountYen), CategoryID: item.CategoryID,
			ProductVariantID: item.ProductVariantID,
		}
		for _, allocation := range item.Allocations {
			if allocation == nil {
				converted.Allocations = append(converted.Allocations, purchase.Allocation{})
				continue
			}
			converted.Allocations = append(converted.Allocations, purchase.Allocation{PoolID: allocation.PoolID, Weight: int64(allocation.Weight)})
		}
		if item.Quantity != nil {
			converted.Quantity = &purchase.Measure{Numerator: item.Quantity.Numerator, Denominator: item.Quantity.Denominator, Unit: item.Quantity.Unit}
		}
		if item.PrintedUnitPrice != nil {
			if item.PrintedUnitPrice.Basis == nil {
				return purchase.CreateCommand{}, ErrBadInput
			}
			converted.PrintedUnitPrice = &purchase.PrintedUnitPrice{
				AmountYen: int64(item.PrintedUnitPrice.AmountYen),
				Basis:     purchase.Measure{Numerator: item.PrintedUnitPrice.Basis.Numerator, Denominator: item.PrintedUnitPrice.Basis.Denominator, Unit: item.PrintedUnitPrice.Basis.Unit},
			}
		}
		command.Items = append(command.Items, converted)
	}
	for _, paymentInput := range input.PaymentAllocations {
		if paymentInput == nil {
			command.PaymentAllocations = append(command.PaymentAllocations, purchase.Payment{})
			continue
		}
		command.PaymentAllocations = append(command.PaymentAllocations, purchase.Payment{AccountID: paymentInput.PaymentAccountID, AmountYen: int64(paymentInput.AmountYen)})
	}
	return command, nil
}

func (r *mutationResolver) postPurchase(ctx context.Context, purchaseID string, expectedRevision int, idempotencyKey string) (*model.Purchase, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	fingerprint, err := mutationRequestFingerprint(struct {
		PurchaseID       string
		ExpectedRevision int
	}{PurchaseID: purchaseID, ExpectedRevision: expectedRevision})
	if err != nil {
		return nil, err
	}
	result, err := purchase.NewService(r.DB).Post(ctx, purchase.PostCommand{
		ActorID: user.MemberID, PurchaseID: purchaseID, ExpectedRevision: int64(expectedRevision), IdempotencyKey: idempotencyKey,
	}, fingerprint)
	if err != nil {
		return nil, err
	}
	return loadPurchase(ctx, r.DB, result.PurchaseID)
}
