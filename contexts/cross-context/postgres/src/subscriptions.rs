use crate::purchase_command_result::PurchaseCommandResult;
use async_trait::async_trait;
use cross_context_application::subscriptions::{
    PurchaseEvent, PurchaseItemAllocationSnapshot, PurchaseItemSnapshot, PurchasePaymentSnapshot,
    PurchaseSnapshot, SubscriptionQuery,
};
use diesel::{BoolExpressionMethods, ExpressionMethods, OptionalExtension, QueryDsl};
use diesel_async::RunQueryDsl;
use std::collections::HashMap;
use uuid::Uuid;

use crate::PgPool;

const MAX_SUBSCRIPTION_BATCH_SIZE: usize = 16_000;

pub struct PostgresSubscriptionQuery {
    pool: PgPool,
}

impl PostgresSubscriptionQuery {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait]
impl SubscriptionQuery for PostgresSubscriptionQuery {
    async fn session_is_valid(&self, actor_id: Uuid, token_hash: [u8; 32]) -> Result<bool, String> {
        use horse_postgres_common::schema::public::oidc_sessions::dsl as sessions;
        let mut conn = self.pool.get().await.map_err(|error| error.to_string())?;
        let member_id = sessions::oidc_sessions
            .filter(sessions::token_hash.eq(token_hash.to_vec()))
            .filter(sessions::expiry.gt(chrono::Utc::now()))
            .select(sessions::member_id)
            .first::<Uuid>(&mut conn)
            .await
            .optional()
            .map_err(|error| error.to_string())?;
        Ok(member_id == Some(actor_id))
    }

    async fn household_access(&self, actor_id: Uuid, household_id: Uuid) -> Result<bool, String> {
        use horse_postgres_common::schema::public::household_members::dsl as members;
        let mut conn = self.pool.get().await.map_err(|error| error.to_string())?;
        members::household_members
            .filter(members::household_id.eq(household_id))
            .filter(members::member_id.eq(actor_id))
            .filter(members::left_at.is_null())
            .select(members::member_id)
            .first::<Uuid>(&mut conn)
            .await
            .optional()
            .map(|member| member.is_some())
            .map_err(|error| error.to_string())
    }

    async fn latest_purchase_cursor(&self, household_id: Uuid) -> Result<i64, String> {
        use horse_postgres_common::schema::public::household_events::dsl as events;
        let mut conn = self.pool.get().await.map_err(|error| error.to_string())?;
        events::household_events
            .filter(events::household_id.eq(household_id))
            .select(events::stream_cursor)
            .order(events::stream_cursor.desc())
            .first::<i64>(&mut conn)
            .await
            .optional()
            .map(|cursor| cursor.unwrap_or(0))
            .map_err(|error| error.to_string())
    }

    async fn purchase_events(
        &self,
        cursors: Vec<(Uuid, i64)>,
    ) -> Result<Vec<PurchaseEvent>, String> {
        use horse_postgres_common::schema::public::household_events::dsl as events;
        let mut minimum_cursors = std::collections::HashMap::new();
        for (household_id, cursor) in cursors {
            minimum_cursors
                .entry(household_id)
                .and_modify(|minimum: &mut i64| *minimum = (*minimum).min(cursor))
                .or_insert(cursor);
        }
        if minimum_cursors.is_empty() {
            return Ok(Vec::new());
        }
        let mut conn = self.pool.get().await.map_err(|error| error.to_string())?;
        let cursors: Vec<_> = minimum_cursors.into_iter().collect();
        let mut rows = Vec::new();
        for chunk in cursors.chunks(MAX_SUBSCRIPTION_BATCH_SIZE) {
            let mut query = events::household_events.into_boxed::<diesel::pg::Pg>();
            let mut conditions = chunk.iter().copied();
            if let Some((household_id, cursor)) = conditions.next() {
                query = query.filter(
                    events::household_id
                        .eq(household_id)
                        .and(events::stream_cursor.gt(cursor)),
                );
                for (household_id, cursor) in conditions {
                    query = query.or_filter(
                        events::household_id
                            .eq(household_id)
                            .and(events::stream_cursor.gt(cursor)),
                    );
                }
            }
            rows.extend(
                query
                    .order(events::cursor.asc())
                    .limit(100)
                    .select((
                        events::cursor,
                        events::stream_cursor,
                        events::household_id,
                        events::event_type,
                        events::entity_kind,
                        events::entity_id,
                        events::revision,
                        events::changed_fields,
                        events::created_at,
                        events::payload,
                    ))
                    .load::<(
                        i64,
                        i64,
                        Uuid,
                        String,
                        String,
                        Uuid,
                        i64,
                        Vec<Option<String>>,
                        chrono::DateTime<chrono::Utc>,
                        serde_json::Value,
                    )>(&mut conn)
                    .await
                    .map_err(|error| error.to_string())?,
            );
        }
        rows.sort_unstable_by_key(|row| row.0);
        rows.truncate(100);
        Ok(rows
            .into_iter()
            .map(
                |(
                    _global_cursor,
                    cursor,
                    household_id,
                    event_type,
                    entity_kind,
                    entity_id,
                    revision,
                    changed_fields,
                    created_at,
                    payload,
                )| PurchaseEvent {
                    cursor,
                    household_id,
                    event_type,
                    entity_kind,
                    entity_id,
                    revision,
                    changed_fields,
                    created_at,
                    payload,
                },
            )
            .collect())
    }

    async fn purchase_id_for_item(&self, item_id: Uuid) -> Result<Option<Uuid>, String> {
        use horse_postgres_common::schema::public::purchase_items::dsl as items;
        let mut conn = self.pool.get().await.map_err(|error| error.to_string())?;
        items::purchase_items
            .filter(items::id.eq(item_id))
            .select(items::purchase_id)
            .first::<Uuid>(&mut conn)
            .await
            .optional()
            .map_err(|error| error.to_string())
    }

    async fn purchase_ids_for_items(
        &self,
        item_ids: Vec<Uuid>,
    ) -> Result<HashMap<Uuid, Uuid>, String> {
        use horse_postgres_common::schema::public::purchase_items::dsl as items;
        let mut conn = self.pool.get().await.map_err(|error| error.to_string())?;
        let mut purchases = HashMap::new();
        for chunk in item_ids.chunks(MAX_SUBSCRIPTION_BATCH_SIZE) {
            let rows = items::purchase_items
                .filter(items::id.eq_any(chunk))
                .select((items::id, items::purchase_id))
                .load::<(Uuid, Uuid)>(&mut conn)
                .await
                .map_err(|error| error.to_string())?;
            purchases.extend(rows);
        }
        Ok(purchases)
    }

    async fn purchase_snapshot(
        &self,
        household_id: Uuid,
        purchase_id: Uuid,
    ) -> Result<Option<PurchaseSnapshot>, String> {
        use crate::purchase_command_result::load_purchase_command_result;
        match load_purchase_command_result(&self.pool, household_id, purchase_id).await {
            Ok(value) => Ok(Some(purchase_snapshot(value))),
            Err(crate::purchase_command_result::PurchaseCommandResultError::PurchaseNotFound) => {
                Ok(None)
            }
            Err(error) => Err(format!("{error:?}")),
        }
    }

    async fn purchase_snapshots(
        &self,
        purchases: Vec<(Uuid, Uuid)>,
    ) -> Result<HashMap<(Uuid, Uuid), PurchaseSnapshot>, String> {
        use crate::purchase_command_result::load_purchase_command_results;
        let households = purchases
            .iter()
            .map(|(household_id, _)| *household_id)
            .collect::<std::collections::HashSet<_>>()
            .into_iter()
            .collect::<Vec<_>>();
        let purchase_ids = purchases
            .iter()
            .map(|(_, purchase_id)| *purchase_id)
            .collect::<std::collections::HashSet<_>>()
            .into_iter()
            .collect::<Vec<_>>();
        load_purchase_command_results(&self.pool, &households, &purchase_ids)
            .await
            .map(|results| {
                results
                    .into_iter()
                    .map(|result| ((result.household_id, result.id), purchase_snapshot(result)))
                    .collect()
            })
            .map_err(|error| format!("{error:?}"))
    }

    async fn shared_draft_revision(
        &self,
        draft_id: Uuid,
        after: i64,
    ) -> Result<Option<i64>, String> {
        use horse_postgres_common::schema::public::shared_purchase_drafts::dsl as drafts;
        let mut conn = self.pool.get().await.map_err(|error| error.to_string())?;
        drafts::shared_purchase_drafts
            .filter(drafts::id.eq(draft_id))
            .filter(drafts::revision.gt(after))
            .select(drafts::revision)
            .first::<i64>(&mut conn)
            .await
            .optional()
            .map_err(|error| error.to_string())
    }

    async fn shared_draft_revisions(
        &self,
        cursors: Vec<(Uuid, i64)>,
    ) -> Result<HashMap<Uuid, i64>, String> {
        use horse_postgres_common::schema::public::shared_purchase_drafts::dsl as drafts;
        let minimum_cursors: HashMap<Uuid, i64> =
            cursors
                .into_iter()
                .fold(HashMap::new(), |mut minimums, (draft_id, cursor)| {
                    minimums
                        .entry(draft_id)
                        .and_modify(|minimum: &mut i64| *minimum = (*minimum).min(cursor))
                        .or_insert(cursor);
                    minimums
                });
        let mut conn = self.pool.get().await.map_err(|error| error.to_string())?;
        let mut revisions = HashMap::new();
        let draft_ids: Vec<_> = minimum_cursors.keys().copied().collect();
        for chunk in draft_ids.chunks(MAX_SUBSCRIPTION_BATCH_SIZE) {
            let rows = drafts::shared_purchase_drafts
                .filter(drafts::id.eq_any(chunk))
                .select((drafts::id, drafts::revision))
                .load::<(Uuid, i64)>(&mut conn)
                .await
                .map_err(|error| error.to_string())?;
            revisions.extend(rows.into_iter().filter(|(id, revision)| {
                minimum_cursors
                    .get(id)
                    .is_some_and(|cursor| revision > cursor)
            }));
        }
        Ok(revisions)
    }

    async fn shared_draft_is_accessible(
        &self,
        actor_id: Uuid,
        draft_id: Uuid,
    ) -> Result<bool, String> {
        bookkeeping_postgres::shared_draft::get_shared_purchase_draft(
            &self.pool,
            &actor_id.to_string(),
            &draft_id.to_string(),
        )
        .await
        .map(|_| true)
        .map_err(|error| format!("{error:?}"))
    }
}

fn purchase_snapshot(value: PurchaseCommandResult) -> PurchaseSnapshot {
    PurchaseSnapshot {
        id: value.id,
        household_id: value.household_id,
        occurred_at: value.occurred_at,
        date_precision: value.date_precision,
        merchant: value.merchant,
        origin: value.origin,
        status: value.status,
        revision: value.revision,
        created_at: value.created_at,
        updated_at: value.updated_at,
        has_evidence: value.has_evidence,
        source_kinds: value.source_kinds,
        total_yen: value.total_yen,
        correction_amount_yen: value.correction_amount_yen,
        correction_count: value.correction_count,
        remaining_refundable_amount_yen: value.remaining_refundable_amount_yen,
        items: value
            .items
            .into_iter()
            .map(|item| PurchaseItemSnapshot {
                id: item.id,
                name: item.name,
                amount_yen: item.amount_yen,
                category_id: item.category_id,
                product_variant_id: item.product_variant_id,
                revision: item.revision,
                quantity_numerator: item.quantity_numerator,
                quantity_denominator: item.quantity_denominator,
                quantity_unit: item.quantity_unit,
                printed_unit_price_yen: item.printed_unit_price_yen,
                printed_unit_price_quantity_numerator: item.printed_unit_price_quantity_numerator,
                printed_unit_price_quantity_denominator: item
                    .printed_unit_price_quantity_denominator,
                printed_unit_price_unit: item.printed_unit_price_unit,
                allocations: item
                    .allocations
                    .into_iter()
                    .map(|allocation| PurchaseItemAllocationSnapshot {
                        pool_id: allocation.pool_id,
                        pool_name: allocation.pool_name,
                        weight: allocation.weight,
                        amount_yen: allocation.amount_yen,
                    })
                    .collect(),
                tags: item.tags,
                remaining_refundable_amount_yen: item.remaining_refundable_amount_yen,
            })
            .collect(),
        payments: value
            .payments
            .into_iter()
            .map(|payment| PurchasePaymentSnapshot {
                account_id: payment.account_id,
                account_name: payment.account_name,
                account_kind: payment.account_kind,
                account_card_last4: payment.account_card_last4,
                account_pool_id: payment.account_pool_id,
                amount_yen: payment.amount_yen,
            })
            .collect(),
    }
}
