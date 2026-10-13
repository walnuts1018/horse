package graph

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestPurchaseChangeUsesKnownFieldEnumValues(t *testing.T) {
	event := &models.HouseholdEvent{
		Cursor: 1, Type: "purchase.item.updated", EntityID: uuid.Must(uuid.NewV7()), Revision: 2,
		ChangedFields: types.JSON[json.RawMessage]{Val: json.RawMessage(`["name","quantity"]`)},
	}
	change, ok := purchaseChange(event)
	if !ok {
		t.Fatal("purchase item change was not converted")
	}
	want := []model.PurchaseChangeField{model.PurchaseChangeFieldName, model.PurchaseChangeFieldQuantity}
	if !reflect.DeepEqual(change.ChangedFields, want) {
		t.Fatalf("changed fields = %v, want %v", change.ChangedFields, want)
	}
}

func TestPurchaseChangeRejectsUnknownFieldKeys(t *testing.T) {
	event := &models.HouseholdEvent{
		Type:          "purchase.updated",
		ChangedFields: types.JSON[json.RawMessage]{Val: json.RawMessage(`["unknown"]`)},
	}
	if _, ok := purchaseChange(event); ok {
		t.Fatal("purchase change with an unknown field was converted")
	}
}
