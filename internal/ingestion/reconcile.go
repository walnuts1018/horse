package ingestion

import (
	"strings"
	"time"
)

type Observation struct {
	DocumentID      string    `json:"documentId"`
	SourceReference string    `json:"sourceReference"`
	Hash            string    `json:"hash"`
	SourceKind      string    `json:"sourceKind"`
	TransactionKind string    `json:"transactionKind"`
	OrderID         string    `json:"orderId"`
	TransactionID   string    `json:"transactionId"`
	Merchant        string    `json:"merchant"`
	OccurredAt      time.Time `json:"occurredAt"`
	Amount          int64     `json:"amount"`
	CardLast4       string    `json:"cardLast4"`
}

type Match struct {
	DocumentID      string   `json:"documentId"`
	SourceReference string   `json:"sourceReference"`
	State           string   `json:"state"`
	Reasons         []string `json:"reasons"`
}

// Reconcileは注文と分割決済を多対多の確認候補に残し、同額の別取引を自動統合しない。
func Reconcile(incoming Observation, existing []Observation) []Match {
	var matches []Match
	for _, old := range existing {
		if incoming.DocumentID != "" && incoming.DocumentID == old.DocumentID {
			continue
		}
		match := Match{DocumentID: old.DocumentID, SourceReference: old.SourceReference, State: statementMatchPossible}
		if incoming.Hash != "" && incoming.Hash == old.Hash {
			match.State = "DUPLICATE"
			match.Reasons = []string{"同じ原本"}
			matches = append(matches, match)
			continue
		}
		if !reconcilablePair(incoming, old) {
			continue
		}
		transaction := incoming.TransactionID != "" && incoming.TransactionID == old.TransactionID
		order := incoming.OrderID != "" && incoming.OrderID == old.OrderID
		amount := reconciliationAmountsEqual(incoming, old)
		merchant := normalize(incoming.Merchant) != "" && normalize(incoming.Merchant) == normalize(old.Merchant)
		merchantConflict := normalize(incoming.Merchant) != "" && normalize(old.Merchant) != "" && !merchant
		near := !incoming.OccurredAt.IsZero() && !old.OccurredAt.IsZero() && incoming.OccurredAt.Sub(old.OccurredAt).Abs() <= 72*time.Hour
		card := incoming.CardLast4 != "" && incoming.CardLast4 == old.CardLast4
		if transaction {
			match.Reasons = append(match.Reasons, "同じ取引番号")
			if amount && !merchantConflict {
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
		if merchantConflict {
			match.Reasons = append(match.Reasons, "店舗名相違")
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

// SourceObservationsは原本全体の購入とCSVの各行を区別し、再抽出しても同じ原本行を参照できるようにする。
func SourceObservations(documentID, hash, sourceKind string, result Verification) []Observation {
	observations := make([]Observation, 0, len(result.Statements)+1)
	for _, entry := range result.Statements {
		occurredAt, _ := time.Parse(time.RFC3339, entry.OccurredAt)
		reference := entry.SourceReference
		if reference == "" {
			reference = statementSourceReference(entry.Row)
		}
		observations = append(observations, Observation{DocumentID: documentID, SourceReference: reference, Hash: hash, SourceKind: sourceKind, TransactionKind: entry.Kind, OrderID: entry.OrderID, TransactionID: entry.TransactionID, Merchant: entry.Merchant, OccurredAt: occurredAt, Amount: entry.Amount, CardLast4: entry.CardLast4})
	}
	if len(result.Receipt.Items) > 0 {
		receipt := result.Receipt
		occurredAt, err := time.Parse(time.RFC3339, receipt.PurchasedAt)
		if err != nil {
			occurredAt, _ = time.Parse(time.DateOnly, receipt.PurchasedAt)
		}
		card := ""
		for _, payment := range receipt.Payments {
			if payment.CardLast4 == "" {
				continue
			}
			if card != "" && card != payment.CardLast4 {
				card = ""
				break
			}
			card = payment.CardLast4
		}
		observations = append(observations, Observation{DocumentID: documentID, Hash: hash, SourceKind: sourceKind, TransactionKind: statementKindReceipt, OrderID: receipt.OrderID, TransactionID: receipt.TransactionID, Merchant: receipt.Merchant, OccurredAt: occurredAt, Amount: receipt.Total, CardLast4: card})
	}
	return observations
}

func reconcilablePair(left, right Observation) bool {
	if left.SourceKind != statementSourceBank && right.SourceKind != statementSourceBank {
		return true
	}
	if left.SourceKind == statementSourceBank {
		return left.TransactionKind == statementKindPayment && right.TransactionKind == statementKindReceipt
	}
	return right.TransactionKind == statementKindPayment && left.TransactionKind == statementKindReceipt
}

func reconciliationAmountsEqual(left, right Observation) bool {
	if left.SourceKind == statementSourceBank && left.TransactionKind == statementKindPayment && right.TransactionKind == statementKindReceipt {
		return left.Amount < 0 && right.Amount > 0 && left.Amount == -right.Amount
	}
	if right.SourceKind == statementSourceBank && right.TransactionKind == statementKindPayment && left.TransactionKind == statementKindReceipt {
		return right.Amount < 0 && left.Amount > 0 && right.Amount == -left.Amount
	}
	return left.Amount == right.Amount
}

func normalize(value string) string { return strings.ToLower(strings.Join(strings.Fields(value), "")) }
