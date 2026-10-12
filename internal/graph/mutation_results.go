package graph

import (
	"errors"

	"github.com/walnuts1018/horse/internal/graph/model"
)

func mutationErrorResult(err error) (*model.MutationError, bool) {
	switch {
	case errors.Is(err, ErrBadInput):
		return &model.MutationError{Code: model.MutationErrorCodeBadInput, Message: "入力を確認してください。"}, true
	case errors.Is(err, ErrConflict), errors.Is(err, ErrUnattributedPurchasePayment), errors.Is(err, ErrMissingPurchaseFundingSnapshot):
		return &model.MutationError{Code: model.MutationErrorCodeConflict, Message: "最新の状態を読み込んでから再試行してください。"}, true
	case errors.Is(err, ErrNotFound):
		return &model.MutationError{Code: model.MutationErrorCodeNotFound, Message: "対象が見つかりません。"}, true
	default:
		return nil, false
	}
}
