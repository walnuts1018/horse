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
}

type CorrectionResult struct {
	PurchaseID string
}
