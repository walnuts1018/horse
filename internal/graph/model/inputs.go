package model

type AllocationInput struct {
	PoolID string `json:"poolId"`
	Weight Long   `json:"weight"`
}

type PurchasePaymentInput struct {
	PaymentAccountID string `json:"paymentAccountId"`
	AmountYen        Long   `json:"amountYen"`
}

type PurchaseQuantityInput struct {
	Numerator   string `json:"numerator"`
	Denominator string `json:"denominator"`
	Unit        string `json:"unit"`
}

type PrintedUnitPriceInput struct {
	AmountYen Long                   `json:"amountYen"`
	Basis     *PurchaseQuantityInput `json:"basis"`
}
