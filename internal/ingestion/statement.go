package ingestion

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
)

type StatementEntry struct {
	Row             int     `json:"row"`
	SourceReference string  `json:"sourceReference,omitempty"`
	Kind            string  `json:"kind"`
	OccurredAt      string  `json:"occurredAt"`
	Merchant        string  `json:"merchant"`
	Amount          int64   `json:"amount"`
	TransactionID   string  `json:"transactionId"`
	OrderID         string  `json:"orderId"`
	CardLast4       string  `json:"cardLast4"`
	Region          *Region `json:"region,omitempty"`
}

func VerifyStatements(sourceKind string, entries []StatementEntry, warnings []string) (Verification, error) {
	if sourceKind != statementSourceCard && sourceKind != statementSourceBank || len(entries) == 0 || len(entries) > 1000 || len(warnings) > 100 {
		return Verification{}, errors.New("invalid statement extraction")
	}
	warnings = slices.Clone(warnings)
	for _, warning := range warnings {
		if strings.TrimSpace(warning) == "" || len(warning) > 1024 {
			return Verification{}, errors.New("invalid statement warning")
		}
	}
	verified := make([]StatementEntry, len(entries))
	copy(verified, entries)
	for i := range verified {
		entry := &verified[i]
		occurredAt, err := time.Parse(time.RFC3339, entry.OccurredAt)
		if err != nil || entry.Merchant == "" || len(entry.Merchant) > 1024 || strings.TrimSpace(entry.Merchant) != entry.Merchant || !validAmount(entry.Amount) || entry.Region == nil || !validRegion(*entry.Region) || entry.Region.Page >= maxOCRPDFPages || entry.TransactionID != "" && len(entry.TransactionID) > 256 || entry.OrderID != "" && len(entry.OrderID) > 256 || entry.CardLast4 != "" && (len(entry.CardLast4) != 4 || !allASCIIDigits(entry.CardLast4)) {
			return Verification{}, fmt.Errorf("invalid statement transaction at index %d", i)
		}
		for previous := range verified[:i] {
			if entry.Region.Page == verified[previous].Region.Page && overlap(*entry.Region, *verified[previous].Region) > 0.6 {
				return Verification{}, fmt.Errorf("overlapping statement transaction regions at index %d", i)
			}
		}
		if sourceKind == statementSourceCard {
			if entry.Kind != statementKindCardPurchase && entry.Kind != "REFUND" {
				return Verification{}, fmt.Errorf("invalid card transaction kind at index %d", i)
			}
		} else if !validBankStatementKind(entry.Kind) {
			return Verification{}, fmt.Errorf("invalid bank transaction kind at index %d", i)
		} else if entry.Kind == "UNCLASSIFIED" {
			warnings = append(warnings, fmt.Sprintf("取引%dの種別が未確定です。", i+1))
		}
		entry.OccurredAt = occurredAt.Format(time.RFC3339)
		entry.Row = i + 1
		entry.SourceReference = statementSourceReference(i + 1)
	}
	issues := append([]string(nil), warnings...)
	return Verification{Receipt: Receipt{Statements: verified, Warnings: slices.Clone(warnings)}, Statements: verified, Issues: issues, Balanced: len(issues) == 0}, nil
}

func validBankStatementKind(kind string) bool {
	switch kind {
	case statementKindPayment, "CARD_BILL_PAYMENT", "ACCOUNT_TRANSFER", "REFUND", "UNCLASSIFIED":
		return true
	default:
		return false
	}
}

func allASCIIDigits(value string) bool {
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

// ParseStatementCSVはカード利用と銀行資金移動を区別し、複数行を独立した観測として保持する。
func ParseStatementCSV(data []byte, sourceKind string) ([]StatementEntry, error) {
	if len(data) == 0 || len(data) > MaxDocumentBytes || (sourceKind != statementSourceCard && sourceKind != statementSourceBank) {
		return nil, errors.New("invalid statement source")
	}
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), "\ufeff")))
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read statement header: %w", err)
	}
	columns := map[string]int{}
	for i, column := range header {
		key := headerKey(column)
		if key != "" {
			if _, exists := columns[key]; exists {
				return nil, errors.New("duplicate statement column")
			}
			columns[key] = i
		}
	}
	for _, key := range []string{statementDateField, schemaMerchantKey, schemaAmountKey} {
		if _, exists := columns[key]; !exists {
			return nil, fmt.Errorf("missing statement column: %s", key)
		}
	}
	value := func(row []string, key string) string {
		if i, exists := columns[key]; exists {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
	var result []StatementEntry
	for rowNumber := 2; ; rowNumber++ {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read statement row %d: %w", rowNumber, err)
		}
		if rowNumber > 10_001 {
			return nil, errors.New("too many statement rows")
		}
		date, err := statementDate(value(row, "date"))
		if err != nil {
			return nil, fmt.Errorf("invalid date at row %d", rowNumber)
		}
		amount, err := statementAmount(value(row, schemaAmountKey))
		if err != nil {
			return nil, fmt.Errorf("invalid amount at row %d", rowNumber)
		}
		merchant := value(row, schemaMerchantKey)
		if merchant == "" || len(merchant) > 1024 {
			return nil, fmt.Errorf("invalid merchant at row %d", rowNumber)
		}
		kind := statementKindCardPurchase
		if sourceKind == statementSourceBank {
			kind = bankTransactionKind(value(row, "transactionKind"))
		}
		result = append(result, StatementEntry{Row: rowNumber, SourceReference: statementSourceReference(rowNumber), Kind: kind, OccurredAt: date.Format(time.RFC3339), Merchant: merchant, Amount: amount, TransactionID: value(row, "transactionId"), OrderID: value(row, "orderId"), CardLast4: value(row, "cardLast4")})
	}
	if len(result) == 0 {
		return nil, errors.New("statement has no transactions")
	}
	return result, nil
}

func statementSourceReference(row int) string {
	return "row:" + strconv.Itoa(row)
}

func headerKey(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "date", "日時", "日付", "利用日", "ご利用日", "取引日":
		return "date"
	case schemaMerchantKey, "店名", "店舗", "利用店名", "ご利用店名", "摘要":
		return schemaMerchantKey
	case schemaAmountKey, "金額", "利用金額", "ご利用金額", "取引金額":
		return schemaAmountKey
	case "transactionid", "transaction_id", "取引id", "取引番号":
		return "transactionId"
	case "orderid", "order_id", "注文番号":
		return "orderId"
	case "cardlast4", "card_last4", "カード末尾":
		return "cardLast4"
	case "kind", schemaTypeKey, "transactionkind", "transaction_kind", "取引種別", "取引区分", "種別":
		return "transactionKind"
	default:
		return ""
	}
}

func bankTransactionKind(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case statementKindPayment, "商品購入", "デビット購入":
		return statementKindPayment
	case "CARD_BILL_PAYMENT", "CARD_PAYMENT", "クレジットカード引落", "カード利用代金":
		return "CARD_BILL_PAYMENT"
	case "ACCOUNT_TRANSFER", "TRANSFER", "振込", "振替":
		return "ACCOUNT_TRANSFER"
	case "REFUND", "返金", "払い戻し":
		return "REFUND"
	default:
		return "UNCLASSIFIED"
	}
}

func statementDate(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006/01/02 15:04", "2006-01-02", "2006/01/02", "2006/1/2", "2006年1月2日"} {
		parsed, err := time.ParseInLocation(layout, value, time.FixedZone("JST", 9*60*60))
		if err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, errors.New("invalid statement date")
}

func statementAmount(value string) (int64, error) {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, ",", "")
	value = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(value, "¥"), "￥"), "円"))
	if strings.HasPrefix(value, "(") && strings.HasSuffix(value, ")") {
		value = "-" + strings.TrimSuffix(strings.TrimPrefix(value, "("), ")")
	}
	amount, err := strconv.ParseInt(value, 10, 64)
	if err != nil || !validAmount(amount) {
		return 0, errors.New("invalid statement amount")
	}
	return amount, nil
}
