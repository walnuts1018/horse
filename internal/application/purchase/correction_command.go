package purchase

type CorrectionCommand struct {
	ActorID            string
	HouseholdID        string
	PurchaseID         string
	ItemID             *string
	AmountDeltaYen     int64
	Reason             string
	IdempotencyKey     string
	Allocations        []Allocation
	PaymentAllocations []Payment
	SourceDocumentID   string
	SourceReference    string
}

type CorrectionResult struct {
	PurchaseID string
}
