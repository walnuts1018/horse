package purchase

import "time"

type CreateCommand struct {
	ActorID                    string
	HouseholdID                string
	OccurredAt                 time.Time
	IdempotencyKey             string
	SourceDocumentID           *string
	SourceReference            *string
	ReceiptCandidateID         *string
	ManualAmountOverrideReason *string
	Origin                     string
	Merchant                   string
	Items                      []Item
	PaymentAllocations         []Payment
}

type Item struct {
	Name             string
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
	IdempotencyKey   string
}

type PostResult struct {
	PurchaseID string
}
