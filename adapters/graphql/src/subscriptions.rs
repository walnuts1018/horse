use std::{
    collections::HashMap,
    sync::{Arc, Mutex},
    time::Duration,
};

use crate::{Actor, DateTime, actor, decode_household_id, purchase_mutations};
use async_graphql::{Context, Enum, Error, ID, Result, SimpleObject, Subscription};
use cross_context_application::subscriptions::{PurchaseEvent, SubscriptionQuery};
use futures_channel::mpsc;
use futures_util::Stream;
use uuid::Uuid;

#[derive(Clone, Copy, Debug)]
pub struct SessionTokenHash(pub [u8; 32]);

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

    fn start(self: &Arc<Self>, query: Arc<dyn SubscriptionQuery>) {
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
                if let Err(error) = hub.poll(query.as_ref()).await {
                    tracing::warn!(?error, "GraphQL subscription poll failed");
                }
            }
        });
    }

    async fn poll(&self, query: &dyn SubscriptionQuery) -> Result<()> {
        self.poll_purchases(query).await?;
        self.poll_drafts(query).await?;
        Ok(())
    }

    async fn poll_purchases(&self, query: &dyn SubscriptionQuery) -> Result<()> {
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
        let mut minimum_cursors = HashMap::new();
        for (household, cursor) in cursors {
            minimum_cursors
                .entry(household)
                .and_modify(|minimum: &mut i64| *minimum = (*minimum).min(cursor))
                .or_insert(cursor);
        }
        let rows = query
            .purchase_events(minimum_cursors.into_iter().collect())
            .await
            .map_err(crate::internal_error)?;
        for row in rows {
            let PurchaseEvent {
                cursor,
                household_id: household,
                event_type,
                entity_kind: entity_type,
                entity_id,
                revision,
                changed_fields: fields,
                created_at,
                payload,
            } = row;
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
                    query
                        .purchase_id_for_item(entity_id)
                        .await
                        .map_err(crate::internal_error)?
                } else {
                    Some(entity_id)
                };
                let Some(purchase_id) = purchase_id else {
                    self.advance_purchase(id, cursor);
                    continue;
                };
                let Some(projection) = query
                    .purchase_snapshot(household, purchase_id)
                    .await
                    .map_err(crate::internal_error)?
                else {
                    self.advance_purchase(id, cursor);
                    continue;
                };
                let purchase = purchase_mutations::purchase_projection(projection)
                    .map_err(crate::internal_error)?;
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
                if let Some(watch) = watches.get_mut(&id)
                    && watch.tx.try_send(change).is_ok()
                {
                    watch.cursor = cursor;
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

    async fn poll_drafts(&self, query: &dyn SubscriptionQuery) -> Result<()> {
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
        for (id, draft_id, cursor) in watches {
            let revision = query
                .shared_draft_revision(draft_id, cursor)
                .await
                .map_err(crate::internal_error)?;
            if let Some(revision) = revision {
                let mut watches = self.drafts.lock().unwrap_or_else(|e| e.into_inner());
                if let Some(watch) = watches.get_mut(&id)
                    && watch
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
        let query = Arc::clone(ctx.data::<Arc<dyn SubscriptionQuery>>()?);
        let actor = actor(ctx)?;
        let session_token_hash = ctx.data::<SessionTokenHash>()?.0;
        let household = decode_household_id(&household_id)?;
        authorize_session(query.as_ref(), actor, session_token_hash).await?;
        authorize_household(query.as_ref(), actor, household).await?;
        let cursor = match after {
            Some(value) if !value.is_empty() => parse_cursor(&value)?,
            _ => query
                .latest_purchase_cursor(household)
                .await
                .map_err(crate::internal_error)?,
        };
        let (tx, rx) = mpsc::channel(16);
        let hub = ctx.data::<Arc<SubscriptionHub>>()?;
        let id = hub.register_purchase(household, cursor, tx);
        hub.start(Arc::clone(&query));
        let mut authorization_interval = tokio::time::interval(Duration::from_secs(30));
        authorization_interval.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);
        authorization_interval.tick().await;
        Ok(futures_util::stream::unfold(
            (
                rx,
                query,
                actor,
                household,
                id,
                hub.clone(),
                session_token_hash,
                authorization_interval,
            ),
            |(
                mut rx,
                query,
                actor,
                household,
                id,
                hub,
                session_token_hash,
                mut authorization_interval,
            )| async move {
                loop {
                    tokio::select! {
                        value = futures_util::StreamExt::next(&mut rx) => match value {
                            Some(value) => {
                                if authorize_subscription(query.as_ref(), actor, session_token_hash, household).await.is_err() {
                                    hub.remove_purchase(id);
                                    return None;
                                }
                                return Some((value, (rx, query, actor, household, id, hub, session_token_hash, authorization_interval)));
                            }
                            None => {
                                hub.remove_purchase(id);
                                return None;
                            }
                        },
                        _ = authorization_interval.tick() => if authorize_subscription(query.as_ref(), actor, session_token_hash, household).await.is_err() { hub.remove_purchase(id); return None; },
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
        let query = Arc::clone(ctx.data::<Arc<dyn SubscriptionQuery>>()?);
        let actor = actor(ctx)?;
        let session_token_hash = ctx.data::<SessionTokenHash>()?.0;
        let draft_id = Uuid::parse_str(id.as_str()).map_err(crate::internal_error)?;
        authorize_session(query.as_ref(), actor, session_token_hash).await?;
        let cursor = query
            .shared_draft_revision(draft_id, -1)
            .await
            .map_err(crate::internal_error)?
            .ok_or_else(|| Error::new("shared purchase draft not found"))?;
        authorize_draft(query.as_ref(), actor, draft_id).await?;
        let cursor = after
            .map(|value| parse_cursor(&value))
            .transpose()?
            .unwrap_or(cursor);
        let (tx, rx) = mpsc::channel(1);
        let hub = ctx.data::<Arc<SubscriptionHub>>()?;
        let watch_id = hub.register_draft(draft_id, cursor, tx);
        hub.start(Arc::clone(&query));
        let mut authorization_interval = tokio::time::interval(Duration::from_secs(30));
        authorization_interval.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);
        authorization_interval.tick().await;
        Ok(futures_util::stream::unfold(
            (
                rx,
                query,
                actor,
                draft_id,
                watch_id,
                hub.clone(),
                session_token_hash,
                authorization_interval,
            ),
            |(
                mut rx,
                query,
                actor,
                draft_id,
                watch_id,
                hub,
                session_token_hash,
                mut authorization_interval,
            )| async move {
                loop {
                    tokio::select! {
                        value = futures_util::StreamExt::next(&mut rx) => match value {
                            Some(value) => {
                                if authorize_session(query.as_ref(), actor, session_token_hash).await.is_err()
                                    || authorize_draft(query.as_ref(), actor, draft_id).await.is_err()
                                {
                                    hub.remove_draft(watch_id);
                                    return None;
                                }
                                return Some((value, (rx, query, actor, draft_id, watch_id, hub, session_token_hash, authorization_interval)));
                            }
                            None => {
                                hub.remove_draft(watch_id);
                                return None;
                            }
                        },
                        _ = authorization_interval.tick() => if authorize_session(query.as_ref(), actor, session_token_hash).await.is_err() || authorize_draft(query.as_ref(), actor, draft_id).await.is_err() { hub.remove_draft(watch_id); return None; },
                    }
                }
            },
        ))
    }
}

async fn authorize_session(
    query: &dyn SubscriptionQuery,
    actor: Actor,
    token_hash: [u8; 32],
) -> Result<()> {
    if query
        .session_is_valid(actor.0, token_hash)
        .await
        .map_err(crate::internal_error)?
    {
        Ok(())
    } else {
        Err(Error::new("authentication required"))
    }
}

async fn authorize_subscription(
    query: &dyn SubscriptionQuery,
    actor: Actor,
    token_hash: [u8; 32],
    household: Uuid,
) -> Result<()> {
    authorize_session(query, actor, token_hash).await?;
    authorize_household(query, actor, household).await
}

async fn authorize_household(
    query: &dyn SubscriptionQuery,
    actor: Actor,
    household: Uuid,
) -> Result<()> {
    query
        .household_access(actor.0, household)
        .await
        .map_err(crate::internal_error)?
        .then_some(())
        .ok_or_else(|| Error::new("household access denied"))
}

async fn authorize_draft(query: &dyn SubscriptionQuery, actor: Actor, id: Uuid) -> Result<()> {
    query
        .shared_draft_is_accessible(actor.0, id)
        .await
        .map_err(crate::internal_error)?
        .then_some(())
        .ok_or_else(|| Error::new("shared purchase draft not found"))
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
