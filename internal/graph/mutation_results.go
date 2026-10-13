package graph

import (
	"errors"

	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/graph/model"
)

type mutationErrorResultData struct {
	Code    model.MutationErrorCode
	Message string
}

func mutationErrorResult(err error) (*mutationErrorResultData, bool) {
	if kind, ok := ApplicationErrorKind(err); ok {
		switch kind {
		case apperror.KindBadInput:
			return &mutationErrorResultData{Code: model.MutationErrorCodeBadInput, Message: "入力を確認してください。"}, true
		case apperror.KindConflict:
			return &mutationErrorResultData{Code: model.MutationErrorCodeConflict, Message: "最新の状態を読み込んでから再試行してください。"}, true
		case apperror.KindNotFound:
			return &mutationErrorResultData{Code: model.MutationErrorCodeNotFound, Message: "対象が見つかりません。"}, true
		case apperror.KindUnknown:
			return nil, false
		}
	}
	if errors.Is(err, ErrUnattributedPurchasePayment) || errors.Is(err, ErrMissingPurchaseFundingSnapshot) {
		return &mutationErrorResultData{Code: model.MutationErrorCodeConflict, Message: "最新の状態を読み込んでから再試行してください。"}, true
	}
	return nil, false
}
