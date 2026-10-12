package graph

import (
	"errors"

	"github.com/walnuts1018/horse/internal/graph/model"
)

type mutationErrorResultData struct {
	Code    model.MutationErrorCode
	Message string
}

func mutationErrorResult(err error) (*mutationErrorResultData, bool) {
	switch {
	case errors.Is(err, ErrBadInput):
		return &mutationErrorResultData{Code: model.MutationErrorCodeBadInput, Message: "入力を確認してください。"}, true
	case errors.Is(err, ErrConflict), errors.Is(err, ErrUnattributedPurchasePayment), errors.Is(err, ErrMissingPurchaseFundingSnapshot):
		return &mutationErrorResultData{Code: model.MutationErrorCodeConflict, Message: "最新の状態を読み込んでから再試行してください。"}, true
	case errors.Is(err, ErrNotFound):
		return &mutationErrorResultData{Code: model.MutationErrorCodeNotFound, Message: "対象が見つかりません。"}, true
	default:
		return nil, false
	}
}
