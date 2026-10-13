package graph

import (
	"context"
	"crypto/sha256"
	"database/sql"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const maxMoney int64 = 1_000_000_000_000

func mutationRequestFingerprint(input any) ([sha256.Size]byte, error) {
	encoded, err := stdjson.Marshal(input)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}

func validateIdempotencyFingerprint(existing *models.IdempotencyKey, fingerprint [sha256.Size]byte) error {
	if !existing.RequestFingerprint.Valid {
		return fmt.Errorf("%w: idempotency key has no request fingerprint", ErrConflict)
	}
	if !slices.Equal(existing.RequestFingerprint.V, fingerprint[:]) {
		return fmt.Errorf("%w: idempotency key was used with a different request", ErrConflict)
	}
	return nil
}

func writeEvent(ctx context.Context, tx pgx.Tx, householdID, eventType, entityID string, revision int64, fields []string, actorID string) error {
	encoded, err := stdjson.Marshal(fields)
	if err != nil {
		return err
	}
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return fmt.Errorf("parse event household id: %w", err)
	}
	entityUUID, err := gofrsuuid.FromString(entityID)
	if err != nil {
		return fmt.Errorf("parse event entity id: %w", err)
	}
	actorUUID, err := gofrsuuid.FromString(actorID)
	if err != nil {
		return fmt.Errorf("parse event actor id: %w", err)
	}
	changedFields := types.JSON[stdjson.RawMessage]{Val: encoded}
	actor := sql.Null[gofrsuuid.UUID]{V: actorUUID, Valid: true}
	_, err = models.HouseholdEvents.Insert(&models.HouseholdEventSetter{
		HouseholdID: &householdUUID, Type: &eventType, EntityID: &entityUUID, Revision: &revision,
		ChangedFields: &changedFields, ActorID: &actor,
	}).Exec(ctx, bobpgx.NewTx(tx, nil))
	return err
}

func generatedUUID(value string) gofrsuuid.UUID {
	id, _ := gofrsuuid.FromString(value)
	return id
}

func parseDate[T ~string](value T) (time.Time, error) {
	parsed, err := time.Parse(time.DateOnly, string(value))
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: date must use YYYY-MM-DD", ErrBadInput)
	}
	return parsed, nil
}

func verifyHouseholdMember(ctx context.Context, db *pgxpool.Pool, householdID, memberID string) error {
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return fmt.Errorf("%w: invalid household id", ErrBadInput)
	}
	memberUUID, err := gofrsuuid.FromString(memberID)
	if err != nil {
		return fmt.Errorf("%w: invalid member id", ErrBadInput)
	}
	_, err = models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberUUID)),
	))).One(ctx, bobpgx.NewPool(db))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return nil
}
