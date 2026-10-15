package purchase

import (
	"errors"
	"testing"

	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestPreparePurchaseItemUpdateValues(t *testing.T) {
	t.Parallel()

	name := "  milk  "
	amount := int64(250)
	categoryID := "category-id"
	tests := []struct {
		name       string
		command    UpdateItemCommand
		wantName   string
		wantAmount int64
		wantErr    bool
	}{
		{
			name:       "keeps untouched values",
			command:    UpdateItemCommand{},
			wantName:   "milk",
			wantAmount: 100,
		},
		{
			name: "normalizes changed values",
			command: UpdateItemCommand{
				Name: &name, AmountYen: &amount, CategoryIDSet: true, CategoryID: &categoryID,
			},
			wantName:   "milk",
			wantAmount: amount,
		},
		{
			name:    "rejects an empty normalized name",
			command: UpdateItemCommand{Name: new(" \t ")},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			values, err := preparePurchaseItemUpdateValues(test.command, &models.PurchaseItem{Name: "milk", AmountYen: 100})
			if test.wantErr {
				if !errors.Is(err, ErrBadInput) {
					t.Fatalf("preparePurchaseItemUpdateValues() error = %v, want ErrBadInput", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("preparePurchaseItemUpdateValues() error = %v, want nil", err)
			}
			if values.name != test.wantName || values.amount != test.wantAmount {
				t.Fatalf("prepared values = (%q, %d), want (%q, %d)", values.name, values.amount, test.wantName, test.wantAmount)
			}
			if test.command.CategoryIDSet && (values.categoryID == nil || *values.categoryID != categoryID) {
				t.Fatalf("prepared categoryID = %v, want %q", values.categoryID, categoryID)
			}
		})
	}
}
