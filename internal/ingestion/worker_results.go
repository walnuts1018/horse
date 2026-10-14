package ingestion

import (
	"context"
	"database/sql"
	stdjson "encoding/json"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func postgresInt32(value int, field string) (int32, error) {
	if value < math.MinInt32 || value > math.MaxInt32 {
		return 0, fmt.Errorf("%s exceeds PostgreSQL integer range", field)
	}
	return int32(value), nil
}

func (w *Worker) recordExtractionFailure(finishCtx context.Context, j job, result Verification, extractErr error) (bool, error) {

	spendCap := errors.Is(extractErr, ErrGeminiSpendCap)
	failureCount := j.FailureCount
	if !spendCap {
		failureCount++
	}
	status := extractionStatusFailed
	if spendCap || retryWorkerError(extractErr) && failureCount < 5 {
		status = extractionStatusQueued
	}
	payload, marshalErr := json.Marshal(result.Runs)
	if marshalErr != nil {
		return true, fmt.Errorf("encode failed extraction runs: %w", marshalErr)
	}
	jobUUID, parseErr := gofrsuuid.FromString(j.ID)
	if parseErr != nil {
		return true, fmt.Errorf("record extraction failure: %w", parseErr)
	}
	attempt, err := postgresInt32(j.Attempt, "extraction attempt")
	if err != nil {
		return true, err
	}
	failureCountValue, err := postgresInt32(failureCount, "extraction failure count")
	if err != nil {
		return true, err
	}
	updatedStatus := status
	code := sql.Null[string]{V: workerErrorCode(extractErr), Valid: true}
	updatedAt := time.Now().UTC()
	availableAt := updatedAt.Add(retryDelay(extractErr, j.Attempt))
	setter := models.ExtractionJobSetter{
		Status: &updatedStatus, ErrorCode: &code, FailureCount: &failureCountValue,
		AvailableAt: &availableAt, UpdatedAt: &updatedAt,
	}
	if len(result.Runs) > 0 {
		previous := make(map[string]stdjson.RawMessage)
		var newRuns []stdjson.RawMessage
		if err := stdjson.Unmarshal(payload, &newRuns); err != nil {
			return true, fmt.Errorf("decode failed extraction runs: %w", err)
		}
		if len(j.Previous) > 0 {
			if err := stdjson.Unmarshal(j.Previous, &previous); err != nil {
				previous = make(map[string]stdjson.RawMessage)
			}
		}
		var previousRuns []stdjson.RawMessage
		_ = stdjson.Unmarshal(previous["runs"], &previousRuns)
		existingRuns := make([]stdjson.RawMessage, len(previousRuns), len(previousRuns)+len(newRuns))
		copy(existingRuns, previousRuns)
		existingRuns = append(existingRuns, newRuns...)
		encodedRuns, err := stdjson.Marshal(existingRuns)
		if err != nil {
			return true, fmt.Errorf("encode failed extraction runs: %w", err)
		}
		previous["runs"] = encodedRuns
		encodedResult, err := stdjson.Marshal(previous)
		if err != nil {
			return true, fmt.Errorf("encode failed extraction result: %w", err)
		}
		resultValue := sql.Null[types.JSON[stdjson.RawMessage]]{
			V: types.JSON[stdjson.RawMessage]{Val: encodedResult}, Valid: true,
		}
		setter.Result = &resultValue
	}
	_, err = models.ExtractionJobs.Update(setter.UpdateMod(), um.Where(psql.And(
		models.ExtractionJobs.Columns.ID.EQ(psql.Arg(jobUUID)),
		models.ExtractionJobs.Columns.Status.EQ(psql.Arg("running")),
		models.ExtractionJobs.Columns.Attempt.EQ(psql.Arg(attempt)),
	))).Exec(finishCtx, bobpgx.NewPool(w.DB))
	if err != nil {
		return true, fmt.Errorf("record extraction failure: %w", err)
	}
	return true, nil
}

func (w *Worker) recordExtractionSuccess(finishCtx context.Context, j job, result Verification) (bool, error) {
	result.Runs = appendPreviousRuns(result.Runs, j.Previous)
	payload, err := json.Marshal(result)
	if err != nil {
		return false, fmt.Errorf("encode extraction result: %w", err)
	}
	finishTx, err := w.DB.Begin(finishCtx)
	if err != nil {
		return false, fmt.Errorf("begin extraction completion: %w", err)
	}
	defer func() {
		if err := finishTx.Rollback(finishCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.Error("Rollback extraction completion failed", "error", err)
		}
	}()
	jobUUID, err := gofrsuuid.FromString(j.ID)
	if err != nil {
		return false, fmt.Errorf("save extraction result: %w", err)
	}
	attempt, err := postgresInt32(j.Attempt, "extraction attempt")
	if err != nil {
		return false, err
	}
	status := extractionStatusSucceeded
	completionAt := time.Now().UTC()
	completionErrorCode := sql.Null[string]{}
	resultValue := sql.Null[types.JSON[stdjson.RawMessage]]{
		V: types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(payload)}, Valid: true,
	}
	updated, err := models.ExtractionJobs.Update((&models.ExtractionJobSetter{
		Status: &status, Result: &resultValue, ErrorCode: &completionErrorCode, UpdatedAt: &completionAt,
	}).UpdateMod(), um.Where(psql.And(
		models.ExtractionJobs.Columns.ID.EQ(psql.Arg(jobUUID)),
		models.ExtractionJobs.Columns.Status.EQ(psql.Arg("running")),
		models.ExtractionJobs.Columns.Attempt.EQ(psql.Arg(attempt)),
	))).Exec(finishCtx, bobpgx.NewTx(finishTx, nil))
	if err != nil {
		return false, fmt.Errorf("save extraction result: %w", err)
	}
	if updated == 0 {
		return false, nil
	}
	event, err := json.Marshal(map[string]any{"jobId": j.ID, "sourceDocumentId": j.SourceID, "status": extractionStatusSucceeded, "balanced": result.Balanced})
	if err != nil {
		return false, fmt.Errorf("encode extraction event: %w", err)
	}
	householdUUID, err := gofrsuuid.FromString(j.HouseholdID)
	if err != nil {
		return false, fmt.Errorf("record extraction event: %w", err)
	}
	sourceUUID, err := gofrsuuid.FromString(j.SourceID)
	if err != nil {
		return false, fmt.Errorf("record extraction event: %w", err)
	}
	if err := persistSourceObservations(finishCtx, bobpgx.NewTx(finishTx, nil), householdUUID, sourceUUID, j.SourceHash, j.SourceKind, result); err != nil {
		return false, fmt.Errorf("record source observations: %w", err)
	}
	if _, err := models.Households.Query(
		sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID))),
		sm.ForUpdate(),
	).One(finishCtx, bobpgx.NewTx(finishTx, nil)); err != nil {
		return false, fmt.Errorf("lock household event cursor: %w", err)
	}
	eventType := "receipt.extraction.updated"
	jVersion := j.Version
	changedFields := types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(`["status","result"]`)}
	eventPayload := types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(event)}
	if _, err := models.HouseholdEvents.Insert(&models.HouseholdEventSetter{
		HouseholdID: &householdUUID, Type: &eventType, EntityID: &sourceUUID, Revision: &jVersion,
		ChangedFields: &changedFields, Payload: &eventPayload,
	}).Exec(finishCtx, bobpgx.NewTx(finishTx, nil)); err != nil {
		return false, fmt.Errorf("record extraction event: %w", err)
	}
	if err := finishTx.Commit(finishCtx); err != nil {
		return false, fmt.Errorf("commit extraction result: %w", err)
	}
	return true, nil
}
