package accountmovement

import (
	"testing"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/walnuts1018/horse/internal/ingestion"
)

func TestValidateStatementTransfer(t *testing.T) {
	t.Parallel()
	statementAccount := gofrsuuid.Must(gofrsuuid.FromString("019a0000-0000-7000-8000-000000000001"))
	otherAccount := gofrsuuid.Must(gofrsuuid.FromString("019a0000-0000-7000-8000-000000000002"))
	occurredAt := time.Date(2026, time.June, 10, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		entryAmount int64
		statementID gofrsuuid.UUID
		fromID      gofrsuuid.UUID
		toID        gofrsuuid.UUID
		amount      int64
		occurredAt  time.Time
		wantError   bool
	}{
		{name: "outgoing statement row", entryAmount: -250, statementID: statementAccount, fromID: statementAccount, toID: otherAccount, amount: 250, occurredAt: occurredAt},
		{name: "incoming statement row", entryAmount: 250, statementID: statementAccount, fromID: otherAccount, toID: statementAccount, amount: 250, occurredAt: occurredAt},
		{name: "direction mismatch", entryAmount: -250, statementID: statementAccount, fromID: otherAccount, toID: statementAccount, amount: 250, occurredAt: occurredAt, wantError: true},
		{name: "amount mismatch", entryAmount: -250, statementID: statementAccount, fromID: statementAccount, toID: otherAccount, amount: 251, occurredAt: occurredAt, wantError: true},
		{name: "date mismatch", entryAmount: -250, statementID: statementAccount, fromID: statementAccount, toID: otherAccount, amount: 250, occurredAt: occurredAt.Add(time.Second), wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateStatementTransfer(ingestion.StatementEntry{
				Kind: "ACCOUNT_TRANSFER", Amount: test.entryAmount, OccurredAt: occurredAt.Format(time.RFC3339),
			}, test.statementID, test.fromID, test.toID, test.amount, test.occurredAt)
			if (err != nil) != test.wantError {
				t.Fatalf("validateStatementTransfer() error = %v, wantError %t", err, test.wantError)
			}
		})
	}
}
