package accounting

import (
	"context"
	"database/sql"
	"fmt"
	"math"

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
			debitTotal, ok = checkedAdd(debitTotal, posting.AmountYen)
		case Credit:
			entry.CreditYen = posting.AmountYen
			creditTotal, ok = checkedAdd(creditTotal, posting.AmountYen)
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

func InsertEntries(ctx context.Context, executor bob.Executor, journalID gofrsuuid.UUID, entries []Entry) error {
	for _, entry := range entries {
		if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
			JournalID: &journalID, LineNo: &entry.LineNo, AccountID: &entry.AccountID, PoolID: &entry.PoolID,
			MemberID: &entry.MemberID, DebitYen: &entry.DebitYen, CreditYen: &entry.CreditYen, Memo: &entry.Memo,
		}).Exec(ctx, executor); err != nil {
			return err
		}
	}
	return nil
}

func nullableUUID(value *gofrsuuid.UUID) sql.Null[gofrsuuid.UUID] {
	if value == nil {
		return sql.Null[gofrsuuid.UUID]{}
	}
	return sql.Null[gofrsuuid.UUID]{V: *value, Valid: true}
}

func checkedAdd(left, right int64) (int64, bool) {
	if right > 0 && left > math.MaxInt64-right {
		return 0, false
	}
	if right < 0 && left < math.MinInt64-right {
		return 0, false
	}
	return left + right, true
}
