use std::collections::BTreeMap;

use bookkeeping_workflows::update_purchase_item::{
    AllocationBasis, ItemAllocationWeight, PurchaseItemState, UpdatePurchaseItemCommand,
    UpdatePurchaseItemDecision, UpdatePurchaseItemError, decide_update_purchase_item,
};
use chrono::{DateTime, Datelike, FixedOffset, NaiveDate, Utc};
use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug, Clone)]
pub struct PersistUpdatePurchaseItem {
    pub household_id: String,
    pub actor_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PersistedUpdatePurchaseItem {
    pub purchase_id: String,
    pub revision: u64,
}

#[derive(Debug)]
pub enum UpdatePurchaseItemPersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    NumericOutOfRange(&'static str),
    PurchaseNotFound,
    ItemNotFound,
    HouseholdMismatch,
    MemberNotFound,
    AccountingPeriodClosed,
    ReferenceNotFound(&'static str),
    PostedAccountingSyncRequired,
    InvalidDecision,
    InvalidWorkflow(UpdatePurchaseItemError),
}

impl From<DieselError> for UpdatePurchaseItemPersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_update_purchase_item(
    pool: &PgPool,
    input: PersistUpdatePurchaseItem,
    command: UpdatePurchaseItemCommand,
) -> Result<PersistedUpdatePurchaseItem, UpdatePurchaseItemPersistenceError> {
    let household_id = parse_uuid(&input.household_id, "household_id")?;
    let actor_id = parse_uuid(&input.actor_id, "actor_id")?;
    let item_id = parse_uuid(&command.item_id, "item_id")?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| UpdatePurchaseItemPersistenceError::Pool(error.to_string()))?;

    (&mut *pooled)
        .transaction::<PersistedUpdatePurchaseItem, UpdatePurchaseItemPersistenceError, _>(
            async move |conn| {
                use public::accounting_periods::dsl as periods;
                use public::household_events::dsl as events;
                use public::household_members::dsl as members;
                use public::households::dsl as households;
                use public::purchase_items::dsl as items;
                use public::purchases::dsl as purchases;

                let observed = items::purchase_items
                    .filter(items::id.eq(item_id))
                    .select((items::household_id, items::purchase_id))
                    .first::<(Uuid, Uuid)>(conn)
                    .await
                    .optional()?
                    .ok_or(UpdatePurchaseItemPersistenceError::ItemNotFound)?;
                if observed.0 != household_id {
                    return Err(UpdatePurchaseItemPersistenceError::HouseholdMismatch);
                }
                let purchase_observed = purchases::purchases
                    .filter(purchases::id.eq(observed.1))
                    .select((purchases::occurred_at, purchases::status))
                    .first::<(DateTime<Utc>, String)>(conn)
                    .await
                    .optional()?
                    .ok_or(UpdatePurchaseItemPersistenceError::PurchaseNotFound)?;
                if purchase_observed.1 == "POSTED" {
                    return Err(UpdatePurchaseItemPersistenceError::PostedAccountingSyncRequired);
                }
                require_member(&mut *conn, household_id, actor_id).await?;
                let old_period = accounting_period(purchase_observed.0)?;
                diesel::insert_into(periods::accounting_periods)
                    .values((
                        periods::household_id.eq(household_id),
                        periods::period_start.eq(old_period),
                        periods::status.eq("OPEN"),
                        periods::revision.eq(1_i64),
                    ))
                    .on_conflict((periods::household_id, periods::period_start))
                    .do_nothing()
                    .execute(&mut *conn)
                    .await?;
                let period_status = periods::accounting_periods
                    .filter(periods::household_id.eq(household_id))
                    .filter(periods::period_start.eq(old_period))
                    .for_update()
                    .select(periods::status)
                    .first::<String>(&mut *conn)
                    .await?;
                if period_status != "OPEN" {
                    return Err(UpdatePurchaseItemPersistenceError::AccountingPeriodClosed);
                }

                let default_version = households::households
                    .filter(households::id.eq(household_id))
                    .for_share()
                    .select(households::default_allocation_version)
                    .first::<i64>(&mut *conn)
                    .await
                    .optional()?
                    .ok_or(UpdatePurchaseItemPersistenceError::HouseholdMismatch)?;
                let active_member = members::household_members
                    .filter(members::household_id.eq(household_id))
                    .filter(members::member_id.eq(actor_id))
                    .filter(members::left_at.is_null())
                    .select(members::member_id)
                    .first::<Uuid>(&mut *conn)
                    .await
                    .optional()?
                    .is_some();
                if !active_member {
                    return Err(UpdatePurchaseItemPersistenceError::MemberNotFound);
                }

                let purchase = purchases::purchases
                    .filter(purchases::id.eq(observed.1))
                    .for_update()
                    .select((
                        purchases::household_id,
                        purchases::status,
                        purchases::occurred_at,
                        purchases::merchant,
                        purchases::revision,
                    ))
                    .first::<(Uuid, String, DateTime<Utc>, String, i64)>(&mut *conn)
                    .await
                    .optional()?
                    .ok_or(UpdatePurchaseItemPersistenceError::PurchaseNotFound)?;
                if purchase.0 != household_id || accounting_period(purchase.2)? != old_period {
                    return Err(UpdatePurchaseItemPersistenceError::HouseholdMismatch);
                }
                if purchase.1 != "DRAFT" {
                    return Err(UpdatePurchaseItemPersistenceError::PostedAccountingSyncRequired);
                }

                let locked = items::purchase_items
                    .filter(items::id.eq(item_id))
                    .filter(items::purchase_id.eq(observed.1))
                    .for_update()
                    .select((
                        items::revision,
                        items::field_revisions,
                        items::name,
                        items::amount_yen,
                        items::category_id,
                        items::product_variant_id,
                        items::allocation_override,
                        items::quantity_numerator,
                        items::quantity_denominator,
                        items::quantity_unit,
                        items::printed_unit_price_yen,
                        items::printed_unit_price_quantity_numerator,
                        items::printed_unit_price_quantity_denominator,
                        items::printed_unit_price_unit,
                    ))
                    .first::<(
                        i64,
                        serde_json::Value,
                        String,
                        i64,
                        Option<Uuid>,
                        Option<Uuid>,
                        bool,
                        Option<i64>,
                        Option<i64>,
                        Option<String>,
                        Option<i64>,
                        Option<i64>,
                        Option<i64>,
                        Option<String>,
                    )>(&mut *conn)
                    .await
                    .optional()?
                    .ok_or(UpdatePurchaseItemPersistenceError::ItemNotFound)?;

                let existing_allocations = public::purchase_allocations::dsl::purchase_allocations
                    .filter(public::purchase_allocations::dsl::item_id.eq(item_id))
                    .order(public::purchase_allocations::dsl::pool_id)
                    .select((
                        public::purchase_allocations::dsl::pool_id,
                        public::purchase_allocations::dsl::weight,
                        public::purchase_allocations::dsl::rule_id,
                        public::purchase_allocations::dsl::rule_version,
                    ))
                    .load::<(Uuid, i64, Option<Uuid>, i64)>(&mut *conn)
                    .await?;
                let current_allocations = existing_allocations
                    .iter()
                    .map(|(pool_id, weight, _, _)| ItemAllocationWeight {
                        pool_id: pool_id.to_string(),
                        weight: *weight,
                    })
                    .collect::<Vec<_>>();
                let (allocation_rule_id, allocation_rule_version) = existing_allocations
                    .first()
                    .map(|row| (row.2.map(|id| id.to_string()), row.3))
                    .unwrap_or((None, default_version));
                let current = PurchaseItemState {
                    revision: to_u64(locked.0, "item_revision")?,
                    status: purchase.1,
                    name: locked.2.clone(),
                    amount_yen: locked.3,
                    category_id: locked.4.map(|id| id.to_string()),
                    product_variant_id: locked.5.map(|id| id.to_string()),
                    quantity: measure(locked.7, locked.8, locked.9)?,
                    printed_unit_price: printed_price(locked.10, locked.11, locked.12, locked.13)?,
                    allocation_override: locked.6,
                    allocation_rule_id,
                    allocation_rule_version: to_u64(
                        allocation_rule_version,
                        "allocation_rule_version",
                    )?,
                    field_revisions: revisions(locked.1)?,
                    allocations: current_allocations,
                };
                let default_basis =
                    load_default_basis(&mut *conn, household_id, default_version).await?;
                let target_category_id = if command.category_id_set {
                    command
                        .category_id
                        .as_deref()
                        .map(|id| parse_uuid(id, "category_id"))
                        .transpose()?
                } else {
                    locked.4
                };
                let target_variant_id = if command.product_variant_id_set {
                    command
                        .product_variant_id
                        .as_deref()
                        .map(|id| parse_uuid(id, "product_variant_id"))
                        .transpose()?
                } else {
                    locked.5
                };
                let automatic_basis = if command.category_id_set || command.product_variant_id_set {
                    load_automatic_basis(
                        &mut *conn,
                        household_id,
                        target_category_id,
                        target_variant_id,
                        &purchase.3,
                    )
                    .await?
                } else {
                    None
                };
                validate_references(
                    &mut *conn,
                    household_id,
                    command
                        .category_id_set
                        .then_some(command.category_id.as_deref())
                        .flatten()
                        .map(|id| parse_uuid(id, "category_id"))
                        .transpose()?,
                    command
                        .product_variant_id_set
                        .then_some(command.product_variant_id.as_deref())
                        .flatten()
                        .map(|id| parse_uuid(id, "product_variant_id"))
                        .transpose()?,
                )
                .await?;
                let decision = decide_update_purchase_item(
                    command.clone(),
                    current,
                    automatic_basis,
                    default_basis,
                )
                .map_err(UpdatePurchaseItemPersistenceError::InvalidWorkflow)?;
                validate_decision(&decision, item_id)?;
                let next_revision = i64::try_from(decision.revision).map_err(|_| {
                    UpdatePurchaseItemPersistenceError::NumericOutOfRange("revision")
                })?;
                let field_revisions = serde_json::Value::Object(
                    decision
                        .field_revisions
                        .iter()
                        .map(|(field, revision)| {
                            i64::try_from(*revision)
                                .map(|revision| (field.clone(), serde_json::Value::from(revision)))
                                .map_err(|_| {
                                    UpdatePurchaseItemPersistenceError::NumericOutOfRange(
                                        "field_revision",
                                    )
                                })
                        })
                        .collect::<Result<serde_json::Map<_, _>, _>>()?,
                );
                let (quantity_numerator, quantity_denominator, quantity_unit) =
                    match decision.quantity.as_ref() {
                        None => (locked.7, locked.8, locked.9.clone()),
                        Some(None) => (None, None, None),
                        Some(Some(value)) => (
                            Some(value.numerator.parse().map_err(|_| {
                                UpdatePurchaseItemPersistenceError::InvalidDecision
                            })?),
                            Some(value.denominator.parse().map_err(|_| {
                                UpdatePurchaseItemPersistenceError::InvalidDecision
                            })?),
                            Some(value.unit.clone()),
                        ),
                    };
                let (
                    printed_unit_price_yen,
                    printed_unit_price_quantity_numerator,
                    printed_unit_price_quantity_denominator,
                    printed_unit_price_unit,
                ) = match decision.printed_unit_price.as_ref() {
                    None => (locked.10, locked.11, locked.12, locked.13.clone()),
                    Some(None) => (None, None, None, None),
                    Some(Some(value)) => (
                        Some(value.amount_yen),
                        Some(value.basis.numerator.parse().map_err(|_| {
                            UpdatePurchaseItemPersistenceError::InvalidDecision
                        })?),
                        Some(value.basis.denominator.parse().map_err(|_| {
                            UpdatePurchaseItemPersistenceError::InvalidDecision
                        })?),
                        Some(value.basis.unit.clone()),
                    ),
                };
                let category_id = match &decision.category_id {
                    Some(Some(value)) => Some(parse_uuid(value, "category_id")?),
                    Some(None) => None,
                    None => locked.4,
                };
                let product_variant_id = match &decision.product_variant_id {
                    Some(Some(value)) => Some(parse_uuid(value, "product_variant_id")?),
                    Some(None) => None,
                    None => locked.5,
                };
                diesel::update(items::purchase_items.filter(items::id.eq(item_id)))
                    .set((
                        items::name.eq(decision.name.as_deref().unwrap_or(&locked.2)),
                        items::amount_yen.eq(decision.amount_yen.unwrap_or(locked.3)),
                        items::category_id.eq(category_id),
                        items::product_variant_id.eq(product_variant_id),
                        items::allocation_override.eq(decision.allocation_override),
                        items::revision.eq(next_revision),
                        items::field_revisions.eq(field_revisions),
                        items::quantity_numerator.eq(quantity_numerator),
                        items::quantity_denominator.eq(quantity_denominator),
                        items::quantity_unit.eq(quantity_unit),
                        items::printed_unit_price_yen.eq(printed_unit_price_yen),
                        items::printed_unit_price_quantity_numerator
                            .eq(printed_unit_price_quantity_numerator),
                        items::printed_unit_price_quantity_denominator
                            .eq(printed_unit_price_quantity_denominator),
                        items::printed_unit_price_unit.eq(printed_unit_price_unit),
                        items::updated_at.eq(now),
                    ))
                    .execute(&mut *conn)
                    .await?;

                if let Some(allocations) = &decision.allocations {
                    validate_pools(&mut *conn, household_id, allocations).await?;
                    diesel::delete(
                        public::purchase_allocations::dsl::purchase_allocations
                            .filter(public::purchase_allocations::dsl::item_id.eq(item_id)),
                    )
                    .execute(&mut *conn)
                    .await?;
                    for allocation in allocations {
                        let pool_id = parse_uuid(&allocation.pool_id, "pool_id")?;
                        let rule_id = decision
                            .allocation_rule_id
                            .as_deref()
                            .map(|id| parse_uuid(id, "allocation_rule_id"))
                            .transpose()?;
                        let rule_version = i64::try_from(decision.allocation_rule_version)
                            .map_err(|_| {
                                UpdatePurchaseItemPersistenceError::NumericOutOfRange(
                                    "allocation_rule_version",
                                )
                            })?;
                        diesel::insert_into(public::purchase_allocations::table)
                            .values((
                                public::purchase_allocations::household_id.eq(household_id),
                                public::purchase_allocations::purchase_id.eq(observed.1),
                                public::purchase_allocations::item_id.eq(item_id),
                                public::purchase_allocations::pool_id.eq(pool_id),
                                public::purchase_allocations::amount_yen.eq(allocation.amount_yen),
                                public::purchase_allocations::weight.eq(allocation.weight),
                                public::purchase_allocations::rule_version.eq(rule_version),
                                public::purchase_allocations::rule_id.eq(rule_id),
                            ))
                            .execute(&mut *conn)
                            .await?;
                    }
                }
                let purchase_revision = purchase.4.checked_add(1).ok_or(
                    UpdatePurchaseItemPersistenceError::NumericOutOfRange("purchase_revision"),
                )?;
                diesel::update(purchases::purchases.filter(purchases::id.eq(observed.1)))
                    .set((
                        purchases::revision.eq(purchase_revision),
                        purchases::updated_at.eq(now),
                    ))
                    .execute(&mut *conn)
                    .await?;
                diesel::insert_into(events::household_events)
                    .values((
                        events::household_id.eq(household_id),
                        events::event_type.eq("purchase.item.updated"),
                        events::entity_kind.eq("purchase_item"),
                        events::entity_id.eq(item_id),
                        events::revision.eq(next_revision),
                        events::changed_fields.eq(decision
                            .changed_fields
                            .iter()
                            .cloned()
                            .map(Some)
                            .collect::<Vec<_>>()),
                        events::actor_id.eq(actor_id),
                    ))
                    .execute(&mut *conn)
                    .await?;
                Ok(PersistedUpdatePurchaseItem {
                    purchase_id: observed.1.to_string(),
                    revision: decision.revision,
                })
            },
        )
        .await
}

async fn require_member(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    actor_id: Uuid,
) -> Result<(), UpdatePurchaseItemPersistenceError> {
    let exists = public::household_members::dsl::household_members
        .filter(public::household_members::dsl::household_id.eq(household_id))
        .filter(public::household_members::dsl::member_id.eq(actor_id))
        .filter(public::household_members::dsl::left_at.is_null())
        .select(public::household_members::dsl::member_id)
        .first::<Uuid>(conn)
        .await
        .optional()?
        .is_some();
    if exists {
        Ok(())
    } else {
        Err(UpdatePurchaseItemPersistenceError::MemberNotFound)
    }
}

async fn load_default_basis(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    version: i64,
) -> Result<AllocationBasis, UpdatePurchaseItemPersistenceError> {
    let weights = public::allocation_defaults::dsl::allocation_defaults
        .filter(public::allocation_defaults::dsl::household_id.eq(household_id))
        .filter(public::allocation_defaults::dsl::version.eq(version))
        .order(public::allocation_defaults::dsl::pool_id)
        .select((
            public::allocation_defaults::dsl::pool_id,
            public::allocation_defaults::dsl::weight,
        ))
        .load::<(Uuid, i64)>(conn)
        .await?
        .into_iter()
        .map(|(pool_id, weight)| ItemAllocationWeight {
            pool_id: pool_id.to_string(),
            weight,
        })
        .collect();
    Ok(AllocationBasis {
        weights,
        rule_id: None,
        rule_version: to_u64(version, "default_allocation_version")?,
    })
}

async fn load_automatic_basis(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    category_id: Option<Uuid>,
    variant_id: Option<Uuid>,
    merchant: &str,
) -> Result<Option<AllocationBasis>, UpdatePurchaseItemPersistenceError> {
    use public::allocation_rules::dsl as rules;
    let concept_id = if let Some(variant_id) = variant_id {
        public::product_variants::dsl::product_variants
            .filter(public::product_variants::dsl::household_id.eq(household_id))
            .filter(public::product_variants::dsl::id.eq(variant_id))
            .select(public::product_variants::dsl::concept_id)
            .first::<Uuid>(conn)
            .await
            .optional()?
    } else {
        None
    };
    let mut rule_id = None;
    for condition in [
        variant_id.map(|id| rules::product_variant_id.eq(Some(id))),
        concept_id.map(|id| rules::product_concept_id.eq(Some(id))),
        category_id.map(|id| rules::category_id.eq(Some(id))),
        (!merchant.trim().is_empty())
            .then(|| rules::merchant_key.eq(Some(normalize_merchant(merchant)))),
    ]
    .into_iter()
    .flatten()
    {
        if let Some(id) = rules::allocation_rules
            .filter(rules::household_id.eq(household_id))
            .filter(condition)
            .select((rules::id, rules::revision))
            .first::<(Uuid, i64)>(conn)
            .await
            .optional()?
        {
            rule_id = Some(id);
            break;
        }
    }
    let Some((rule_id, revision)) = rule_id else {
        return Ok(None);
    };
    let weights = public::allocation_rule_weights::dsl::allocation_rule_weights
        .filter(public::allocation_rule_weights::dsl::household_id.eq(household_id))
        .filter(public::allocation_rule_weights::dsl::rule_id.eq(rule_id))
        .order(public::allocation_rule_weights::dsl::pool_id)
        .select((
            public::allocation_rule_weights::dsl::pool_id,
            public::allocation_rule_weights::dsl::weight,
        ))
        .load::<(Uuid, i64)>(conn)
        .await?
        .into_iter()
        .map(|(pool_id, weight)| ItemAllocationWeight {
            pool_id: pool_id.to_string(),
            weight,
        })
        .collect();
    Ok(Some(AllocationBasis {
        weights,
        rule_id: Some(rule_id.to_string()),
        rule_version: to_u64(revision, "allocation_rule_revision")?,
    }))
}

async fn validate_references(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    category_id: Option<Uuid>,
    variant_id: Option<Uuid>,
) -> Result<(), UpdatePurchaseItemPersistenceError> {
    if let Some(category_id) = category_id {
        let exists = public::categories::dsl::categories
            .filter(public::categories::dsl::household_id.eq(household_id))
            .filter(public::categories::dsl::id.eq(category_id))
            .filter(public::categories::dsl::archived.eq(false))
            .select(public::categories::dsl::id)
            .first::<Uuid>(conn)
            .await
            .optional()?
            .is_some();
        if !exists {
            return Err(UpdatePurchaseItemPersistenceError::ReferenceNotFound(
                "category_id",
            ));
        }
    }
    if let Some(variant_id) = variant_id {
        let exists = public::product_variants::dsl::product_variants
            .filter(public::product_variants::dsl::household_id.eq(household_id))
            .filter(public::product_variants::dsl::id.eq(variant_id))
            .filter(public::product_variants::dsl::archived.eq(false))
            .select(public::product_variants::dsl::id)
            .first::<Uuid>(conn)
            .await
            .optional()?
            .is_some();
        if !exists {
            return Err(UpdatePurchaseItemPersistenceError::ReferenceNotFound(
                "product_variant_id",
            ));
        }
    }
    Ok(())
}

async fn validate_pools(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    allocations: &[bookkeeping_workflows::update_purchase_item::ItemAllocation],
) -> Result<(), UpdatePurchaseItemPersistenceError> {
    let ids = allocations
        .iter()
        .map(|allocation| parse_uuid(&allocation.pool_id, "pool_id"))
        .collect::<Result<Vec<_>, _>>()?;
    let found = public::pools::dsl::pools
        .filter(public::pools::dsl::household_id.eq(household_id))
        .filter(public::pools::dsl::id.eq_any(ids.clone()))
        .select(public::pools::dsl::id)
        .load::<Uuid>(conn)
        .await?;
    if found.len() != ids.len() {
        return Err(UpdatePurchaseItemPersistenceError::ReferenceNotFound(
            "pool_id",
        ));
    }
    Ok(())
}

fn measure(
    numerator: Option<i64>,
    denominator: Option<i64>,
    unit: Option<String>,
) -> Result<Option<bookkeeping_workflows::Measure>, UpdatePurchaseItemPersistenceError> {
    match (numerator, denominator, unit) {
        (None, None, None) => Ok(None),
        (Some(numerator), Some(denominator), Some(unit)) => {
            Ok(Some(bookkeeping_workflows::Measure {
                numerator: numerator.to_string(),
                denominator: denominator.to_string(),
                unit,
            }))
        }
        _ => Err(UpdatePurchaseItemPersistenceError::InvalidDecision),
    }
}

fn printed_price(
    yen: Option<i64>,
    numerator: Option<i64>,
    denominator: Option<i64>,
    unit: Option<String>,
) -> Result<
    Option<bookkeeping_workflows::update_purchase_item::PrintedUnitPrice>,
    UpdatePurchaseItemPersistenceError,
> {
    match (yen, numerator, denominator, unit) {
        (None, None, None, None) => Ok(None),
        (Some(amount_yen), Some(numerator), Some(denominator), Some(unit)) => Ok(Some(
            bookkeeping_workflows::update_purchase_item::PrintedUnitPrice {
                amount_yen,
                basis: bookkeeping_workflows::Measure {
                    numerator: numerator.to_string(),
                    denominator: denominator.to_string(),
                    unit,
                },
            },
        )),
        _ => Err(UpdatePurchaseItemPersistenceError::InvalidDecision),
    }
}

fn revisions(
    value: serde_json::Value,
) -> Result<BTreeMap<String, u64>, UpdatePurchaseItemPersistenceError> {
    serde_json::from_value::<BTreeMap<String, u64>>(value)
        .map_err(|_| UpdatePurchaseItemPersistenceError::InvalidDecision)
}

fn validate_decision(
    decision: &UpdatePurchaseItemDecision,
    item_id: Uuid,
) -> Result<(), UpdatePurchaseItemPersistenceError> {
    if decision.revision == 0
        || decision.changed_fields.is_empty()
        || decision.allocations_changed && decision.allocations.as_ref().is_none_or(Vec::is_empty)
        || !decision.allocations_changed && decision.allocations.is_some()
    {
        return Err(UpdatePurchaseItemPersistenceError::InvalidDecision);
    }
    if item_id.is_nil() {
        return Err(UpdatePurchaseItemPersistenceError::InvalidDecision);
    }
    Ok(())
}

fn accounting_period(
    value: DateTime<Utc>,
) -> Result<NaiveDate, UpdatePurchaseItemPersistenceError> {
    let tokyo = FixedOffset::east_opt(9 * 60 * 60)
        .ok_or(UpdatePurchaseItemPersistenceError::InvalidDecision)?;
    let local = value.with_timezone(&tokyo);
    NaiveDate::from_ymd_opt(local.year(), local.month(), 1)
        .ok_or(UpdatePurchaseItemPersistenceError::InvalidDecision)
}

fn normalize_merchant(value: &str) -> String {
    value
        .split_whitespace()
        .collect::<Vec<_>>()
        .join(" ")
        .to_lowercase()
}

fn parse_uuid(
    value: &str,
    field: &'static str,
) -> Result<Uuid, UpdatePurchaseItemPersistenceError> {
    Uuid::parse_str(value).map_err(|_| UpdatePurchaseItemPersistenceError::InvalidUuid(field))
}

fn to_u64(value: i64, field: &'static str) -> Result<u64, UpdatePurchaseItemPersistenceError> {
    u64::try_from(value).map_err(|_| UpdatePurchaseItemPersistenceError::NumericOutOfRange(field))
}
