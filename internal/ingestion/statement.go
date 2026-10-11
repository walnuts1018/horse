package ingestion

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

type StatementEntry struct {
	Row           int    `json:"row"`
	Kind          string `json:"kind"`
	OccurredAt    string `json:"occurredAt"`
	Merchant      string `json:"merchant"`
	Amount        int64  `json:"amount"`
	TransactionID string `json:"transactionId"`
	OrderID       string `json:"orderId"`
	CardLast4     string `json:"cardLast4"`
}

// ParseStatementCSVはカード利用と銀行資金移動を区別し、複数行を独立した観測として保持する。
func ParseStatementCSV(data []byte, sourceKind string) ([]StatementEntry, error) {
	if len(data) == 0 || len(data) > MaxDocumentBytes || (sourceKind != "card" && sourceKind != "bank") {
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
	for _, key := range []string{"date", "merchant", "amount"} {
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
		amount, err := statementAmount(value(row, "amount"))
		if err != nil {
			return nil, fmt.Errorf("invalid amount at row %d", rowNumber)
		}
		merchant := value(row, "merchant")
		if merchant == "" || len(merchant) > 1024 {
			return nil, fmt.Errorf("invalid merchant at row %d", rowNumber)
		}
		kind := "CARD_PURCHASE"
		if sourceKind == "bank" {
			kind = "BANK_MOVEMENT"
		}
		result = append(result, StatementEntry{Row: rowNumber, Kind: kind, OccurredAt: date.Format(time.RFC3339), Merchant: merchant, Amount: amount, TransactionID: value(row, "transactionId"), OrderID: value(row, "orderId"), CardLast4: value(row, "cardLast4")})
	}
	if len(result) == 0 {
		return nil, errors.New("statement has no transactions")
	}
	return result, nil
}

func headerKey(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "date", "日時", "日付", "利用日", "ご利用日", "取引日":
		return "date"
	case "merchant", "店名", "店舗", "利用店名", "ご利用店名", "摘要":
		return "merchant"
	case "amount", "金額", "利用金額", "ご利用金額", "取引金額":
		return "amount"
	case "transactionid", "transaction_id", "取引id", "取引番号":
		return "transactionId"
	case "orderid", "order_id", "注文番号":
		return "orderId"
	case "cardlast4", "card_last4", "カード末尾":
		return "cardLast4"
	default:
		return ""
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
