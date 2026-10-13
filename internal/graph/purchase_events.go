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
		case "occurredAt":
			changedFields[i] = model.PurchaseChangeFieldOccurredAt
		case "merchant":
			changedFields[i] = model.PurchaseChangeFieldMerchant
		case "paymentAllocations":
			changedFields[i] = model.PurchaseChangeFieldPaymentAllocations
		case "status":
			changedFields[i] = model.PurchaseChangeFieldStatus
		case "correction":
			changedFields[i] = model.PurchaseChangeFieldCorrection
		case "name":
			changedFields[i] = model.PurchaseChangeFieldName
		case "amountYen":
			changedFields[i] = model.PurchaseChangeFieldAmountYen
		case "quantity":
			changedFields[i] = model.PurchaseChangeFieldQuantity
		case "printedUnitPrice":
			changedFields[i] = model.PurchaseChangeFieldPrintedUnitPrice
		case "categoryId":
			changedFields[i] = model.PurchaseChangeFieldCategoryID
		case "productVariantId":
			changedFields[i] = model.PurchaseChangeFieldProductVariantID
		case "allocations":
			changedFields[i] = model.PurchaseChangeFieldAllocations
		case "tags":
			changedFields[i] = model.PurchaseChangeFieldTags
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
