package graph

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"strconv"

	"github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func latestHouseholdEventCursor(ctx context.Context, db *pgxpool.Pool, householdID uuid.UUID) (int64, error) {
	event, err := models.HouseholdEvents.Query(
		sm.Where(models.HouseholdEvents.Columns.HouseholdID.EQ(psql.Arg(householdID))),
		sm.OrderBy(models.HouseholdEvents.Columns.Cursor).Desc(),
		sm.Limit(1),
	).One(ctx, bobpgx.NewPool(db))
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return event.Cursor, nil
}

func purchaseChange(event *models.HouseholdEvent) (*model.PurchaseChange, bool) {
	kind := model.PurchaseChangeKind("")
	entityKind := model.PurchaseChangeEntityPurchase
	switch event.Type {
	case "purchase.created":
		kind = model.PurchaseChangeKindCreated
	case "purchase.posted":
		kind = model.PurchaseChangeKindPosted
	case "purchase.updated":
		kind = model.PurchaseChangeKindUpdated
	case "purchase.item.updated":
		kind = model.PurchaseChangeKindItemUpdated
		entityKind = model.PurchaseChangeEntityPurchaseItem
	case "purchase.item.deleted":
		if _, ok := deletedPurchaseID(event); !ok {
			return nil, false
		}
		kind = model.PurchaseChangeKindItemDeleted
		entityKind = model.PurchaseChangeEntityPurchaseItem
	case "purchase.corrected":
		kind = model.PurchaseChangeKindCorrected
	default:
		return nil, false
	}
	var fieldKeys []string
	if err := json.Unmarshal(event.ChangedFields.Val, &fieldKeys); err != nil {
		return nil, false
	}
	changedFields := make([]model.PurchaseChangeField, len(fieldKeys))
	for i, field := range fieldKeys {
		switch field {
		case fieldOccurredAt:
			changedFields[i] = model.PurchaseChangeFieldOccurredAt
		case fieldMerchant:
			changedFields[i] = model.PurchaseChangeFieldMerchant
		case fieldPaymentAllocations:
			changedFields[i] = model.PurchaseChangeFieldPaymentAllocations
		case fieldStatus:
			changedFields[i] = model.PurchaseChangeFieldStatus
		case "correction":
			changedFields[i] = model.PurchaseChangeFieldCorrection
		case fieldName:
			changedFields[i] = model.PurchaseChangeFieldName
		case fieldAmountYen:
			changedFields[i] = model.PurchaseChangeFieldAmountYen
		case fieldQuantity:
			changedFields[i] = model.PurchaseChangeFieldQuantity
		case fieldPrintedUnitPrice:
			changedFields[i] = model.PurchaseChangeFieldPrintedUnitPrice
		case fieldCategoryID:
			changedFields[i] = model.PurchaseChangeFieldCategoryID
		case fieldProductVariantID:
			changedFields[i] = model.PurchaseChangeFieldProductVariantID
		case fieldAllocations:
			changedFields[i] = model.PurchaseChangeFieldAllocations
		case fieldTags:
			changedFields[i] = model.PurchaseChangeFieldTags
		case "deleted":
			changedFields[i] = model.PurchaseChangeFieldDeleted
		default:
			return nil, false
		}
	}
	return &model.PurchaseChange{
		Cursor: strconv.FormatInt(event.Cursor, 10), Kind: kind, EntityKind: entityKind,
		EntityID: event.EntityID.String(), Revision: int(event.Revision), ChangedFields: changedFields,
		OccurredAt: timeString(event.CreatedAt),
	}, true
}

func purchaseForChange(ctx context.Context, db *pgxpool.Pool, event *models.HouseholdEvent) (*model.Purchase, error) {
	purchaseID := event.EntityID.String()
	switch event.Type {
	case "purchase.item.updated":
		item, err := models.PurchaseItems.Query(sm.Where(psql.And(
			models.PurchaseItems.Columns.ID.EQ(psql.Arg(event.EntityID)),
			models.PurchaseItems.Columns.HouseholdID.EQ(psql.Arg(event.HouseholdID)),
		))).One(ctx, bobpgx.NewPool(db))
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		purchaseID = item.PurchaseID.String()
	case "purchase.item.deleted":
		deletedPurchaseID, ok := deletedPurchaseID(event)
		if !ok {
			return nil, ErrNotFound
		}
		purchaseID = deletedPurchaseID
	}
	purchase, err := loadPurchase(ctx, db, purchaseID)
	if err != nil {
		return nil, err
	}
	if purchase.HouseholdID != event.HouseholdID.String() {
		return nil, ErrNotFound
	}
	return purchase, nil
}

func deletedPurchaseID(event *models.HouseholdEvent) (string, bool) {
	var payload struct {
		PurchaseID string `json:"purchaseId"`
	}
	if err := json.Unmarshal(event.Payload.Val, &payload); err != nil {
		return "", false
	}
	if _, err := uuid.FromString(payload.PurchaseID); err != nil {
		return "", false
	}
	return payload.PurchaseID, true
}
