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
	t.Parallel()
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
	t.Parallel()
	event := &models.HouseholdEvent{
		Type:          "purchase.updated",
		ChangedFields: types.JSON[json.RawMessage]{Val: json.RawMessage(`["unknown"]`)},
	}
	if _, ok := purchaseChange(event); ok {
		t.Fatal("purchase change with an unknown field was converted")
	}
}

func TestPurchaseItemDeletionProducesChange(t *testing.T) {
	t.Parallel()
	event := &models.HouseholdEvent{
		Cursor: 1, Type: "purchase.item.deleted", EntityID: uuid.Must(uuid.NewV7()), Revision: 3,
		ChangedFields: types.JSON[json.RawMessage]{Val: json.RawMessage(`["deleted"]`)},
		Payload:       types.JSON[json.RawMessage]{Val: json.RawMessage(`{"purchaseId":"0198a9f1-4f62-7d21-b2eb-57a5fa43892f"}`)},
	}
	change, ok := purchaseChange(event)
	if !ok {
		t.Fatal("purchase item deletion was not converted")
	}
	if change.Kind != model.PurchaseChangeKindItemDeleted || change.EntityKind != model.PurchaseChangeEntityPurchaseItem {
		t.Fatalf("purchase item deletion kind = %s/%s", change.Kind, change.EntityKind)
	}
	if !reflect.DeepEqual(change.ChangedFields, []model.PurchaseChangeField{model.PurchaseChangeFieldDeleted}) {
		t.Fatalf("changed fields = %v, want DELETED", change.ChangedFields)
	}
}
