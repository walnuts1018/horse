package ingestion

import (
	"strings"
	"time"
)

type Observation struct {
	DocumentID    string    `json:"documentId"`
	Hash          string    `json:"hash"`
	SourceKind    string    `json:"sourceKind"`
	OrderID       string    `json:"orderId"`
	TransactionID string    `json:"transactionId"`
	Merchant      string    `json:"merchant"`
	OccurredAt    time.Time `json:"occurredAt"`
	Amount        int64     `json:"amount"`
	CardLast4     string    `json:"cardLast4"`
}

type Match struct {
	DocumentID string   `json:"documentId"`
	State      string   `json:"state"`
	Reasons    []string `json:"reasons"`
}

// Reconcileは注文と分割決済を多対多の確認候補に残し、同額の別取引を自動統合しない。
func Reconcile(incoming Observation, existing []Observation) []Match {
	var matches []Match
	for _, old := range existing {
		if incoming.DocumentID != "" && incoming.DocumentID == old.DocumentID {
			continue
		}
		match := Match{DocumentID: old.DocumentID, State: "POSSIBLE"}
		if incoming.Hash != "" && incoming.Hash == old.Hash {
			match.State = "DUPLICATE"
			match.Reasons = []string{"同じ原本"}
			matches = append(matches, match)
			continue
		}
		if incoming.SourceKind == "bank" || old.SourceKind == "bank" {
			// 銀行引落しは購入との同一性を示さず、決済債務の履行として別に記録する。
			continue
		}
		transaction := incoming.TransactionID != "" && incoming.TransactionID == old.TransactionID && normalize(incoming.Merchant) == normalize(old.Merchant)
		order := incoming.OrderID != "" && incoming.OrderID == old.OrderID
		amount := incoming.Amount == old.Amount
		merchant := normalize(incoming.Merchant) != "" && normalize(incoming.Merchant) == normalize(old.Merchant)
		near := !incoming.OccurredAt.IsZero() && !old.OccurredAt.IsZero() && incoming.OccurredAt.Sub(old.OccurredAt).Abs() <= 72*time.Hour
		card := incoming.CardLast4 != "" && incoming.CardLast4 == old.CardLast4
		if transaction {
			match.Reasons = append(match.Reasons, "同じ取引番号")
			if amount && merchant {
				match.State = "IDENTICAL"
			} else {
				match.State = "CONFLICT"
			}
		} else if order {
			match.Reasons = append(match.Reasons, "同じ注文番号")
			if !amount {
				match.Reasons = append(match.Reasons, "分割発送・分割決済または金額相違")
			}
		} else if !amount || !merchant || !near {
			continue
		}
		if amount {
			match.Reasons = append(match.Reasons, "金額一致")
		}
		if merchant {
			match.Reasons = append(match.Reasons, "店舗一致")
		}
		if near {
			match.Reasons = append(match.Reasons, "購入日時が近い")
		}
		if card {
			match.Reasons = append(match.Reasons, "カード末尾一致")
		}
		if incoming.CardLast4 != "" && old.CardLast4 != "" && !card {
			match.State = "CONFLICT"
			match.Reasons = append(match.Reasons, "カード末尾相違")
		}
		matches = append(matches, match)
	}
	return matches
}

func normalize(value string) string { return strings.ToLower(strings.Join(strings.Fields(value), "")) }
