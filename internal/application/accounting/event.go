package accounting

import (
	"context"
	"database/sql"
	stdjson "encoding/json"
	"encoding/json/v2"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func RecordEvent(ctx context.Context, executor bob.Executor, householdID, entityID gofrsuuid.UUID, eventType string, revision int64, fields []string, actorID gofrsuuid.UUID) error {
	if err := LockHouseholdEventCursor(ctx, executor, householdID); err != nil {
		return err
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	changedFields := types.JSON[stdjson.RawMessage]{Val: encoded}
	actor := sql.Null[gofrsuuid.UUID]{V: actorID, Valid: true}
	_, err = models.HouseholdEvents.Insert(&models.HouseholdEventSetter{
		HouseholdID: &householdID, Type: &eventType, EntityID: &entityID, Revision: &revision,
		ChangedFields: &changedFields, ActorID: &actor,
	}).Exec(ctx, executor)

	return err
}

func LockHouseholdEventCursor(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID) error {
	_, err := models.Households.Query(
		sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))),
		sm.ForUpdate(),
	).One(ctx, executor)

	return err
}
