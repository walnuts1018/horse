package purchase

import (
	"time"
)

type CreateCommand struct {
	ActorID                    string
	HouseholdID                string
	OccurredAt                 time.Time
	IdempotencyKey             string `validate:"required,max=200"`
	SourceDocumentID           *string
	SourceReference            *string `validate:"omitempty,max=256"`
	ReceiptCandidateID         *string
	ManualAmountOverrideReason *string
	Origin                     string
	Merchant                   string `validate:"max=240"`
	Items                      []Item `validate:"min=1,max=200,dive"`
	PaymentAllocations         []Payment
}

type Item struct {
	Name             string `validate:"notblank,max=240"`
	AmountYen        int64
	CategoryID       *string
	ProductVariantID *string
	Allocations      []Allocation
	Quantity         *Measure
	PrintedUnitPrice *PrintedUnitPrice
}

type Allocation struct {
	PoolID string
	Weight int64
}

type Payment struct {
	AccountID string
	AmountYen int64
}

type Measure struct {
	Numerator   string
	Denominator string
	Unit        string
}

type PrintedUnitPrice struct {
	AmountYen int64
	Basis     Measure
}

type CreateResult struct {
	PurchaseID string
}

type PostCommand struct {
	ActorID          string
	PurchaseID       string
	ExpectedRevision int64
	IdempotencyKey   string `validate:"required,max=200"`
}

type PostResult struct {
	PurchaseID string
}

type UpdateCommand struct {
	ActorID            string
	PurchaseID         string
	BaseRevision       int64
	OccurredAt         *time.Time
	Merchant           *string `validate:"omitempty,max=240"`
	PaymentAllocations *[]Payment
	ChangedFields      []string
}

type UpdateResult struct {
	PurchaseID string
}

type UpdateItemCommand struct {
	ActorID             string
	ItemID              string
	BaseRevision        int64
	ChangedFields       []string
	Name                *string `validate:"omitempty,notblank,max=240"`
	AmountYen           *int64
	QuantitySet         bool
	Quantity            *Measure
	PrintedUnitPriceSet bool
	PrintedUnitPrice    *PrintedUnitPrice
	CategoryIDSet       bool
	CategoryID          *string
	ProductVariantIDSet bool
	ProductVariantID    *string
	ProductLearningSet  bool
	ProductLearning     *ProductLearning
	AllocationsSet      bool
	Allocations         []Allocation
}

type ProductLearning struct {
	ItemOnly      *bool
	VariantName   *string
	MerchantAlias *bool
}
