package graph

import (
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/ingestion"
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

func (r *queryResolver) unmatchedObservations(ctx context.Context, householdID string, from, to *string, sourceKind *model.SourceKind, state *model.UnmatchedObservationState, after *string, first *int) (*model.UnmatchedObservationConnection, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	pageSize := 50
	if first != nil {
		pageSize = *first
	}
	if pageSize < 1 || pageSize > 100 {
		return nil, fmt.Errorf("%w: first must be between 1 and 100", ErrBadInput)
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
	documents, err := models.SourceDocuments.Query(
		sm.Where(models.SourceDocuments.Columns.HouseholdID.EQ(psql.Arg(hhID))),
		sm.OrderBy(models.SourceDocuments.Columns.CreatedAt).Desc(),
		sm.OrderBy(models.SourceDocuments.Columns.ID).Desc(),
	).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	confirmations, err := models.DocumentConfirmations.Query(sm.Where(models.DocumentConfirmations.Columns.HouseholdID.EQ(psql.Arg(hhID)))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	confirmed := make(map[string]string, len(confirmations))
	for _, confirmation := range confirmations {
		confirmed[unmatchedCursor(confirmation.SourceDocumentID.String(), confirmation.SourceReference)] = confirmation.PurchaseID.String()
	}
	jobs, err := models.ExtractionJobs.Query(
		sm.Where(psql.And(models.ExtractionJobs.Columns.HouseholdID.EQ(psql.Arg(hhID)), models.ExtractionJobs.Columns.Status.EQ(psql.Arg("succeeded")))),
		sm.OrderBy(models.ExtractionJobs.Columns.SourceDocumentID),
		sm.OrderBy(models.ExtractionJobs.Columns.Version).Desc(),
	).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	latestJobs := make(map[gofrsuuid.UUID]*models.ExtractionJob, len(documents))
	for _, job := range jobs {
		if _, exists := latestJobs[job.SourceDocumentID]; !exists {
			latestJobs[job.SourceDocumentID] = job
		}
	}
	items := make([]*model.UnmatchedObservation, 0)
	cursors := make([]string, 0)
	for _, document := range documents {
		job, exists := latestJobs[document.ID]
		if !exists || !job.Result.Valid {
			continue
		}
		var verification ingestion.Verification
		if err := json.Unmarshal(job.Result.V.Val, &verification); err != nil {
			return nil, fmt.Errorf("decode extraction result: %w", err)
		}
		observations := ingestion.SourceObservations(document.ID.String(), document.Sha256, document.SourceKind, verification)
		for _, observation := range observations {
			cursor := unmatchedCursor(document.ID.String(), observation.SourceReference)
			purchaseID, isConfirmed := confirmed[cursor]
			observationState := model.UnmatchedObservationStateUnmatched
			if isConfirmed {
				observationState = model.UnmatchedObservationStateConfirmed
			}
			if state != nil && *state != observationState {
				continue
			}
			kind := model.SourceKind(strings.ToUpper(document.SourceKind))
			if sourceKind != nil && *sourceKind != kind {
				continue
			}
			if !fromMonth.IsZero() || !toMonth.IsZero() {
				if observation.OccurredAt.IsZero() || !fromMonth.IsZero() && observation.OccurredAt.Before(fromMonth) || !toMonth.IsZero() && !observation.OccurredAt.Before(toMonth) {
					continue
				}
			}
			var occurredAt *string
			if !observation.OccurredAt.IsZero() {
				value := timeString(observation.OccurredAt)
				occurredAt = &value
			}
			var confirmedPurchaseID *string
			if isConfirmed {
				confirmedPurchaseID = &purchaseID
			}
			items = append(items, &model.UnmatchedObservation{
				DocumentID: document.ID.String(), SourceReference: observation.SourceReference, PurchaseID: confirmedPurchaseID, SourceKind: kind,
				State: observationState, Merchant: observation.Merchant, OccurredAt: occurredAt,
				AmountYen: graphLong(observation.Amount), OrderID: observation.OrderID, TransactionID: observation.TransactionID,
			})
			cursors = append(cursors, cursor)
		}
	}
	totalCount := len(items)
	start := 0
	if after != nil && *after != "" {
		parsedCursor, err := parseUnmatchedCursor(*after)
		if err != nil {
			return nil, err
		}
		cursor := unmatchedCursor(parsedCursor.DocumentID, parsedCursor.SourceReference)
		index := slices.Index(cursors, cursor)
		if index < 0 {
			return nil, ErrNotFound
		}
		start = index + 1
	}
	if start > len(items) {
		start = len(items)
	}
	end := min(start+pageSize, len(items))
	edges := make([]*model.UnmatchedObservationEdge, 0, end-start)
	for index := start; index < end; index++ {
		edges = append(edges, &model.UnmatchedObservationEdge{Cursor: cursors[index], Node: items[index]})
	}
	pageInfo := &model.PageInfo{HasNextPage: end < len(items)}
	if end > start {
		endCursor := cursors[end-1]
		pageInfo.EndCursor = &endCursor
	}
	return &model.UnmatchedObservationConnection{Edges: edges, PageInfo: pageInfo, TotalCount: totalCount}, nil
}
