package accounting

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"slices"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type Side uint8

const (
	Debit Side = iota + 1
	Credit
)

type Posting struct {
	Side      Side
	AccountID *gofrsuuid.UUID
	PoolID    *gofrsuuid.UUID
	MemberID  *gofrsuuid.UUID
	AmountYen int64
	Memo      string
}

type PurchaseAllocation struct {
	PoolID    gofrsuuid.UUID
	AmountYen int64
}

type PurchasePayment struct {
	AccountID gofrsuuid.UUID
	AmountYen int64
}

type Entry struct {
	LineNo    int32
	AccountID sql.Null[gofrsuuid.UUID]
	PoolID    sql.Null[gofrsuuid.UUID]
	MemberID  sql.Null[gofrsuuid.UUID]
	DebitYen  int64
	CreditYen int64
	Memo      string
}

func BuildEntries(postings []Posting) ([]Entry, error) {
	if len(postings) == 0 || uint64(len(postings)) > math.MaxInt32 {
		return nil, fmt.Errorf("invalid journal entry count")
	}
	entries := make([]Entry, 0, len(postings))
	var debitTotal, creditTotal int64
	for index, posting := range postings {
		if posting.AmountYen <= 0 {
			return nil, fmt.Errorf("journal posting amount must be positive")
		}
		entry := Entry{
			LineNo:    int32(index + 1),
			AccountID: nullableUUID(posting.AccountID),
			PoolID:    nullableUUID(posting.PoolID),
			MemberID:  nullableUUID(posting.MemberID),
			Memo:      posting.Memo,
		}
		var ok bool
		switch posting.Side {
		case Debit:
			entry.DebitYen = posting.AmountYen
			debitTotal, ok = Add(debitTotal, posting.AmountYen)
		case Credit:
			entry.CreditYen = posting.AmountYen
			creditTotal, ok = Add(creditTotal, posting.AmountYen)
		default:
			return nil, fmt.Errorf("invalid journal side")
		}
		if !ok {
			return nil, fmt.Errorf("journal total exceeds int64")
		}
		entries = append(entries, entry)
	}
	if debitTotal != creditTotal {
		return nil, fmt.Errorf("journal debits and credits do not balance")
	}
	return entries, nil
}

func BuildPurchaseEntries(allocations []PurchaseAllocation, payments []PurchasePayment, totalYen int64) ([]Entry, error) {
	if totalYen <= 0 {
		return nil, fmt.Errorf("purchase total must be positive")
	}
	if len(payments) > 0 {
		var paidTotal int64
		for _, payment := range payments {
			var ok bool
			paidTotal, ok = Add(paidTotal, payment.AmountYen)
			if !ok {
				return nil, fmt.Errorf("purchase payment total exceeds int64")
			}
		}
		if paidTotal != totalYen {
			return nil, fmt.Errorf("purchase payments do not equal purchase total")
		}
	}
	sharesByPool := make(map[gofrsuuid.UUID]int64, len(allocations))
	for _, allocation := range allocations {
		amount, ok := Add(sharesByPool[allocation.PoolID], allocation.AmountYen)
		if !ok {
			return nil, fmt.Errorf("purchase allocation total exceeds int64")
		}
		sharesByPool[allocation.PoolID] = amount
	}
	pools := make([]gofrsuuid.UUID, 0, len(sharesByPool))
	for poolID := range sharesByPool {
		pools = append(pools, poolID)
	}
	slices.SortFunc(pools, func(left, right gofrsuuid.UUID) int {
		return strings.Compare(left.String(), right.String())
	})

	postings := make([]Posting, 0, len(pools)+len(payments)+1)
	for _, poolID := range pools {
		amount := sharesByPool[poolID]
		if amount == 0 {
			continue
		}
		postings = append(postings, Posting{Side: Debit, PoolID: &poolID, AmountYen: amount, Memo: "Purchase allocation"})
	}
	if len(payments) == 0 {
		postings = append(postings, Posting{Side: Credit, AmountYen: totalYen, Memo: "Payment"})
	} else {
		for _, payment := range payments {
			postings = append(postings, Posting{Side: Credit, AccountID: &payment.AccountID, AmountYen: payment.AmountYen, Memo: "Payment"})
		}
	}
	return BuildEntries(postings)
}

func InsertEntries(ctx context.Context, executor bob.Executor, journalID gofrsuuid.UUID, entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}
	setters := make([]*models.JournalEntrySetter, 0, len(entries))
	for _, entry := range entries {
		setters = append(setters, &models.JournalEntrySetter{
			JournalID: &journalID, LineNo: &entry.LineNo, AccountID: &entry.AccountID, PoolID: &entry.PoolID,
			MemberID: &entry.MemberID, DebitYen: &entry.DebitYen, CreditYen: &entry.CreditYen, Memo: &entry.Memo,
		})
	}
	_, err := models.JournalEntries.Insert(bob.ToMods(setters...)).All(ctx, executor)
	return err
}

func nullableUUID(value *gofrsuuid.UUID) sql.Null[gofrsuuid.UUID] {
	if value == nil {
		return sql.Null[gofrsuuid.UUID]{}
	}
	return sql.Null[gofrsuuid.UUID]{V: *value, Valid: true}
}

func Add(left, right int64) (int64, bool) {
	if right > 0 && left > math.MaxInt64-right {
		return 0, false
	}
	if right < 0 && left < math.MinInt64-right {
		return 0, false
	}
	return left + right, true
}
