package graph

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dialect"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type unmatchedObservationCursor struct {
	DocumentID      string `json:"documentId"`
	SourceReference string `json:"sourceReference"`
}

func unmatchedCursor(documentID, reference string) string {
	payload, _ := json.Marshal(unmatchedObservationCursor{DocumentID: documentID, SourceReference: reference})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func parseUnmatchedCursor(cursor string) (unmatchedObservationCursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return unmatchedObservationCursor{}, fmt.Errorf("%w: invalid observation cursor", ErrBadInput)
	}
	var parsed unmatchedObservationCursor
	if err := json.Unmarshal(payload, &parsed); err != nil || parsed.DocumentID == "" {
		return unmatchedObservationCursor{}, fmt.Errorf("%w: invalid observation cursor", ErrBadInput)
	}
	return parsed, nil
}

func unmatchedObservationQuery(filters []bob.Expression, mods ...bob.Mod[*dialect.SelectQuery]) models.SourceObservationsQuery {
	queryMods := []bob.Mod[*dialect.SelectQuery]{
		sm.InnerJoin(models.SourceDocuments.NameExpr()).On(models.SourceDocuments.Columns.ID.EQ(models.SourceObservations.Columns.SourceDocumentID)),
		sm.Where(psql.And(filters...)),
	}
	queryMods = append(queryMods, mods...)
	return models.SourceObservations.Query(queryMods...)
}

func unmatchedObservationFilters(householdID gofrsuuid.UUID, fromMonth, toMonth time.Time, sourceKind *model.SourceKind, state *model.UnmatchedObservationState) []bob.Expression {
	filters := []bob.Expression{models.SourceObservations.Columns.HouseholdID.EQ(psql.Arg(householdID))}
	if sourceKind != nil {
		filters = append(filters, models.SourceObservations.Columns.SourceKind.EQ(psql.Arg(strings.ToLower(string(*sourceKind)))))
	}
	if !fromMonth.IsZero() {
		filters = append(filters, models.SourceObservations.Columns.OccurredAt.GTE(psql.Arg(fromMonth)))
	}
	if !toMonth.IsZero() {
		filters = append(filters, models.SourceObservations.Columns.OccurredAt.LT(psql.Arg(toMonth)))
	}
	if state != nil {
		confirmation := models.DocumentConfirmations.Query(sm.Where(psql.And(
			models.DocumentConfirmations.Columns.SourceDocumentID.EQ(models.SourceObservations.Columns.SourceDocumentID),
			models.DocumentConfirmations.Columns.SourceReference.EQ(models.SourceObservations.Columns.SourceReference),
			psql.Or(models.DocumentConfirmations.Columns.PurchaseID.IsNotNull(), models.DocumentConfirmations.Columns.CorrectionID.IsNotNull()),
		)))
		isConfirmed := psql.Exists(confirmation)
		switch *state {
		case model.UnmatchedObservationStateConfirmed:
			filters = append(filters, isConfirmed)
		case model.UnmatchedObservationStateUnmatched:
			filters = append(filters, psql.Not(isConfirmed))
		default:
			filters = append(filters, psql.Arg(false).EQ(psql.Arg(true)))
		}
	}
	return filters
}

func unmatchedObservationCursorPosition(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, filters []bob.Expression, value *string) (int64, error) {
	if value == nil || *value == "" {
		return -1, nil
	}
	parsed, err := parseUnmatchedCursor(*value)
	if err != nil {
		return 0, err
	}
	documentID, err := gofrsuuid.FromString(parsed.DocumentID)
	if err != nil {
		return 0, ErrNotFound
	}
	cursorFilters := append([]bob.Expression(nil), filters...)
	cursorFilters = append(cursorFilters,
		models.SourceObservations.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.SourceObservations.Columns.SourceDocumentID.EQ(psql.Arg(documentID)),
		models.SourceObservations.Columns.SourceReference.EQ(psql.Arg(parsed.SourceReference)),
	)
	observation, err := unmatchedObservationQuery(cursorFilters).One(ctx, executor)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	document, err := models.SourceDocuments.Query(sm.Where(psql.And(
		models.SourceDocuments.Columns.ID.EQ(psql.Arg(observation.SourceDocumentID)),
		models.SourceDocuments.Columns.HouseholdID.EQ(psql.Arg(householdID)),
	))).One(ctx, executor)
	if err != nil {
		return 0, err
	}
	preceding := psql.Or(
		models.SourceDocuments.Columns.CreatedAt.GT(psql.Arg(document.CreatedAt)),
		psql.And(
			models.SourceDocuments.Columns.CreatedAt.EQ(psql.Arg(document.CreatedAt)),
			models.SourceDocuments.Columns.ID.GT(psql.Arg(document.ID)),
		),
		psql.And(
			models.SourceDocuments.Columns.CreatedAt.EQ(psql.Arg(document.CreatedAt)),
			models.SourceDocuments.Columns.ID.EQ(psql.Arg(document.ID)),
			models.SourceObservations.Columns.SourceReference.LT(psql.Arg(parsed.SourceReference)),
		),
	)
	positionFilters := append([]bob.Expression(nil), filters...)
	positionFilters = append(positionFilters, preceding)
	return unmatchedObservationQuery(positionFilters).Count(ctx, executor)
}

func (r *queryResolver) unmatchedObservations(ctx context.Context, householdID string, from, to *string, sourceKind *model.SourceKind, state *model.UnmatchedObservationState, after, before *string, first, last *int) (*model.UnmatchedObservationConnection, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	pageSize, backward, err := pageDirection(first, last)
	if err != nil {
		return nil, err
	}
	var fromMonth, toMonth time.Time
	if from != nil {
		parsed, err := parseAccountingPeriod(*from)
		if err != nil {
			return nil, fmt.Errorf("%w: from must use YYYY-MM", ErrBadInput)
		}
		fromMonth = parsed
	}
	if to != nil {
		parsed, err := parseAccountingPeriod(*to)
		if err != nil {
			return nil, fmt.Errorf("%w: to must use YYYY-MM", ErrBadInput)
		}
		toMonth = parsed.AddDate(0, 1, 0)
	}
	if !fromMonth.IsZero() && !toMonth.IsZero() && toMonth.Before(fromMonth) {
		return nil, fmt.Errorf("%w: to must not precede from", ErrBadInput)
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	executor := queryExecutor(r.DB)
	filters := unmatchedObservationFilters(hhID, fromMonth, toMonth, sourceKind, state)
	totalCount, err := unmatchedObservationQuery(filters).Count(ctx, executor)
	if err != nil {
		return nil, err
	}
	start, end := int64(0), totalCount
	if after != nil && *after != "" {
		position, err := unmatchedObservationCursorPosition(ctx, executor, hhID, filters, after)
		if err != nil {
			return nil, err
		}
		start = position + 1
	}
	if before != nil && *before != "" {
		position, err := unmatchedObservationCursorPosition(ctx, executor, hhID, filters, before)
		if err != nil {
			return nil, err
		}
		end = position
	}
	if start > end {
		return nil, fmt.Errorf("%w: cursor range is empty", ErrBadInput)
	}
	pageStart, pageEnd := start, end
	if backward {
		pageStart = max(start, end-int64(pageSize))
	} else {
		pageEnd = min(end, start+int64(pageSize))
	}
	rows, err := unmatchedObservationQuery(filters,
		sm.OrderBy(models.SourceDocuments.Columns.CreatedAt).Desc(),
		sm.OrderBy(models.SourceDocuments.Columns.ID).Desc(),
		sm.OrderBy(models.SourceObservations.Columns.SourceReference),
		sm.Offset(int(pageStart)),
		sm.Limit(int(pageEnd-pageStart)),
	).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	confirmed, err := unmatchedObservationConfirmations(ctx, executor, hhID, rows)
	if err != nil {
		return nil, err
	}
	edges := make([]*model.UnmatchedObservationEdge, 0, len(rows))
	for _, observation := range rows {
		cursor := unmatchedCursor(observation.SourceDocumentID.String(), observation.SourceReference)
		kind := model.SourceKind(strings.ToUpper(observation.SourceKind))
		var occurredAt *model.DateTime
		if observation.OccurredAt.Valid {
			value := timeString(observation.OccurredAt.V)
			occurredAt = &value
		}
		fields := &model.ObservationFields{
			DocumentID: observation.SourceDocumentID.String(), SourceReference: observation.SourceReference, SourceKind: kind,
			Merchant: observation.Merchant, OccurredAt: occurredAt,
			AmountYen: graphLong(observation.AmountYen), OrderID: observation.OrderID, TransactionID: observation.TransactionID,
		}
		var node model.ObservationVariant = &model.UnmatchedObservation{ObservationFields: fields}
		if purchaseID, ok := confirmed[cursor]; ok {
			node = &model.ConfirmedObservation{ObservationFields: fields, PurchaseID: purchaseID}
		}
		edges = append(edges, &model.UnmatchedObservationEdge{Cursor: cursor, Node: node})
	}
	pageInfo := &model.PageInfo{HasPreviousPage: pageStart > 0, HasNextPage: pageEnd < totalCount}
	if len(edges) > 0 {
		startCursor, endCursor := edges[0].Cursor, edges[len(edges)-1].Cursor
		pageInfo.StartCursor = &startCursor
		pageInfo.EndCursor = &endCursor
	}
	return &model.UnmatchedObservationConnection{Edges: edges, PageInfo: pageInfo, TotalCount: int(totalCount)}, nil
}

func unmatchedObservationConfirmations(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, observations models.SourceObservationSlice) (map[string]string, error) {
	filters := make([]bob.Expression, 0, len(observations)+1)
	filters = append(filters, models.DocumentConfirmations.Columns.HouseholdID.EQ(psql.Arg(householdID)))
	for _, observation := range observations {
		filters = append(filters, psql.And(
			models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(observation.SourceDocumentID)),
			models.DocumentConfirmations.Columns.SourceReference.EQ(psql.Arg(observation.SourceReference)),
		))
	}
	confirmed := make(map[string]string, len(observations))
	if len(observations) == 0 {
		return confirmed, nil
	}
	confirmations, err := models.DocumentConfirmations.Query(sm.Where(psql.And(
		filters[0],
		psql.Or(filters[1:]...),
	))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	correctionIDs := make([]bob.Expression, 0, len(confirmations))
	for _, confirmation := range confirmations {
		if confirmation.PurchaseID.Valid {
			confirmed[unmatchedCursor(confirmation.SourceDocumentID.String(), confirmation.SourceReference)] = confirmation.PurchaseID.V.String()
		} else if confirmation.CorrectionID.Valid {
			correctionIDs = append(correctionIDs, psql.Arg(confirmation.CorrectionID.V))
		}
	}
	if len(correctionIDs) == 0 {
		return confirmed, nil
	}
	corrections, err := models.Corrections.Query(sm.Where(models.Corrections.Columns.ID.In(correctionIDs...))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	purchaseByCorrection := make(map[gofrsuuid.UUID]string, len(corrections))
	for _, correction := range corrections {
		purchaseByCorrection[correction.ID] = correction.PurchaseID.String()
	}
	for _, confirmation := range confirmations {
		if !confirmation.CorrectionID.Valid {
			continue
		}
		purchaseID, exists := purchaseByCorrection[confirmation.CorrectionID.V]
		if !exists {
			return nil, fmt.Errorf("correction confirmation has no correction")
		}
		confirmed[unmatchedCursor(confirmation.SourceDocumentID.String(), confirmation.SourceReference)] = purchaseID
	}
	return confirmed, nil
}
