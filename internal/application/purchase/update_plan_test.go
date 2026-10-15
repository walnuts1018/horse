package purchase

import (
	stdjson "encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestPlanPurchaseUpdateKeepsPartialFieldConflict(t *testing.T) {
	t.Parallel()

	occurredAt := time.Date(2026, time.October, 13, 12, 0, 0, 0, time.UTC)
	merchant := "Updated merchant"
	tests := []struct {
		name           string
		baseRevision   int64
		fieldRevisions string
		wantErr        error
		wantRevision   int64
		wantMerchant   string
	}{
		{
			name:           "unrelated newer field does not conflict",
			baseRevision:   2,
			fieldRevisions: `{"occurredAt":3,"merchant":2}`,
			wantRevision:   4,
			wantMerchant:   merchant,
		},
		{
			name:           "changed field newer than base conflicts",
			baseRevision:   1,
			fieldRevisions: `{"occurredAt":3,"merchant":2}`,
			wantErr:        ErrConflict,
			wantMerchant:   "Before",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			purchase := &models.Purchase{
				OccurredAt:     occurredAt,
				Merchant:       "Before",
				Revision:       3,
				FieldRevisions: types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(test.fieldRevisions)},
			}
			plan, err := planPurchaseUpdate(purchase, occurredAt, test.baseRevision, []string{updateFieldMerchant}, nil, nil, &merchant)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("planPurchaseUpdate() error = %v, want %v", err, test.wantErr)
			}
			if err != nil {
				if purchase.Merchant != test.wantMerchant {
					t.Fatalf("merchant after conflict = %q, want %q", purchase.Merchant, test.wantMerchant)
				}
				return
			}
			if plan.revision != test.wantRevision || purchase.Merchant != test.wantMerchant || plan.accountingChanged {
				t.Fatalf("plan = (revision %d, merchant %q, accounting changed %t), want (%d, %q, false)", plan.revision, purchase.Merchant, plan.accountingChanged, test.wantRevision, test.wantMerchant)
			}
		})
	}
}
