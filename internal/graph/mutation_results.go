package graph

import (
	"errors"

	"github.com/walnuts1018/horse/internal/application/funding"
	"github.com/walnuts1018/horse/internal/application/transfer"
	"github.com/walnuts1018/horse/internal/graph/model"
)

type mutationErrorResultData struct {
	Code    model.MutationErrorCode
	Message string
}

func mutationErrorResult(err error) (*mutationErrorResultData, bool) {
	switch {
	case errors.Is(err, ErrBadInput), errors.Is(err, funding.ErrBadInput), errors.Is(err, transfer.ErrBadInput):
		return &mutationErrorResultData{Code: model.MutationErrorCodeBadInput, Message: "入力を確認してください。"}, true
	case errors.Is(err, ErrConflict), errors.Is(err, funding.ErrConflict), errors.Is(err, transfer.ErrConflict), errors.Is(err, ErrUnattributedPurchasePayment), errors.Is(err, ErrMissingPurchaseFundingSnapshot):
		return &mutationErrorResultData{Code: model.MutationErrorCodeConflict, Message: "最新の状態を読み込んでから再試行してください。"}, true
	case errors.Is(err, ErrNotFound), errors.Is(err, funding.ErrNotFound), errors.Is(err, transfer.ErrNotFound):
		return &mutationErrorResultData{Code: model.MutationErrorCodeNotFound, Message: "対象が見つかりません。"}, true
	default:
		return nil, false
	}
}
