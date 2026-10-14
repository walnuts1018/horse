package graph

import (
	"strconv"

	"github.com/walnuts1018/horse/internal/graph/model"
)

func sharedPurchaseDraftChange(revision int64) *model.SharedPurchaseDraftChange {
	return &model.SharedPurchaseDraftChange{Cursor: strconv.FormatInt(revision, 10)}
}
