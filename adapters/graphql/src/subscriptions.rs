use std::{
    collections::HashMap,
    sync::{Arc, Mutex},
    time::Duration,
};

use async_graphql::{Context, Enum, Error, ID, Result, SimpleObject, Subscription};
use chrono::{DateTime as ChronoDateTime, Utc};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl};
use futures_channel::mpsc;
use futures_util::Stream;
use horse_postgres_common::PgPool;
use uuid::Uuid;

use crate::{Actor, DateTime, actor, decode_household_id, purchase_mutations};

#[derive(Clone, Copy, Debug, Enum, Eq, PartialEq)]
pub enum PurchaseChangeKind {
    Created,
    Posted,
    Updated,
    ItemUpdated,
    ItemDeleted,
    Corrected,
}

#[derive(Clone, Copy, Debug, Enum, Eq, PartialEq)]
pub enum PurchaseChangeEntity {
    Purchase,
    PurchaseItem,
}

#[derive(Clone, Copy, Debug, Enum, Eq, PartialEq)]
pub enum PurchaseChangeField {
    OccurredAt,
    Merchant,
    PaymentAllocations,
    Status,
    Correction,
    Name,
    AmountYen,
    Quantity,
    PrintedUnitPrice,
    CategoryId,
    ProductVariantId,
    Allocations,
    Tags,
    Deleted,
}

#[derive(Clone, Debug, SimpleObject)]
#[graphql(rename_fields = "camelCase")]
pub struct PurchaseChange {
    pub cursor: String,
    pub kind: PurchaseChangeKind,
    pub entity_kind: PurchaseChangeEntity,
    pub entity_id: ID,
    pub revision: i32,
    pub changed_fields: Vec<PurchaseChangeField>,
    pub occurred_at: DateTime,
    pub purchase: purchase_mutations::PurchaseObject,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct SharedPurchaseDraftChange {
    pub cursor: String,
}

#[derive(Clone)]
struct Watch<T> {
    key: Uuid,
    cursor: i64,
    tx: mpsc::Sender<T>,
}

#[derive(Default)]
pub struct SubscriptionHub {
    purchases: Mutex<HashMap<u64, Watch<PurchaseChange>>>,
    drafts: Mutex<HashMap<u64, Watch<SharedPurchaseDraftChange>>>,
    next_id: Mutex<u64>,
    started: Mutex<bool>,
}

impl SubscriptionHub {
    pub fn new() -> Arc<Self> {
        Arc::new(Self::default())
    }

    fn start(self: &Arc<Self>, pool: PgPool) {
        let Ok(mut started) = self.started.lock() else {
            return;
        };
        if *started {
            return;
        }
        *started = true;
        let hub = Arc::clone(self);
        tokio::spawn(async move {
            let mut ticker = tokio::time::interval(Duration::from_secs(1));
            loop {
                ticker.tick().await;
                hub.remove_closed_watches();
                if hub.is_idle() {
                    *hub.started.lock().unwrap_or_else(|e| e.into_inner()) = false;
                    break;
                }
                if let Err(error) = hub.poll(&pool).await {
                    tracing::warn!(?error, "GraphQL subscription poll failed");
                }
            }
        });
    }

    async fn poll(&self, pool: &PgPool) -> Result<()> {
        self.poll_purchases(pool).await?;
        self.poll_drafts(pool).await?;
        Ok(())
    }

    async fn poll_purchases(&self, pool: &PgPool) -> Result<()> {
        use diesel_async::RunQueryDsl;
        let cursors: Vec<(Uuid, i64)> = {
            let watches = self.purchases.lock().unwrap_or_else(|e| e.into_inner());
            watches
                .values()
                .map(|watch| (watch.key, watch.cursor))
                .collect()
        };
        if cursors.is_empty() {
            return Ok(());
        }
        use horse_postgres_common::schema::public::household_events::dsl as events;
        let minimum = cursors.iter().map(|(_, cursor)| *cursor).min().unwrap_or(0);
        let rows = {
            let mut conn = pool.get().await.map_err(crate::internal_error)?;
            events::household_events
                .filter(events::cursor.gt(minimum))
                .order(events::cursor.asc())
                .limit(100)
                .select((
                    events::cursor,
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
                    Uuid,
                    String,
                    String,
                    Uuid,
                    i64,
                    Vec<Option<String>>,
                    ChronoDateTime<Utc>,
                    serde_json::Value,
                )>(&mut conn)
                .await
                .map_err(crate::internal_error)?
        };
        for (
            cursor,
            household,
            event_type,
            entity_type,
            entity_id,
            revision,
            fields,
            created_at,
            payload,
        ) in rows
        {
            let targets: Vec<u64> = self
                .purchases
                .lock()
                .unwrap_or_else(|e| e.into_inner())
                .iter()
                .filter_map(|(id, watch)| {
                    (watch.key == household && watch.cursor < cursor).then_some(*id)
                })
                .collect();
            for watch in self
                .purchases
                .lock()
                .unwrap_or_else(|e| e.into_inner())
                .values_mut()
            {
                if watch.key != household && watch.cursor < cursor {
                    watch.cursor = cursor;
                }
            }
            for id in targets {
                let Some((kind, entity_kind, changed_fields)) = change_fields(
                    &event_type,
                    &entity_type,
                    fields.iter().flatten().map(String::as_str),
                ) else {
                    self.advance_purchase(id, cursor);
                    continue;
                };
                let purchase_id = if event_type == "purchase.item.deleted" {
                    payload
                        .get("purchaseId")
                        .and_then(serde_json::Value::as_str)
                        .and_then(|value| Uuid::parse_str(value).ok())
                } else if entity_type == "purchase_item" {
                    let mut conn = pool.get().await.map_err(crate::internal_error)?;
                    use horse_postgres_common::schema::public::purchase_items::dsl as items;
                    items::purchase_items
                        .filter(items::id.eq(entity_id))
                        .select(items::purchase_id)
                        .first::<Uuid>(&mut conn)
                        .await
                        .optional()
                        .map_err(crate::internal_error)?
                } else {
                    Some(entity_id)
                };
                let Some(purchase_id) = purchase_id else {
                    self.advance_purchase(id, cursor);
                    continue;
                };
                let purchase =
                    purchase_mutations::purchase_result(pool, household, &purchase_id.to_string())
                        .await?;
                let node_kind = if entity_kind == PurchaseChangeEntity::PurchaseItem {
                    "PurchaseItem"
                } else {
                    "Purchase"
                };
                let change = PurchaseChange {
                    cursor: cursor.to_string(),
                    kind,
                    entity_kind,
                    entity_id: crate::node_query::encode_global_node_id(node_kind, entity_id),
                    revision: revision as i32,
                    changed_fields,
                    occurred_at: DateTime::from(created_at),
                    purchase,
                };
                let mut watches = self.purchases.lock().unwrap_or_else(|e| e.into_inner());
                if let Some(watch) = watches.get_mut(&id) {
                    match watch.tx.try_send(change) {
                        Ok(()) => watch.cursor = cursor,
                        Err(_) => {}
                    }
                }
            }
        }
        Ok(())
    }

    fn advance_purchase(&self, id: u64, cursor: i64) {
        if let Some(watch) = self
            .purchases
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .get_mut(&id)
        {
            watch.cursor = cursor;
        }
    }

    async fn poll_drafts(&self, pool: &PgPool) -> Result<()> {
        use diesel_async::RunQueryDsl;
        let watches: Vec<(u64, Uuid, i64)> = self
            .drafts
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .iter()
            .map(|(id, watch)| (*id, watch.key, watch.cursor))
            .collect();
        if watches.is_empty() {
            return Ok(());
        }
        use horse_postgres_common::schema::public::shared_purchase_drafts::dsl as drafts;
        for (id, draft_id, cursor) in watches {
            let revision = {
                let mut conn = pool.get().await.map_err(crate::internal_error)?;
                drafts::shared_purchase_drafts
                    .filter(drafts::id.eq(draft_id))
                    .filter(drafts::revision.gt(cursor))
                    .select(drafts::revision)
                    .first::<i64>(&mut conn)
                    .await
                    .optional()
                    .map_err(crate::internal_error)?
            };
            if let Some(revision) = revision {
                let mut watches = self.drafts.lock().unwrap_or_else(|e| e.into_inner());
                if let Some(watch) = watches.get_mut(&id) {
                    if watch
                        .tx
                        .try_send(SharedPurchaseDraftChange {
                            cursor: revision.to_string(),
                        })
                        .is_ok()
                    {
                        watch.cursor = revision;
                    }
                }
            }
        }
        Ok(())
    }
}

fn change_fields<'a>(
    event: &str,
    entity: &str,
    fields: impl Iterator<Item = &'a str>,
) -> Option<(
    PurchaseChangeKind,
    PurchaseChangeEntity,
    Vec<PurchaseChangeField>,
)> {
    let kind = match event {
        "purchase.created" => PurchaseChangeKind::Created,
        "purchase.posted" => PurchaseChangeKind::Posted,
        "purchase.updated" => PurchaseChangeKind::Updated,
        "purchase.item.updated" => PurchaseChangeKind::ItemUpdated,
        "purchase.item.deleted" => PurchaseChangeKind::ItemDeleted,
        "purchase.corrected" => PurchaseChangeKind::Corrected,
        _ => return None,
    };
    let entity_kind = if entity == "purchase_item" {
        PurchaseChangeEntity::PurchaseItem
    } else {
        PurchaseChangeEntity::Purchase
    };
    let mut mapped = Vec::new();
    for field in fields {
        mapped.push(match field {
            "occurredAt" => PurchaseChangeField::OccurredAt,
            "merchant" => PurchaseChangeField::Merchant,
            "paymentAllocations" => PurchaseChangeField::PaymentAllocations,
            "status" => PurchaseChangeField::Status,
            "correction" => PurchaseChangeField::Correction,
            "name" => PurchaseChangeField::Name,
            "amountYen" => PurchaseChangeField::AmountYen,
            "quantity" => PurchaseChangeField::Quantity,
            "printedUnitPrice" => PurchaseChangeField::PrintedUnitPrice,
            "categoryId" => PurchaseChangeField::CategoryId,
            "productVariantId" => PurchaseChangeField::ProductVariantId,
            "allocations" => PurchaseChangeField::Allocations,
            "tags" => PurchaseChangeField::Tags,
            "deleted" => PurchaseChangeField::Deleted,
            _ => return None,
        });
    }
    Some((kind, entity_kind, mapped))
}

#[derive(Default)]
pub struct SubscriptionRoot;

#[Subscription]
impl SubscriptionRoot {
    async fn purchase_changes(
        &self,
        ctx: &Context<'_>,
        household_id: ID,
        after: Option<String>,
    ) -> Result<impl Stream<Item = PurchaseChange>> {
        let pool = ctx.data::<PgPool>()?.clone();
        let actor = actor(ctx)?;
        let household = decode_household_id(&household_id)?;
        authorize_household(&pool, actor, household).await?;
        let cursor = match after {
            Some(value) if !value.is_empty() => parse_cursor(&value)?,
            _ => latest_cursor(&pool, household).await?,
        };
        let (tx, rx) = mpsc::channel(16);
        let hub = ctx.data::<Arc<SubscriptionHub>>()?;
        let id = hub.register_purchase(household, cursor, tx);
        hub.start(pool.clone());
        Ok(futures_util::stream::unfold(
            (rx, pool, actor, household, id, hub.clone()),
            |(mut rx, pool, actor, household, id, hub)| async move {
                loop {
                    tokio::select! {
                        value = futures_util::StreamExt::next(&mut rx) => return match value { Some(value) => Some((value, (rx, pool, actor, household, id, hub))), None => None },
                        _ = tokio::time::sleep(Duration::from_secs(30)) => if authorize_household(&pool, actor, household).await.is_err() { hub.remove_purchase(id); return None; },
                    }
                }
            },
        ))
    }

    async fn shared_purchase_draft_changes(
        &self,
        ctx: &Context<'_>,
        id: ID,
        after: Option<String>,
    ) -> Result<impl Stream<Item = SharedPurchaseDraftChange>> {
        let pool = ctx.data::<PgPool>()?.clone();
        let actor = actor(ctx)?;
        let draft_id = Uuid::parse_str(id.as_str()).map_err(crate::internal_error)?;
        let draft = crate::shared_purchase_draft::shared_purchase_draft(ctx, id.clone())
            .await?
            .ok_or_else(|| Error::new("shared purchase draft not found"))?;
        let cursor = after
            .map(|value| parse_cursor(&value))
            .transpose()?
            .unwrap_or(draft.revision as i64);
        let (tx, rx) = mpsc::channel(1);
        let hub = ctx.data::<Arc<SubscriptionHub>>()?;
        let watch_id = hub.register_draft(draft_id, cursor, tx);
        hub.start(pool.clone());
        Ok(futures_util::stream::unfold(
            (rx, pool, actor, draft_id, watch_id, hub.clone()),
            |(mut rx, pool, actor, draft_id, watch_id, hub)| async move {
                loop {
                    tokio::select! {
                        value = futures_util::StreamExt::next(&mut rx) => return match value { Some(value) => Some((value, (rx, pool, actor, draft_id, watch_id, hub))), None => None },
                        _ = tokio::time::sleep(Duration::from_secs(30)) => if authorize_draft(&pool, actor, draft_id).await.is_err() { hub.remove_draft(watch_id); return None; },
                    }
                }
            },
        ))
    }
}

async fn authorize_household(pool: &PgPool, actor: Actor, household: Uuid) -> Result<()> {
    use diesel_async::RunQueryDsl;
    use horse_postgres_common::schema::public::household_members::dsl as members;
    let mut conn = pool.get().await.map_err(crate::internal_error)?;
    let found = members::household_members
        .filter(members::household_id.eq(household))
        .filter(members::member_id.eq(actor.0))
        .filter(members::left_at.is_null())
        .select(members::member_id)
        .first::<Uuid>(&mut conn)
        .await
        .optional()
        .map_err(crate::internal_error)?;
    found
        .map(|_| ())
        .ok_or_else(|| Error::new("household access denied"))
}

async fn authorize_draft(pool: &PgPool, actor: Actor, id: Uuid) -> Result<()> {
    let draft = bookkeeping_postgres::shared_draft::get_shared_purchase_draft(
        pool,
        &actor.0.to_string(),
        &id.to_string(),
    )
    .await
    .map_err(crate::internal_error)?;
    let _ = draft;
    Ok(())
}

async fn latest_cursor(pool: &PgPool, household: Uuid) -> Result<i64> {
    use diesel_async::RunQueryDsl;
    use horse_postgres_common::schema::public::household_events::dsl as events;
    let mut conn = pool.get().await.map_err(crate::internal_error)?;
    events::household_events
        .filter(events::household_id.eq(household))
        .select(events::cursor)
        .order(events::cursor.desc())
        .first::<i64>(&mut conn)
        .await
        .optional()
        .map_err(crate::internal_error)
        .map(|cursor| cursor.unwrap_or(0))
}

fn parse_cursor(value: &str) -> Result<i64> {
    value
        .parse::<i64>()
        .ok()
        .filter(|cursor| *cursor >= 0)
        .ok_or_else(|| Error::new("invalid subscription cursor"))
}

impl SubscriptionHub {
    fn remove_closed_watches(&self) {
        self.purchases
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .retain(|_, watch| !watch.tx.is_closed());
        self.drafts
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .retain(|_, watch| !watch.tx.is_closed());
    }
    fn is_idle(&self) -> bool {
        self.purchases
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .is_empty()
            && self
                .drafts
                .lock()
                .unwrap_or_else(|e| e.into_inner())
                .is_empty()
    }
    fn register_purchase(&self, key: Uuid, cursor: i64, tx: mpsc::Sender<PurchaseChange>) -> u64 {
        let mut next = self.next_id.lock().unwrap_or_else(|e| e.into_inner());
        *next += 1;
        let id = *next;
        self.purchases
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .insert(id, Watch { key, cursor, tx });
        id
    }
    fn register_draft(
        &self,
        key: Uuid,
        cursor: i64,
        tx: mpsc::Sender<SharedPurchaseDraftChange>,
    ) -> u64 {
        let mut next = self.next_id.lock().unwrap_or_else(|e| e.into_inner());
        *next += 1;
        let id = *next;
        self.drafts
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .insert(id, Watch { key, cursor, tx });
        id
    }
    fn remove_purchase(&self, id: u64) {
        self.purchases
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .remove(&id);
    }
    fn remove_draft(&self, id: u64) {
        self.drafts
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .remove(&id);
    }
}
