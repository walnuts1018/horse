package graph

import (
	"strconv"

	purchaseapp "github.com/walnuts1018/horse/internal/application/purchase"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func sharedPurchaseDraftChange(revision int64) *model.SharedPurchaseDraftChange {
	return &model.SharedPurchaseDraftChange{Cursor: strconv.FormatInt(revision, 10)}
}

func graphSharedPurchaseDraft(draft purchaseapp.SharedPurchaseDraft) *model.SharedPurchaseDraft {
	return &model.SharedPurchaseDraft{
		ID: draft.ID, HouseholdID: draft.HouseholdID, Revision: int(draft.Revision), Payload: draft.Payload,
		CreatedAt: timeString(draft.CreatedAt), UpdatedAt: timeString(draft.UpdatedAt),
	}
}
