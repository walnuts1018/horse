package ingestion

import (
	"context"
	"database/sql"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dialect"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func persistSourceObservations(ctx context.Context, executor bob.Executor, householdID, documentID gofrsuuid.UUID, hash, sourceKind string, result Verification) error {
	observations := SourceObservations(documentID.String(), hash, sourceKind, result)
	if _, err := models.SourceObservations.Delete(dm.Where(psql.And(
		models.SourceObservations.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.SourceObservations.Columns.SourceDocumentID.EQ(psql.Arg(documentID)),
	))).Exec(ctx, executor); err != nil {
		return err
	}
	if len(observations) == 0 {
		return nil
	}
	values := make([]bob.Mod[*dialect.InsertQuery], 0, len(observations))
	for _, observation := range observations {
		var occurredAt sql.Null[time.Time]
		if !observation.OccurredAt.IsZero() {
			occurredAt = sql.Null[time.Time]{V: observation.OccurredAt, Valid: true}
		}
		values = append(values, im.Values(psql.Arg(
			householdID, documentID, observation.SourceReference, observation.Hash, observation.SourceKind,
			observation.TransactionKind, observation.OrderID, observation.TransactionID, observation.Merchant,
			occurredAt, observation.Amount, observation.CardLast4,
		)))
	}
	mods := []bob.Mod[*dialect.InsertQuery]{im.Into(models.SourceObservations.NameAsExpr(),
		"household_id", "source_document_id", "source_reference", "source_hash", "source_kind",
		"transaction_kind", "order_id", "transaction_id", "merchant", "occurred_at", "amount_yen", "card_last4",
	)}
	mods = append(mods, values...)
	_, err := psql.Insert(mods...).Exec(ctx, executor)
	return err
}
