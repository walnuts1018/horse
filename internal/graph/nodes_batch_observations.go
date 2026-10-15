package graph

import (
	"context"
	"fmt"
	"strings"

	"github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type observationNodeKey struct {
	documentID uuid.UUID
	reference  string
}

func (r *queryResolver) loadUnmatchedObservationNodes(ctx context.Context, ids []string, households []*model.Household) (map[int]model.Node, error) {
	result := make(map[int]model.Node)
	keys := make([]observationNodeKey, 0)
	indexesByKey := make(map[observationNodeKey][]int)
	for index, id := range ids {
		kind, rawID, err := decodeGlobalNodeID(id)
		if err != nil {
			return nil, err
		}
		if kind != nodeTypeUnmatchedObservation {
			continue
		}
		documentID, reference, err := decodeUnmatchedNodeID(rawID)
		if err != nil {
			return nil, err
		}
		parsedDocumentID, err := uuid.FromString(documentID)
		if err != nil {
			return nil, fmt.Errorf("invalid UnmatchedObservation document ID: %w", err)
		}
		key := observationNodeKey{documentID: parsedDocumentID, reference: reference}
		if _, exists := indexesByKey[key]; !exists {
			keys = append(keys, key)
		}
		indexesByKey[key] = append(indexesByKey[key], index)
	}
	if len(keys) == 0 {
		return result, nil
	}
	if len(households) == 0 {
		return result, nil
	}
	householdIDs := make([]bob.Expression, 0, len(households))
	for _, household := range households {
		householdIDs = append(householdIDs, psql.Arg(mustUUID(household.ID)))
	}
	executor := queryExecutor(r.DB)
	observationFilters := make([]bob.Expression, 0, len(keys))
	for _, key := range keys {
		observationFilters = append(observationFilters, psql.And(
			models.SourceObservations.Columns.SourceDocumentID.EQ(psql.Arg(key.documentID)),
			models.SourceObservations.Columns.SourceReference.EQ(psql.Arg(key.reference)),
		))
	}
	observations, err := models.SourceObservations.Query(sm.Where(psql.And(
		models.SourceObservations.Columns.HouseholdID.In(householdIDs...),
		psql.Or(observationFilters...),
	))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	observationByKey := make(map[observationNodeKey]*models.SourceObservation, len(observations))
	for _, observation := range observations {
		key := observationNodeKey{documentID: observation.SourceDocumentID, reference: observation.SourceReference}
		observationByKey[key] = observation
	}
	if len(observationByKey) == 0 {
		return result, nil
	}
	confirmationFilters := make([]bob.Expression, 0, len(observationByKey))
	for key := range observationByKey {
		confirmationFilters = append(confirmationFilters, psql.And(
			models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(key.documentID)),
			models.DocumentConfirmations.Columns.SourceReference.EQ(psql.Arg(key.reference)),
		))
	}
	confirmations, err := models.DocumentConfirmations.Query(sm.Where(psql.And(
		models.DocumentConfirmations.Columns.HouseholdID.In(householdIDs...),
		psql.Or(confirmationFilters...),
	))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	confirmationByKey := make(map[observationNodeKey]*models.DocumentConfirmation, len(confirmations))
	correctionIDs := make([]bob.Expression, 0)
	for _, confirmation := range confirmations {
		key := observationNodeKey{documentID: confirmation.SourceDocumentID, reference: confirmation.SourceReference}
		if _, exists := observationByKey[key]; !exists {
			continue
		}
		confirmationByKey[key] = confirmation
		if confirmation.CorrectionID.Valid {
			correctionIDs = append(correctionIDs, psql.Arg(confirmation.CorrectionID.V))
		}
	}
	correctionByID := make(map[uuid.UUID]*models.Correction)
	if len(correctionIDs) > 0 {
		corrections, err := models.Corrections.Query(sm.Where(psql.And(
			models.Corrections.Columns.HouseholdID.In(householdIDs...), models.Corrections.Columns.ID.In(correctionIDs...),
		))).All(ctx, executor)
		if err != nil {
			return nil, err
		}
		for _, correction := range corrections {
			correctionByID[correction.ID] = correction
		}
	}
	for _, key := range keys {
		observation := observationByKey[key]
		if observation == nil {
			continue
		}
		fields := observationNodeFields(observation)
		node := model.Node(&model.UnmatchedObservation{ObservationFields: fields})
		confirmation := confirmationByKey[key]
		if confirmation != nil {
			purchaseID := ""
			if confirmation.PurchaseID.Valid {
				purchaseID = confirmation.PurchaseID.V.String()
			} else if confirmation.CorrectionID.Valid {
				correction := correctionByID[confirmation.CorrectionID.V]
				if correction == nil {
					continue
				}
				purchaseID = correction.PurchaseID.String()
			}
			if purchaseID != "" {
				node = &model.ConfirmedObservation{ObservationFields: fields, PurchaseID: purchaseID}
			}
		}
		for _, index := range indexesByKey[key] {
			result[index] = node
		}
	}
	return result, nil
}

func observationNodeFields(observation *models.SourceObservation) *model.ObservationFields {
	fields := &model.ObservationFields{
		DocumentID: observation.SourceDocumentID.String(), SourceReference: observation.SourceReference,
		SourceKind: model.SourceKind(strings.ToUpper(observation.SourceKind)), Merchant: observation.Merchant,
		AmountYen: graphLong(observation.AmountYen), OrderID: observation.OrderID, TransactionID: observation.TransactionID,
	}
	if observation.OccurredAt.Valid {
		occurredAt := timeString(observation.OccurredAt.V)
		fields.OccurredAt = &occurredAt
	}
	return fields
}
