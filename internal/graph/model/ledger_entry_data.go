package model

type LedgerEntryData struct {
	ID               string
	HouseholdID      string
	Kind             string
	OccurredAt       string
	Description      string
	AmountYen        Long
	Currency         string
	FromMemberID     *string
	ToMemberID       *string
	PoolID           *string
	SourcePurchaseID *string
	TransferID       *string
	LoanID           *string
	CreatedAt        string
}
