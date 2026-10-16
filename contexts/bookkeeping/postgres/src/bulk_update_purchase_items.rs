use std::collections::{BTreeMap, BTreeSet};

use bookkeeping_application::{
    MemberFundingBasis, MemberId, MemberWeight, PostingTarget, SettlementPolicy,
};
use bookkeeping_domain::purchase_commands::{
    BulkUpdateItemPatch, BulkUpdateItemState, BulkUpdateItemTarget, BulkUpdateItemsCommand,
    BulkUpdateItemsError, BulkUpdatePurchaseState, ItemAllocationWeight, decide_bulk_update_items,
};
use chrono::{DateTime, Datelike, FixedOffset, NaiveDate, Utc};
use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use horse_authorization::{Operation, authorize};
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug, Clone)]
pub struct PersistBulkUpdatePurchaseItems {
    pub household_id: String,
    pub actor_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PersistedBulkUpdatePurchaseItems {
    pub items: Vec<(String, String)>,
}

#[derive(Debug)]
pub enum BulkUpdatePurchaseItemsPersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    NumericOutOfRange(&'static str),
    ItemNotFound,
    PurchaseNotFound,
    HouseholdMismatch,
    MemberNotFound,
    AccountingPeriodClosed,
    ReferenceNotFound(&'static str),
    InvalidDecision,
    InvalidWorkflow(BulkUpdateItemsError),
}

impl From<DieselError> for BulkUpdatePurchaseItemsPersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_bulk_update_purchase_items<
    C: crate::update_purchase_item::UpdatePurchaseItemReferenceData,
>(
    pool: &PgPool,
    input: PersistBulkUpdatePurchaseItems,
    targets: Vec<BulkUpdateItemTarget>,
    patch: BulkUpdateItemPatch,
    reference_data: &mut C,
) -> Result<PersistedBulkUpdatePurchaseItems, BulkUpdatePurchaseItemsPersistenceError> {
    let household_id = parse_uuid(&input.household_id, "household_id")?;
    let actor_id = parse_uuid(&input.actor_id, "actor_id")?;
    let target_ids = targets
        .iter()
        .map(|target| parse_uuid(&target.item_id, "item_id"))
        .collect::<Result<Vec<_>, _>>()?;
    if target_ids.iter().copied().collect::<BTreeSet<_>>().len() != target_ids.len() {
        return Err(BulkUpdatePurchaseItemsPersistenceError::InvalidWorkflow(
            BulkUpdateItemsError::DuplicateTarget,
        ));
    }
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| BulkUpdatePurchaseItemsPersistenceError::Pool(error.to_string()))?;

    (*pooled)
        .transaction::<PersistedBulkUpdatePurchaseItems, BulkUpdatePurchaseItemsPersistenceError, _>(
            async move |conn| {
                use public::accounting_periods::dsl as periods;
                use public::purchase_items::dsl as items;
                use public::purchases::dsl as purchases;

                if targets.is_empty() {
                    return Err(BulkUpdatePurchaseItemsPersistenceError::InvalidWorkflow(
                        BulkUpdateItemsError::EmptyTargets,
                    ));
                }
                if targets.len() > 100 {
                    return Err(BulkUpdatePurchaseItemsPersistenceError::InvalidWorkflow(
                        BulkUpdateItemsError::TooManyTargets,
                    ));
                }
                let observations = items::purchase_items
                    .filter(items::id.eq_any(&target_ids))
                    .order(items::id.asc())
                    .select((items::id, items::household_id, items::purchase_id, items::revision))
                    .load::<(Uuid, Uuid, Uuid, i64)>(conn)
                    .await?;
                if observations.len() != target_ids.len() {
                    return Err(BulkUpdatePurchaseItemsPersistenceError::ItemNotFound);
                }
                if observations.iter().any(|row| row.1 != household_id) {
                    return Err(BulkUpdatePurchaseItemsPersistenceError::HouseholdMismatch);
                }
                let purchase_ids = observations
                    .iter()
                    .map(|row| row.2)
                    .collect::<BTreeSet<_>>();
                let observed_purchases = purchases::purchases
                    .filter(purchases::id.eq_any(purchase_ids.iter().copied()))
                    .order(purchases::id.asc())
                    .select((
                        purchases::id,
                        purchases::household_id,
                        purchases::occurred_at,
                        purchases::status,
                        purchases::revision,
                        purchases::merchant,
                    ))
                    .load::<(Uuid, Uuid, DateTime<Utc>, String, i64, String)>(conn)
                    .await?;
                if observed_purchases.len() != purchase_ids.len() {
                    return Err(BulkUpdatePurchaseItemsPersistenceError::PurchaseNotFound);
                }
                if observed_purchases.iter().any(|row| row.1 != household_id) {
                    return Err(BulkUpdatePurchaseItemsPersistenceError::HouseholdMismatch);
                }
                let actor_role = household_postgres::household_access::active_member_role(&mut *conn, household_id, actor_id).await?;
                if authorize(actor_role.as_deref(), Operation::FinancialData).is_err() {
                    return Err(BulkUpdatePurchaseItemsPersistenceError::MemberNotFound);
                }
                let period_dates = observed_purchases
                    .iter()
                    .map(|purchase| period_start(purchase.2))
                    .collect::<Result<BTreeSet<_>, _>>()?
                    .into_iter()
                    .collect::<Vec<_>>();
                for period in &period_dates {
                    diesel::insert_into(periods::accounting_periods)
                        .values((
                            periods::household_id.eq(household_id),
                            periods::period_start.eq(period),
                            periods::status.eq("OPEN"),
                            periods::revision.eq(1_i64),
                        ))
                        .on_conflict((periods::household_id, periods::period_start))
                        .do_nothing()
                        .execute(&mut *conn)
                        .await?;
                }
                let locked_periods = periods::accounting_periods
                    .filter(periods::household_id.eq(household_id))
                    .filter(periods::period_start.eq_any(&period_dates))
                    .order(periods::period_start.asc())
                    .for_update()
                    .select((periods::period_start, periods::status))
                    .load::<(NaiveDate, String)>(&mut *conn)
                    .await?;
                if locked_periods.len() != period_dates.len()
                    || locked_periods.iter().any(|(_, status)| status != "OPEN")
                {
                    return Err(BulkUpdatePurchaseItemsPersistenceError::AccountingPeriodClosed);
                }
                let default_version = household_postgres::household_access::default_allocation_version_for_share(
                    &mut *conn,
                    household_id,
                )
                .await?
                .ok_or(BulkUpdatePurchaseItemsPersistenceError::HouseholdMismatch)?;
                let mut locked_purchases = BTreeMap::new();
                for (id, _, occurred_at, _, _, merchant) in &observed_purchases {
                    let row = purchases::purchases
                        .filter(purchases::id.eq(id))
                        .for_update()
                        .select((
                            purchases::household_id,
                            purchases::occurred_at,
                            purchases::status,
                            purchases::revision,
                            purchases::merchant,
                        ))
                        .first::<(Uuid, DateTime<Utc>, String, i64, String)>(&mut *conn)
                        .await
                        .optional()?
                        .ok_or(BulkUpdatePurchaseItemsPersistenceError::PurchaseNotFound)?;
                    if row.0 != household_id || row.1 != *occurred_at || row.4 != *merchant {
                        return Err(BulkUpdatePurchaseItemsPersistenceError::InvalidDecision);
                    }
                    if !matches!(row.2.as_str(), "DRAFT" | "POSTED") {
                        return Err(BulkUpdatePurchaseItemsPersistenceError::InvalidDecision);
                    }
                    locked_purchases.insert(*id, row);
                }
                let mut locked_rows = BTreeMap::new();
                for (id, _, purchase_id, observed_revision) in &observations {
                    let row = items::purchase_items
                        .filter(items::id.eq(id))
                        .for_update()
                        .select((
                            items::household_id,
                            items::purchase_id,
                            items::revision,
                            items::amount_yen,
                            items::category_id,
                            items::product_variant_id,
                            items::allocation_override,
                            items::field_revisions,
                            items::name,
                        ))
                        .first::<(Uuid, Uuid, i64, i64, Option<Uuid>, Option<Uuid>, bool, serde_json::Value, String)>(
                            &mut *conn,
                        )
                        .await
                        .optional()?
                        .ok_or(BulkUpdatePurchaseItemsPersistenceError::ItemNotFound)?;
                    if row.0 != household_id || row.1 != *purchase_id || row.2 != *observed_revision {
                        return Err(BulkUpdatePurchaseItemsPersistenceError::InvalidDecision);
                    }
                    locked_rows.insert(*id, row);
                }
                validate_references(
                    &mut *conn,
                    household_id,
                    patch.category_id_set
                        .then_some(patch.category_id.as_deref())
                        .flatten()
                        .map(|id| parse_uuid(id, "category_id"))
                        .transpose()?,
                    patch.product_variant_id_set
                        .then_some(patch.product_variant_id.as_deref())
                        .flatten()
                        .map(|id| parse_uuid(id, "product_variant_id"))
                        .transpose()?,
                    reference_data,
                )
                .await?;
                let tag_ids = validate_tags(&mut *conn, household_id, patch.tag_ids_set, &patch.tag_ids, reference_data).await?;
                let mut item_states = Vec::with_capacity(targets.len());
                for target in &targets {
                    let item_id = parse_uuid(&target.item_id, "item_id")?;
                    let row = locked_rows.get(&item_id).ok_or(BulkUpdatePurchaseItemsPersistenceError::ItemNotFound)?;
                    let purchase_id = row.1;
                    let purchase = locked_purchases.get(&purchase_id).ok_or(BulkUpdatePurchaseItemsPersistenceError::PurchaseNotFound)?;
                    let current_allocations = public::purchase_allocations::dsl::purchase_allocations
                        .filter(public::purchase_allocations::dsl::household_id.eq(household_id))
                        .filter(public::purchase_allocations::dsl::purchase_id.eq(purchase_id))
                        .filter(public::purchase_allocations::dsl::item_id.eq(item_id))
                        .order(public::purchase_allocations::dsl::pool_id.asc())
                        .select((public::purchase_allocations::dsl::pool_id, public::purchase_allocations::dsl::weight, public::purchase_allocations::dsl::rule_id, public::purchase_allocations::dsl::rule_version))
                        .load::<(Uuid, i64, Option<Uuid>, i64)>(&mut *conn)
                        .await?;
                    let (effective_allocations, allocation_rule_id, allocation_rule_version) = if patch.allocations_set {
                        (patch.allocations.clone(), None, default_version)
                    } else if (patch.category_id_set || patch.product_variant_id_set) && !row.6 {
                        let category_id = if patch.category_id_set { patch.category_id.as_deref().map(|id| parse_uuid(id, "category_id")).transpose()? } else { row.4 };
                        let variant_id = if patch.product_variant_id_set { patch.product_variant_id.as_deref().map(|id| parse_uuid(id, "product_variant_id")).transpose()? } else { row.5 };
                        let basis = load_automatic_basis(&mut *conn, household_id, category_id, variant_id, &purchase.4, reference_data).await?;
                        match basis {
                            Some(basis) => basis,
                            None => load_default_basis(&mut *conn, household_id, default_version, reference_data).await?,
                        }
                    } else if let Some((_, _, rule_id, version)) = current_allocations.as_slice().first() {
                        (current_allocations.iter().map(|(pool_id, weight, _, _)| ItemAllocationWeight { pool_id: pool_id.to_string(), weight: *weight }).collect(), rule_id.map(|id| id.to_string()), *version)
                    } else {
                        load_default_basis(&mut *conn, household_id, default_version, reference_data).await?
                    };
                    let field_revisions = row.7.as_object().into_iter().flatten().filter_map(|(key, value)| value.as_u64().map(|revision| (key.clone(), revision))).collect();
                    item_states.push(BulkUpdateItemState {
                        item_id: item_id.to_string(),
                        purchase_id: purchase_id.to_string(),
                        revision: u64::try_from(row.2).map_err(|_| BulkUpdatePurchaseItemsPersistenceError::NumericOutOfRange("item_revision"))?,
                        amount_yen: row.3,
                        allocation_override: row.6,
                        field_revisions,
                        effective_allocations,
                        allocation_rule_id,
                        allocation_rule_version: u64::try_from(allocation_rule_version).map_err(|_| BulkUpdatePurchaseItemsPersistenceError::NumericOutOfRange("allocation_rule_version"))?,
                    });
                }
                let purchase_states = locked_purchases.iter().map(|(id, row)| Ok(BulkUpdatePurchaseState {
                    purchase_id: id.to_string(),
                    revision: u64::try_from(row.3).map_err(|_| BulkUpdatePurchaseItemsPersistenceError::NumericOutOfRange("purchase_revision"))?,
                })).collect::<Result<Vec<_>, BulkUpdatePurchaseItemsPersistenceError>>()?;
                let decision = decide_bulk_update_items(BulkUpdateItemsCommand { targets: targets.clone(), patch: patch.clone() }, purchase_states, item_states)
                    .map_err(BulkUpdatePurchaseItemsPersistenceError::InvalidWorkflow)?;
                validate_pools(&mut *conn, household_id, &decision.items).await?;
                let mut result = Vec::with_capacity(decision.items.len());
                for item_decision in &decision.items {
                    let item_id = parse_uuid(&item_decision.item_id, "item_id")?;
                    let purchase_id = parse_uuid(&item_decision.purchase_id, "purchase_id")?;
                    let old = locked_rows.get(&item_id).ok_or(BulkUpdatePurchaseItemsPersistenceError::ItemNotFound)?;
                    let category_id = match &item_decision.category_id {
                        Some(Some(id)) => Some(parse_uuid(id, "category_id")?),
                        Some(None) => None,
                        None => old.4,
                    };
                    let variant_id = match &item_decision.product_variant_id {
                        Some(Some(id)) => Some(parse_uuid(id, "product_variant_id")?),
                        Some(None) => None,
                        None => old.5,
                    };
                    let field_revisions = serde_json::Value::Object(item_decision.field_revisions.iter().map(|(field, revision)| {
                        i64::try_from(*revision).map(|revision| (field.clone(), serde_json::Value::from(revision))).map_err(|_| BulkUpdatePurchaseItemsPersistenceError::NumericOutOfRange("field_revision"))
                    }).collect::<Result<serde_json::Map<_, _>, _>>()?);
                    diesel::update(items::purchase_items.filter(items::id.eq(item_id)))
                        .set((
                            items::category_id.eq(category_id),
                            items::product_variant_id.eq(variant_id),
                            items::name.eq(item_decision.name.as_deref().unwrap_or(&old.8)),
                            items::allocation_override.eq(item_decision.allocation_override),
                            items::revision.eq(i64::try_from(item_decision.revision).map_err(|_| BulkUpdatePurchaseItemsPersistenceError::NumericOutOfRange("item_revision"))?),
                            items::field_revisions.eq(field_revisions),
                            items::updated_at.eq(now),
                        ))
                        .execute(&mut *conn)
                        .await?;
                    if let Some(allocations) = &item_decision.allocations {
                        diesel::delete(public::purchase_allocations::dsl::purchase_allocations
                            .filter(public::purchase_allocations::dsl::household_id.eq(household_id))
                            .filter(public::purchase_allocations::dsl::purchase_id.eq(purchase_id))
                            .filter(public::purchase_allocations::dsl::item_id.eq(item_id)))
                            .execute(&mut *conn).await?;
                        for allocation in allocations {
                            diesel::insert_into(public::purchase_allocations::table)
                                .values((
                                    public::purchase_allocations::dsl::household_id.eq(household_id),
                                    public::purchase_allocations::dsl::purchase_id.eq(purchase_id),
                                    public::purchase_allocations::dsl::item_id.eq(item_id),
                                    public::purchase_allocations::dsl::pool_id.eq(parse_uuid(&allocation.pool_id, "pool_id")?),
                                    public::purchase_allocations::dsl::amount_yen.eq(allocation.amount_yen),
                                    public::purchase_allocations::dsl::weight.eq(allocation.weight),
                                    public::purchase_allocations::dsl::rule_id.eq(item_decision.allocation_rule_id.as_deref().map(|id| parse_uuid(id, "rule_id")).transpose()?),
                                    public::purchase_allocations::dsl::rule_version.eq(i64::try_from(item_decision.allocation_rule_version).map_err(|_| BulkUpdatePurchaseItemsPersistenceError::NumericOutOfRange("rule_version"))?),
                                ))
                                .execute(&mut *conn).await?;
                        }
                    }
                    if patch.tag_ids_set {
                        diesel::delete(public::purchase_item_tags::dsl::purchase_item_tags
                            .filter(public::purchase_item_tags::dsl::household_id.eq(household_id))
                            .filter(public::purchase_item_tags::dsl::purchase_id.eq(purchase_id))
                            .filter(public::purchase_item_tags::dsl::item_id.eq(item_id)))
                            .execute(&mut *conn).await?;
                        for tag_id in &tag_ids {
                            diesel::insert_into(public::purchase_item_tags::table)
                                .values((
                                    public::purchase_item_tags::dsl::household_id.eq(household_id),
                                    public::purchase_item_tags::dsl::purchase_id.eq(purchase_id),
                                    public::purchase_item_tags::dsl::item_id.eq(item_id),
                                    public::purchase_item_tags::dsl::tag_id.eq(tag_id),
                                ))
                                .execute(&mut *conn).await?;
                        }
                    }
                    horse_postgres_common::insert_ui_change(
                        &mut *conn,
                        household_id,
                        item_id,
                        i64::try_from(item_decision.revision).map_err(|_| BulkUpdatePurchaseItemsPersistenceError::NumericOutOfRange("item_revision"))?,
                        actor_id,
                        Utc::now(),
                        bookkeeping_application::ui_changes::purchase_item_updated(item_decision.changed_fields.iter().cloned()),
                    ).await?;
                    result.push((item_decision.item_id.clone(), item_decision.purchase_id.clone()));
                }
                for purchase_decision in &decision.purchases {
                    let purchase_id = parse_uuid(&purchase_decision.purchase_id, "purchase_id")?;
                    diesel::update(purchases::purchases.filter(purchases::id.eq(purchase_id)))
                        .set((
                            purchases::revision.eq(i64::try_from(purchase_decision.revision).map_err(|_| BulkUpdatePurchaseItemsPersistenceError::NumericOutOfRange("purchase_revision"))?),
                            purchases::updated_at.eq(now),
                        ))
                        .execute(&mut *conn).await?;
                    let old_purchase = locked_purchases.get(&purchase_id).ok_or(BulkUpdatePurchaseItemsPersistenceError::PurchaseNotFound)?;
                    if old_purchase.2 == "POSTED" {
                        sync_posted_accounting(&mut *conn, household_id, purchase_id, old_purchase.3, reference_data).await?;
                    }
                }
                Ok(PersistedBulkUpdatePurchaseItems { items: result })
            },
        )
        .await
}

async fn load_default_basis<C: crate::update_purchase_item::UpdatePurchaseItemReferenceData>(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    version: i64,
    reference_data: &mut C,
) -> Result<(Vec<ItemAllocationWeight>, Option<String>, i64), BulkUpdatePurchaseItemsPersistenceError>
{
    let weights = reference_data
        .load_default_allocation(conn, household_id, version)
        .await?
        .into_iter()
        .map(|(pool_id, weight)| ItemAllocationWeight {
            pool_id: pool_id.to_string(),
            weight,
        })
        .collect();
    Ok((weights, None, version))
}

async fn load_automatic_basis<C: crate::update_purchase_item::UpdatePurchaseItemReferenceData>(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    category_id: Option<Uuid>,
    variant_id: Option<Uuid>,
    merchant: &str,
    reference_data: &mut C,
) -> Result<
    Option<(Vec<ItemAllocationWeight>, Option<String>, i64)>,
    BulkUpdatePurchaseItemsPersistenceError,
> {
    let Some((rule_id, revision, rows)) = reference_data
        .load_automatic_allocation(conn, household_id, category_id, variant_id, merchant)
        .await?
    else {
        return Ok(None);
    };
    let weights = rows
        .into_iter()
        .map(|(pool_id, weight)| ItemAllocationWeight {
            pool_id: pool_id.to_string(),
            weight,
        })
        .collect();
    Ok(Some((weights, Some(rule_id.to_string()), revision)))
}

async fn validate_references<C: crate::update_purchase_item::UpdatePurchaseItemReferenceData>(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    category_id: Option<Uuid>,
    variant_id: Option<Uuid>,
    reference_data: &mut C,
) -> Result<(), BulkUpdatePurchaseItemsPersistenceError> {
    if let Some(id) = category_id {
        let found = reference_data
            .active_category(conn, household_id, id)
            .await?;
        if !found {
            return Err(BulkUpdatePurchaseItemsPersistenceError::ReferenceNotFound(
                "category_id",
            ));
        }
    }
    if let Some(id) = variant_id {
        let found = reference_data
            .active_variant(conn, household_id, id)
            .await?;
        if !found {
            return Err(BulkUpdatePurchaseItemsPersistenceError::ReferenceNotFound(
                "product_variant_id",
            ));
        }
    }
    Ok(())
}

async fn validate_tags<C: crate::update_purchase_item::UpdatePurchaseItemReferenceData>(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    set: bool,
    tag_ids: &[String],
    reference_data: &mut C,
) -> Result<Vec<Uuid>, BulkUpdatePurchaseItemsPersistenceError> {
    if !set {
        return Ok(Vec::new());
    }
    let ids = tag_ids
        .iter()
        .map(|id| parse_uuid(id, "tag_id"))
        .collect::<Result<Vec<_>, _>>()?;
    if ids.iter().copied().collect::<BTreeSet<_>>().len() != ids.len() {
        return Err(BulkUpdatePurchaseItemsPersistenceError::InvalidWorkflow(
            BulkUpdateItemsError::DuplicateTag,
        ));
    }
    let found = reference_data
        .active_tags(conn, household_id, ids.clone())
        .await?;
    if found.len() != ids.len() {
        return Err(BulkUpdatePurchaseItemsPersistenceError::ReferenceNotFound(
            "tag_id",
        ));
    }
    Ok(ids)
}

async fn validate_pools(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    decisions: &[bookkeeping_domain::purchase_commands::BulkUpdateItemDecision],
) -> Result<(), BulkUpdatePurchaseItemsPersistenceError> {
    let ids = decisions
        .iter()
        .flat_map(|decision| decision.allocations.iter().flatten())
        .map(|allocation| parse_uuid(&allocation.pool_id, "pool_id"))
        .collect::<Result<Vec<_>, _>>()?;
    let unique = ids.iter().copied().collect::<BTreeSet<_>>();
    let found = public::pools::dsl::pools
        .filter(public::pools::dsl::household_id.eq(household_id))
        .filter(public::pools::dsl::id.eq_any(unique.iter().copied()))
        .select(public::pools::dsl::id)
        .load::<Uuid>(conn)
        .await?
        .into_iter()
        .collect::<BTreeSet<_>>();
    if found != unique {
        return Err(BulkUpdatePurchaseItemsPersistenceError::ReferenceNotFound(
            "pool_id",
        ));
    }
    Ok(())
}

async fn sync_posted_accounting<C: crate::update_purchase_item::UpdatePurchaseItemReferenceData>(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    purchase_id: Uuid,
    old_revision: i64,
    reference_data: &mut C,
) -> Result<(), BulkUpdatePurchaseItemsPersistenceError> {
    let loaded = crate::post_purchase_loader::load_post_purchase_command(
        conn,
        &household_id.to_string(),
        &purchase_id.to_string(),
        u64::try_from(old_revision).map_err(|_| {
            BulkUpdatePurchaseItemsPersistenceError::NumericOutOfRange("purchase_revision")
        })?,
        reference_data,
    )
    .await
    .map_err(|error| match error {
        crate::post_purchase_loader::PostPurchaseLoadError::Database(error) => {
            BulkUpdatePurchaseItemsPersistenceError::Database(error)
        }
        _ => BulkUpdatePurchaseItemsPersistenceError::InvalidDecision,
    })?;
    let mut command = loaded.command;
    command.expected_revision = command.draft.revision();
    use public::purchase_member_burdens::dsl as burdens;
    let old_burdens = burdens::purchase_member_burdens
        .filter(burdens::household_id.eq(household_id))
        .filter(burdens::purchase_id.eq(purchase_id))
        .select((
            burdens::item_id,
            burdens::pool_id,
            burdens::member_id,
            burdens::funding_rule_version,
            burdens::funding_weight,
            burdens::settlement_policy,
            burdens::snapshot_source,
        ))
        .load::<(Uuid, Uuid, Uuid, i32, i64, Option<String>, String)>(&mut *conn)
        .await?;
    let mut groups = BTreeMap::<(Uuid, Uuid), Vec<(Uuid, i32, i64, Option<String>)>>::new();
    let mut sources = BTreeMap::new();
    for (item_id, pool_id, member_id, version, weight, policy, source) in old_burdens {
        groups
            .entry((item_id, pool_id))
            .or_default()
            .push((member_id, version, weight, policy));
        sources.insert((item_id, pool_id, member_id), source);
    }
    let bases = groups
        .into_iter()
        .map(|(key, rows)| Ok((key, funding_basis(rows)?)))
        .collect::<Result<BTreeMap<_, _>, BulkUpdatePurchaseItemsPersistenceError>>()?;
    let mut allocation_funding_updates = Vec::new();
    for allocation in command.draft.allocations() {
        let key = (
            parse_uuid(allocation.item_id.as_str(), "item_id")?,
            parse_uuid(allocation.pool_id.as_str(), "pool_id")?,
        );
        if let Some(basis) = bases.get(&key) {
            allocation_funding_updates.push((
                allocation.item_id.clone(),
                allocation.pool_id.clone(),
                basis.clone(),
            ));
        }
    }
    for (item_id, pool_id, basis) in allocation_funding_updates {
        if !command
            .draft
            .update_allocation_funding(&item_id, &pool_id, basis)
        {
            return Err(BulkUpdatePurchaseItemsPersistenceError::InvalidDecision);
        }
    }
    use public::purchase_payment_funding_snapshots::dsl as payment_snapshots;
    let old_payment_bases = payment_snapshots::purchase_payment_funding_snapshots
        .filter(payment_snapshots::household_id.eq(household_id))
        .filter(payment_snapshots::purchase_id.eq(purchase_id))
        .select((
            payment_snapshots::account_id,
            payment_snapshots::pool_id,
            payment_snapshots::member_id,
            payment_snapshots::funding_rule_version,
            payment_snapshots::funding_weight,
            payment_snapshots::settlement_policy,
        ))
        .load::<(Uuid, Uuid, Uuid, i32, i64, Option<String>)>(&mut *conn)
        .await?;
    let mut payment_groups = BTreeMap::<(Uuid, Uuid), Vec<(Uuid, i32, i64, Option<String>)>>::new();
    for (account_id, pool_id, member_id, version, weight, policy) in old_payment_bases {
        payment_groups
            .entry((account_id, pool_id))
            .or_default()
            .push((member_id, version, weight, policy));
    }
    let payment_bases = payment_groups
        .into_iter()
        .map(|(key, rows)| Ok((key, funding_basis(rows)?)))
        .collect::<Result<BTreeMap<_, _>, BulkUpdatePurchaseItemsPersistenceError>>()?;
    let mut payment_funding_updates = Vec::new();
    for payment in command.draft.payments() {
        let account_id = parse_uuid(payment.account_id.as_str(), "account_id")?;
        let pool_id = parse_uuid(payment.funding.pool_id.as_str(), "pool_id")?;
        if let Some(basis) = payment_bases.get(&(account_id, pool_id)).or_else(|| {
            payment_bases
                .iter()
                .find(|((_, old_pool), _)| *old_pool == pool_id)
                .map(|(_, basis)| basis)
        }) {
            payment_funding_updates.push((
                payment.account_id.clone(),
                payment.funding.pool_id.clone(),
                basis.clone(),
            ));
        }
    }
    for (account_id, pool_id, basis) in payment_funding_updates {
        if !command
            .draft
            .update_payment_funding(&account_id, &pool_id, basis)
        {
            return Err(BulkUpdatePurchaseItemsPersistenceError::InvalidDecision);
        }
    }
    let decision = bookkeeping_application::decide_post_purchase(command)
        .map_err(|_| BulkUpdatePurchaseItemsPersistenceError::InvalidDecision)?;
    let journal_ids = public::journals::dsl::journals
        .filter(public::journals::dsl::household_id.eq(household_id))
        .filter(public::journals::dsl::source_kind.eq_any(["purchase", "PURCHASE"]))
        .filter(public::journals::dsl::source_id.eq(purchase_id))
        .select(public::journals::dsl::id)
        .load::<Uuid>(&mut *conn)
        .await?;
    if journal_ids.len() != 1 {
        return Err(BulkUpdatePurchaseItemsPersistenceError::InvalidDecision);
    }
    let journal_id = journal_ids[0];
    diesel::delete(
        public::journal_entries::dsl::journal_entries
            .filter(public::journal_entries::dsl::household_id.eq(household_id))
            .filter(public::journal_entries::dsl::journal_id.eq(journal_id)),
    )
    .execute(&mut *conn)
    .await?;
    for line in decision.journal_lines() {
        let (account_id, pool_id) = match line.target() {
            PostingTarget::Account(id) => (Some(parse_uuid(id.as_str(), "account_id")?), None),
            PostingTarget::Pool(id) => (None, Some(parse_uuid(id.as_str(), "pool_id")?)),
        };
        diesel::insert_into(public::journal_entries::table)
            .values((
                public::journal_entries::dsl::household_id.eq(household_id),
                public::journal_entries::dsl::journal_id.eq(journal_id),
                public::journal_entries::dsl::line_no.eq(i32::try_from(line.line_no()).map_err(
                    |_| BulkUpdatePurchaseItemsPersistenceError::NumericOutOfRange("line_no"),
                )?),
                public::journal_entries::dsl::account_id.eq(account_id),
                public::journal_entries::dsl::pool_id.eq(pool_id),
                public::journal_entries::dsl::member_id.eq::<Option<Uuid>>(None),
                public::journal_entries::dsl::debit_yen.eq(
                    if line.side() == bookkeeping_application::Side::Debit {
                        line.amount().value()
                    } else {
                        0
                    },
                ),
                public::journal_entries::dsl::credit_yen.eq(
                    if line.side() == bookkeeping_application::Side::Credit {
                        line.amount().value()
                    } else {
                        0
                    },
                ),
                public::journal_entries::dsl::memo.eq(line.memo()),
            ))
            .execute(&mut *conn)
            .await?;
    }
    diesel::delete(
        public::purchase_member_burdens::dsl::purchase_member_burdens
            .filter(public::purchase_member_burdens::dsl::household_id.eq(household_id))
            .filter(public::purchase_member_burdens::dsl::purchase_id.eq(purchase_id)),
    )
    .execute(&mut *conn)
    .await?;
    for snapshot in decision.member_burden_snapshots() {
        let item_id = parse_uuid(snapshot.item_id.as_str(), "item_id")?;
        let pool_id = parse_uuid(snapshot.pool_id.as_str(), "pool_id")?;
        let member_id = parse_uuid(snapshot.member_id.as_str(), "member_id")?;
        diesel::insert_into(public::purchase_member_burdens::table)
            .values((
                public::purchase_member_burdens::dsl::household_id.eq(household_id),
                public::purchase_member_burdens::dsl::purchase_id.eq(purchase_id),
                public::purchase_member_burdens::dsl::item_id.eq(item_id),
                public::purchase_member_burdens::dsl::pool_id.eq(pool_id),
                public::purchase_member_burdens::dsl::member_id.eq(member_id),
                public::purchase_member_burdens::dsl::amount_yen.eq(snapshot.amount.value()),
                public::purchase_member_burdens::dsl::funding_rule_version.eq(i32::try_from(
                    snapshot.funding_rule_version,
                )
                .map_err(|_| {
                    BulkUpdatePurchaseItemsPersistenceError::NumericOutOfRange(
                        "funding_rule_version",
                    )
                })?),
                public::purchase_member_burdens::dsl::funding_weight.eq(i64::try_from(
                    snapshot.funding_weight,
                )
                .map_err(|_| {
                    BulkUpdatePurchaseItemsPersistenceError::NumericOutOfRange("funding_weight")
                })?),
                public::purchase_member_burdens::dsl::settlement_policy
                    .eq(snapshot.settlement_policy.map(|policy| policy.as_str())),
                public::purchase_member_burdens::dsl::snapshot_source.eq(sources
                    .get(&(item_id, pool_id, member_id))
                    .map(String::as_str)
                    .unwrap_or("POSTED")),
            ))
            .execute(&mut *conn)
            .await?;
    }
    let amount = decision
        .journal_lines()
        .iter()
        .filter(|line| line.side() == bookkeeping_application::Side::Debit)
        .try_fold(0_i64, |total, line| {
            total.checked_add(line.amount().value())
        })
        .ok_or(BulkUpdatePurchaseItemsPersistenceError::NumericOutOfRange(
            "ledger_amount",
        ))?;
    let updated = diesel::update(
        public::ledger_entries::dsl::ledger_entries
            .filter(public::ledger_entries::dsl::household_id.eq(household_id))
            .filter(public::ledger_entries::dsl::source_kind.eq_any(["purchase", "PURCHASE"]))
            .filter(public::ledger_entries::dsl::source_id.eq(purchase_id)),
    )
    .set(public::ledger_entries::dsl::amount_yen.eq(amount))
    .execute(&mut *conn)
    .await?;
    if updated == 0 {
        return Err(BulkUpdatePurchaseItemsPersistenceError::InvalidDecision);
    }
    Ok(())
}

fn period_start(
    value: DateTime<Utc>,
) -> Result<NaiveDate, BulkUpdatePurchaseItemsPersistenceError> {
    let tokyo = FixedOffset::east_opt(9 * 60 * 60)
        .ok_or(BulkUpdatePurchaseItemsPersistenceError::InvalidDecision)?;
    let date = value.with_timezone(&tokyo);
    NaiveDate::from_ymd_opt(date.year(), date.month(), 1)
        .ok_or(BulkUpdatePurchaseItemsPersistenceError::InvalidDecision)
}

fn funding_basis(
    rows: Vec<(Uuid, i32, i64, Option<String>)>,
) -> Result<MemberFundingBasis, BulkUpdatePurchaseItemsPersistenceError> {
    let Some((_, version, _, policy)) = rows.as_slice().first() else {
        return Err(BulkUpdatePurchaseItemsPersistenceError::InvalidDecision);
    };
    if rows.iter().any(|(_, other_version, _, other_policy)| {
        other_version != version || other_policy != policy
    }) {
        return Err(BulkUpdatePurchaseItemsPersistenceError::InvalidDecision);
    }
    let version = u32::try_from(*version)
        .map_err(|_| BulkUpdatePurchaseItemsPersistenceError::InvalidDecision)?;
    if let Some(policy) = policy {
        let policy = match policy.as_str() {
            "BALANCE_TO_STANDARD" => SettlementPolicy::BalanceToStandard,
            "REIMBURSE_PERSONAL" => SettlementPolicy::ReimbursePersonal,
            _ => return Err(BulkUpdatePurchaseItemsPersistenceError::InvalidDecision),
        };
        let weights = rows
            .into_iter()
            .map(|(member, _, weight, _)| {
                MemberWeight::new(
                    MemberId::new(member.to_string())
                        .map_err(|_| BulkUpdatePurchaseItemsPersistenceError::InvalidDecision)?,
                    u64::try_from(weight)
                        .map_err(|_| BulkUpdatePurchaseItemsPersistenceError::InvalidDecision)?,
                )
                .map_err(|_| BulkUpdatePurchaseItemsPersistenceError::InvalidDecision)
            })
            .collect::<Result<Vec<_>, _>>()?;
        MemberFundingBasis::shared(version, weights, policy)
            .map_err(|_| BulkUpdatePurchaseItemsPersistenceError::InvalidDecision)
    } else {
        if rows.len() != 1 || rows[0].2 != 1 {
            return Err(BulkUpdatePurchaseItemsPersistenceError::InvalidDecision);
        }
        MemberFundingBasis::personal(
            version,
            MemberId::new(rows[0].0.to_string())
                .map_err(|_| BulkUpdatePurchaseItemsPersistenceError::InvalidDecision)?,
        )
        .map_err(|_| BulkUpdatePurchaseItemsPersistenceError::InvalidDecision)
    }
}

fn parse_uuid(
    value: &str,
    field: &'static str,
) -> Result<Uuid, BulkUpdatePurchaseItemsPersistenceError> {
    Uuid::parse_str(value).map_err(|_| BulkUpdatePurchaseItemsPersistenceError::InvalidUuid(field))
}
