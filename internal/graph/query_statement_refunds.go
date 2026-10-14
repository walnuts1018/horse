package graph

import (
	"context"

	"github.com/walnuts1018/horse/internal/application/purchase"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func (r *queryResolver) statementRefundCorrections(ctx context.Context, householdID, sourceDocumentID, sourceReference string, first *int, after *string) (*model.StatementRefundCorrectionConnection, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	pageSize, _, err := pageDirection(first, nil)
	if err != nil {
		return nil, err
	}
	var cursor string
	if value, err := cursorID(after, nodeTypePurchaseCorrection); err != nil {
		return nil, err
	} else if value != nil {
		cursor = *value
	}
	page, err := purchase.NewService(r.DB).StatementRefundCorrections(ctx, householdID, sourceDocumentID, sourceReference, cursor, pageSize)
	if err != nil {
		return nil, err
	}
	connection := &model.StatementRefundCorrectionConnection{
		Edges: make([]*model.StatementRefundCorrectionEdge, 0, len(page.Candidates)),
		PageInfo: &model.PageInfo{
			HasNextPage: page.HasNextPage, HasPreviousPage: after != nil && *after != "",
		},
	}
	for _, candidate := range page.Candidates {
		node := &model.StatementRefundCorrection{
			ID: candidate.CorrectionID, AmountYen: model.Long(candidate.AmountYen),
			OccurredAt: model.DateTime(candidate.OccurredAt), Merchant: candidate.Merchant,
			SourceFilename: candidate.SourceFilename,
		}
		connection.Edges = append(connection.Edges, &model.StatementRefundCorrectionEdge{
			Cursor: encodeCursor(nodeTypePurchaseCorrection, candidate.CorrectionID), Node: node,
		})
	}
	if len(connection.Edges) > 0 {
		start, end := connection.Edges[0].Cursor, connection.Edges[len(connection.Edges)-1].Cursor
		connection.PageInfo.StartCursor = &start
		connection.PageInfo.EndCursor = &end
	}
	return connection, nil
}
