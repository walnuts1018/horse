package model

type ObservationFields struct {
	DocumentID      string     `json:"documentId"`
	SourceReference string     `json:"sourceReference"`
	SourceKind      SourceKind `json:"sourceKind"`
	Merchant        string     `json:"merchant"`
	OccurredAt      *DateTime  `json:"occurredAt,omitempty"`
	AmountYen       Long       `json:"amountYen"`
	OrderID         string     `json:"orderId"`
	TransactionID   string     `json:"transactionId"`
}

type ObservationVariant interface {
	IsNode()
	IsObservation()
	GetDocumentID() string
	GetSourceReference() string
	GetSourceKind() SourceKind
	GetMerchant() string
	GetOccurredAt() *DateTime
	GetAmountYen() Long
	GetOrderID() string
	GetTransactionID() string
}

type UnmatchedObservation struct {
	*ObservationFields
}

func (UnmatchedObservation) IsNode()                            {}
func (UnmatchedObservation) IsObservation()                     {}
func (observation *UnmatchedObservation) GetDocumentID() string { return observation.DocumentID }
func (observation *UnmatchedObservation) GetSourceReference() string {
	return observation.SourceReference
}
func (observation *UnmatchedObservation) GetSourceKind() SourceKind { return observation.SourceKind }
func (observation *UnmatchedObservation) GetMerchant() string       { return observation.Merchant }
func (observation *UnmatchedObservation) GetOccurredAt() *DateTime  { return observation.OccurredAt }
func (observation *UnmatchedObservation) GetAmountYen() Long        { return observation.AmountYen }
func (observation *UnmatchedObservation) GetOrderID() string        { return observation.OrderID }
func (observation *UnmatchedObservation) GetTransactionID() string {
	return observation.TransactionID
}

type ConfirmedObservation struct {
	*ObservationFields
	PurchaseID string `json:"purchaseId"`
}

func (ConfirmedObservation) IsNode()                            {}
func (ConfirmedObservation) IsObservation()                     {}
func (observation *ConfirmedObservation) GetDocumentID() string { return observation.DocumentID }
func (observation *ConfirmedObservation) GetSourceReference() string {
	return observation.SourceReference
}
func (observation *ConfirmedObservation) GetSourceKind() SourceKind { return observation.SourceKind }
func (observation *ConfirmedObservation) GetMerchant() string       { return observation.Merchant }
func (observation *ConfirmedObservation) GetOccurredAt() *DateTime  { return observation.OccurredAt }
func (observation *ConfirmedObservation) GetAmountYen() Long        { return observation.AmountYen }
func (observation *ConfirmedObservation) GetOrderID() string        { return observation.OrderID }
func (observation *ConfirmedObservation) GetTransactionID() string {
	return observation.TransactionID
}
