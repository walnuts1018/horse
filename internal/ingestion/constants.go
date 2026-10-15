package ingestion

const (
	mimeTypePlainText = "text/plain"
	mimeTypePNG       = "image/png"
	mimeTypeJPEG      = "image/jpeg"
	mimeTypePDF       = "application/pdf"
	mimeTypeEmail     = "message/rfc822"
	mimeTypeCSV       = "text/csv"

	extractionStatusQueued              = "queued"
	extractionStatusFailed              = "failed"
	extractionStatusSucceeded           = "succeeded"
	geminiFailureSpendCap               = "spend_cap_reached"
	geminiFailureDeveloperAPIRestricted = "developer_api_consumer_use_restricted"

	geminiReasonInitialExtraction = "initial_extraction"
	geminiServiceTierStandard     = "standard"
	geminiServiceTierFlex         = "flex"
	geminiThinkingLevelLow        = "low"
	geminiThinkingLevelLOW        = "LOW"

	schemaTypeKey     = "type"
	schemaTypeInteger = "integer"
	schemaTypeNumber  = "number"
	schemaMaximumKey  = "maximum"
	schemaMinimumKey  = "minimum"
	schemaItemsKey    = "items"
	schemaAmountKey   = "amount"
	schemaMerchantKey = "merchant"

	priceBasisTaxIncluded = "TAX_INCLUDED"
	priceBasisTaxExcluded = "TAX_EXCLUDED"
	priceBasisUnknown     = "UNKNOWN"
	taxRoundingFloor      = "FLOOR"
	currencyJPY           = "JPY"

	statementSourceBank       = "bank"
	statementSourceCard       = "card"
	paymentMethodCard         = "card"
	statementDateField        = "date"
	statementKindReceipt      = "RECEIPT_PURCHASE"
	statementKindPayment      = "PURCHASE_PAYMENT"
	statementKindCardPurchase = "CARD_PURCHASE"
	statementMatchPossible    = "POSSIBLE"
)
