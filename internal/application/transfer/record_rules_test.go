package transfer

import (
	"errors"
	"testing"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
)

func TestValidateCommandRulesEnforcesTransferKindRules(t *testing.T) {
	command := Command{
		FromMemberID: "from", ToMemberID: "to", Kind: loanKind, AmountYen: 100,
		OccurredAt: time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC), IdempotencyKey: "key",
	}
	if provided, err := validateCommandRules(command); err != nil || provided {
		t.Fatalf("valid loan: provided=%v err=%v", provided, err)
	}

	tests := []struct {
		name   string
		mutate func(*Command)
	}{
		{name: "unknown kind", mutate: func(c *Command) { c.Kind = "UNKNOWN" }},
		{name: "repayment without loan", mutate: func(c *Command) { c.Kind = repaymentKind }},
		{name: "loan id on gift", mutate: func(c *Command) { c.Kind = "GIFT"; c.LoanID = new("loan") }},
		{name: "same members", mutate: func(c *Command) { c.ToMemberID = c.FromMemberID }},
		{name: "statement source on gift", mutate: func(c *Command) {
			c.Kind, c.SourceDocumentID, c.SourceReference, c.SourceAccountID = "GIFT", "doc", "row", "account"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invalid := command
			test.mutate(&invalid)
			if _, err := validateCommandRules(invalid); !errors.Is(err, ErrBadInput) {
				t.Fatalf("error=%v, want ErrBadInput", err)
			}
		})
	}

	command.Kind, command.LoanID = repaymentKind, new("loan-id")
	command.SourceDocumentID, command.SourceReference, command.SourceAccountID = "doc", "row", "account"
	if provided, err := validateCommandRules(command); err != nil || !provided {
		t.Fatalf("valid repayment source: provided=%v err=%v", provided, err)
	}
}

func TestBuildJournalLinesBalancesTransferKindsAndAccountOptions(t *testing.T) {
	from, to, paymentPool, paymentAccount, destinationAccount := uuid(1), uuid(2), uuid(3), uuid(4), uuid(5)
	tests := []struct {
		name               string
		paymentPool        *gofrsuuid.UUID
		paymentAccount     *gofrsuuid.UUID
		destinationAccount *gofrsuuid.UUID
		wantLines          int
	}{
		{name: "members only", wantLines: 2},
		{name: "payment", paymentPool: &paymentPool, paymentAccount: &paymentAccount, wantLines: 6},
		{name: "destination", destinationAccount: &destinationAccount, wantLines: 4},
		{name: "both accounts", paymentPool: &paymentPool, paymentAccount: &paymentAccount, destinationAccount: &destinationAccount, wantLines: 8},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lines, err := buildJournalLines("GIFT", 125, from, to, test.paymentPool, test.paymentAccount, test.destinationAccount)
			if err != nil {
				t.Fatal(err)
			}
			if len(lines) != test.wantLines {
				t.Fatalf("got %d lines, want %d", len(lines), test.wantLines)
			}
			var debits, credits int64
			for index, line := range lines {
				if line.LineNo != int32(index+1) {
					t.Fatalf("line %d has number %d", index, line.LineNo)
				}
				debits += line.DebitYen
				credits += line.CreditYen
			}
			if debits != credits {
				t.Fatalf("debits=%d credits=%d", debits, credits)
			}
		})
	}
	if _, err := buildJournalLines("LOAN", 125, from, to, nil, &paymentAccount, nil); !errors.Is(err, ErrBadInput) {
		t.Fatalf("missing payment pool error=%v", err)
	}
}

func TestSettlementAdjustmentsByTransferKind(t *testing.T) {
	from, to := uuid(1), uuid(2)
	tests := []struct {
		name     string
		kind     string
		poolKind string
		want     []settlementAdjustment
	}{
		{name: "loan", kind: loanKind, want: []settlementAdjustment{{memberID: from, amount: -80}, {memberID: to, amount: 80}}},
		{name: "repayment", kind: repaymentKind, want: []settlementAdjustment{{memberID: from, amount: -80}, {memberID: to, amount: 80}}},
		{name: "shared repayment", kind: repaymentKind, poolKind: "SHARED", want: []settlementAdjustment{{memberID: to, amount: 80}}},
		{name: "gift", kind: "GIFT"},
		{name: "transfer", kind: "TRANSFER"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := settlementAdjustments(test.kind, test.poolKind, 80, from, to)
			if len(got) != len(test.want) {
				t.Fatalf("got %v, want %v", got, test.want)
			}
			for index := range got {
				if got[index] != test.want[index] {
					t.Fatalf("got %v, want %v", got, test.want)
				}
			}
		})
	}
}

func TestValidateRepaymentAmountEnforcesOutstandingBalance(t *testing.T) {
	tests := []struct {
		name            string
		loanAmount      int64
		repaid          int64
		repaymentAmount int64
		wantErr         bool
	}{
		{name: "partial repayment", loanAmount: 100, repaid: 20, repaymentAmount: 50},
		{name: "exact outstanding balance", loanAmount: 100, repaid: 20, repaymentAmount: 80},
		{name: "exceeds outstanding balance", loanAmount: 100, repaid: 20, repaymentAmount: 81, wantErr: true},
		{name: "fully repaid loan", loanAmount: 100, repaid: 100, repaymentAmount: 1, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateRepaymentAmount(test.loanAmount, test.repaid, test.repaymentAmount)
			if test.wantErr != errors.Is(err, ErrBadInput) {
				t.Fatalf("error=%v, want ErrBadInput=%v", err, test.wantErr)
			}
		})
	}
}

func uuid(last byte) gofrsuuid.UUID {
	var id gofrsuuid.UUID
	id[15] = last
	return id
}
