use diesel::{
    BoolExpressionMethods, ExpressionMethods, JoinOnDsl, NullableExpressionMethods, QueryDsl,
};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use bookkeeping_application::ledger_entry_batch_query::{
    LedgerEntryBatchQuery as LedgerEntryBatchQueryPort, LedgerEntryBatchQueryError,
    LedgerEntryProjection,
};

use crate::{PgPool, schema::public};

pub struct PostgresLedgerEntryBatchQuery {
    pool: PgPool,
}

impl PostgresLedgerEntryBatchQuery {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl LedgerEntryBatchQueryPort for PostgresLedgerEntryBatchQuery {
    fn query<'a>(
        &'a self,
        household_ids: &'a [Uuid],
        ids: &'a [Uuid],
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<
                    Output = Result<Vec<LedgerEntryProjection>, LedgerEntryBatchQueryError>,
                > + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            if household_ids.is_empty() || ids.is_empty() {
                return Ok(Vec::new());
            }

            let mut conn =
                self.pool.get().await.map_err(|error| {
                    LedgerEntryBatchQueryError::Infrastructure(error.to_string())
                })?;
            use public::{ledger_entries as ledgers, purchase_corrections as corrections};
            let rows = ledgers::table
                .left_join(
                    corrections::table.on(corrections::id
                        .eq(ledgers::source_id)
                        .and(corrections::household_id.eq(ledgers::household_id))
                        .and(ledgers::source_kind.eq_any(["CORRECTION", "correction"]))),
                )
                .filter(ledgers::household_id.eq_any(household_ids))
                .filter(ledgers::id.eq_any(ids))
                .select((
                    ledgers::id,
                    ledgers::household_id,
                    ledgers::source_kind,
                    ledgers::source_id,
                    corrections::purchase_id.nullable(),
                    ledgers::occurred_at,
                    ledgers::description,
                    ledgers::amount_yen,
                    ledgers::created_at,
                ))
                .load::<(
                    Uuid,
                    Uuid,
                    String,
                    Uuid,
                    Option<Uuid>,
                    chrono::DateTime<chrono::Utc>,
                    String,
                    i64,
                    chrono::DateTime<chrono::Utc>,
                )>(&mut conn)
                .await
                .map_err(|error| {
                    LedgerEntryBatchQueryError::Infrastructure(format!("{error:?}"))
                })?;

            Ok(rows
                .into_iter()
                .map(
                    |(
                        id,
                        household_id,
                        source_kind,
                        source_id,
                        correction_purchase_id,
                        occurred_at,
                        description,
                        amount_yen,
                        created_at,
                    )| LedgerEntryProjection {
                        id,
                        household_id,
                        source_kind,
                        source_id,
                        correction_purchase_id,
                        occurred_at,
                        description,
                        amount_yen,
                        created_at,
                    },
                )
                .collect())
        })
    }
}
