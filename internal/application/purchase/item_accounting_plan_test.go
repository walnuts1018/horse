package purchase

import (
	"slices"
	"testing"
)

func TestAdjustPaymentsToTotal(t *testing.T) {
	payments := []Payment{
		{AccountID: "account-a", AmountYen: 1},
		{AccountID: "account-b", AmountYen: 1},
		{AccountID: "account-c", AmountYen: 1},
	}

	got, err := adjustPaymentsToTotal(payments, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := []Payment{
		{AccountID: "account-a", AmountYen: 1},
		{AccountID: "account-b", AmountYen: 1},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("adjusted payments = %#v, want %#v", got, want)
	}
}

func TestBuildPurchaseItemAccountingPlan(t *testing.T) {
	payment := Payment{AccountID: "account-a", AmountYen: 100}
	tests := []struct {
		name                     string
		status                   string
		replacePayments          bool
		allocationsChanged       bool
		deferAccountingSync      bool
		wantReplacePayments      bool
		wantSyncPostedAccounting bool
	}{
		{
			name:                     "posted allocation change",
			status:                   postedStatus,
			replacePayments:          true,
			allocationsChanged:       true,
			wantReplacePayments:      true,
			wantSyncPostedAccounting: true,
		},
		{
			name:                "posted deferred allocation change",
			status:              postedStatus,
			allocationsChanged:  true,
			deferAccountingSync: true,
		},
		{
			name:               "draft allocation change",
			status:             draftStatus,
			allocationsChanged: true,
		},
		{
			name:   "posted non-accounting change",
			status: postedStatus,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := buildPurchaseItemAccountingPlan(test.status, []Payment{payment}, test.replacePayments, test.allocationsChanged, test.deferAccountingSync)
			if plan.replacePayments != test.wantReplacePayments || plan.syncPostedAccounting != test.wantSyncPostedAccounting {
				t.Fatalf("accounting plan = %#v", plan)
			}
			if !slices.Equal(plan.payments, []Payment{payment}) {
				t.Fatalf("planned payments = %#v, want %#v", plan.payments, []Payment{payment})
			}
		})
	}
}
