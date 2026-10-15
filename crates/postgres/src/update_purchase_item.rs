use std::collections::BTreeMap;

use bookkeeping_workflows::update_purchase_item::{
    AllocationBasis, ItemAllocationWeight, ProductLearning, PurchaseItemState,
    UpdatePurchaseItemCommand, UpdatePurchaseItemDecision, UpdatePurchaseItemError,
    decide_update_purchase_item,
};
use bookkeeping_workflows::{
    MemberFundingBasis, MemberId, MemberWeight, PurchaseStatus, SettlementPolicy,
    decide_post_purchase,
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
    InvalidPaymentAllocation,
    InvalidFundingSnapshot,
    MerchantRequired,
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
                        purchases::payment_account_id,
                    ))
                    .first::<(Uuid, String, DateTime<Utc>, String, i64, Option<Uuid>)>(&mut *conn)
                    .await
                    .optional()?
                    .ok_or(UpdatePurchaseItemPersistenceError::PurchaseNotFound)?;
                if purchase.0 != household_id || accounting_period(purchase.2)? != old_period {
                    return Err(UpdatePurchaseItemPersistenceError::HouseholdMismatch);
                }
                if !matches!(purchase.1.as_str(), "DRAFT" | "POSTED") {
                    return Err(UpdatePurchaseItemPersistenceError::InvalidDecision);
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

                let previous_burdens = public::purchase_member_burdens::dsl::purchase_member_burdens
                    .filter(public::purchase_member_burdens::dsl::household_id.eq(household_id))
                    .filter(public::purchase_member_burdens::dsl::purchase_id.eq(observed.1))
                    .select((
                        public::purchase_member_burdens::dsl::item_id,
                        public::purchase_member_burdens::dsl::pool_id,
                        public::purchase_member_burdens::dsl::member_id,
                        public::purchase_member_burdens::dsl::funding_rule_version,
                        public::purchase_member_burdens::dsl::funding_weight,
                        public::purchase_member_burdens::dsl::settlement_policy,
                        public::purchase_member_burdens::dsl::snapshot_source,
                    ))
                    .load::<(Uuid, Uuid, Uuid, i32, i64, Option<String>, String)>(&mut *conn)
                    .await?;
                let previous_payment_snapshots = public::purchase_payment_funding_snapshots::dsl::purchase_payment_funding_snapshots
                    .filter(public::purchase_payment_funding_snapshots::dsl::household_id.eq(household_id))
                    .filter(public::purchase_payment_funding_snapshots::dsl::purchase_id.eq(observed.1))
                    .select((
                        public::purchase_payment_funding_snapshots::dsl::account_id,
                        public::purchase_payment_funding_snapshots::dsl::pool_id,
                        public::purchase_payment_funding_snapshots::dsl::member_id,
                        public::purchase_payment_funding_snapshots::dsl::funding_rule_version,
                        public::purchase_payment_funding_snapshots::dsl::funding_weight,
                        public::purchase_payment_funding_snapshots::dsl::settlement_policy,
                        public::purchase_payment_funding_snapshots::dsl::snapshot_source,
                    ))
                    .load::<(Uuid, Uuid, Uuid, i32, i64, Option<String>, String)>(&mut *conn)
                    .await?;
                let previous_payment_accounts = public::purchase_payments::dsl::purchase_payments
                    .filter(public::purchase_payments::dsl::household_id.eq(household_id))
                    .filter(public::purchase_payments::dsl::purchase_id.eq(observed.1))
                    .order(public::purchase_payments::dsl::line_no)
                    .select((
                        public::purchase_payments::dsl::account_id,
                        public::purchase_payments::dsl::amount_yen,
                    ))
                    .load::<(Uuid, i64)>(&mut *conn)
                    .await?;

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
                    .as_slice()
                    .first()
                    .map(|row| (row.2.map(|id| id.to_string()), row.3))
                    .unwrap_or((None, default_version));
                let current = PurchaseItemState {
                    revision: to_u64(locked.0, "item_revision")?,
                    status: purchase.1.clone(),
                    name: locked.2.clone(),
                    amount_yen: locked.3,
                    category_id: locked.4.map(|id| id.to_string()),
                    product_variant_id: locked.5.map(|id| id.to_string()),
                    quantity: measure(locked.7, locked.8, locked.9.clone())?,
                    printed_unit_price: printed_price(
                        locked.10,
                        locked.11,
                        locked.12,
                        locked.13.clone(),
                    )?,
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
                let next_item_amount = decision.amount_yen.unwrap_or(locked.3);
                let adjusted_payments = if next_item_amount != locked.3 {
                    Some(
                        adjust_purchase_payments(
                            &mut *conn,
                            household_id,
                            observed.1,
                            locked.3,
                            next_item_amount,
                            purchase.5,
                        )
                        .await?,
                    )
                } else {
                    None
                };
                let payment_changed = adjusted_payments
                    .as_ref()
                    .is_some_and(|payments| !payments.is_empty());
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
                            Some(value.numerator),
                            Some(value.denominator),
                            Some(value.unit.clone()),
                        ),
                    };
                let (
                    printed_unit_price_yen,
                    printed_unit_price_quantity_numerator,
                    printed_unit_price_quantity_denominator,
                    printed_unit_price_unit,
                ) =
                    match decision.printed_unit_price.as_ref() {
                        None => (locked.10, locked.11, locked.12, locked.13.clone()),
                        Some(None) => (None, None, None, None),
                        Some(Some(value)) => (
                            Some(value.amount_yen),
                            Some(value.basis.numerator),
                            Some(value.basis.denominator),
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

                persist_product_learning(
                    &mut *conn,
                    household_id,
                    actor_id,
                    observed.1,
                    item_id,
                    &locked.2,
                    locked.5,
                    product_variant_id,
                    command.product_variant_id_set,
                    &purchase.3,
                    decision.product_learning.as_ref(),
                )
                .await?;

                if let Some(payments) = adjusted_payments {
                    diesel::delete(
                        public::purchase_payments::dsl::purchase_payments
                            .filter(public::purchase_payments::dsl::household_id.eq(household_id))
                            .filter(public::purchase_payments::dsl::purchase_id.eq(observed.1)),
                    )
                    .execute(&mut *conn)
                    .await?;
                    for (index, (account_id, amount_yen)) in payments.iter().enumerate() {
                        let line_no = i32::try_from(index + 1).map_err(|_| {
                            UpdatePurchaseItemPersistenceError::NumericOutOfRange("payment_line_no")
                        })?;
                        diesel::insert_into(public::purchase_payments::table)
                            .values((
                                public::purchase_payments::household_id.eq(household_id),
                                public::purchase_payments::purchase_id.eq(observed.1),
                                public::purchase_payments::line_no.eq(line_no),
                                public::purchase_payments::account_id.eq(account_id),
                                public::purchase_payments::amount_yen.eq(amount_yen),
                            ))
                            .execute(&mut *conn)
                            .await?;
                    }
                }

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
                if purchase.1 == "POSTED" && decision.allocations_changed {
                    sync_posted_accounting(
                        &mut *conn,
                        household_id,
                        observed.1,
                        purchase.2,
                        &purchase.3,
                        purchase.4,
                        &previous_burdens,
                        &previous_payment_accounts,
                        &previous_payment_snapshots,
                        payment_changed,
                    )
                    .await?;
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
    macro_rules! find_rule {
        ($condition:expr) => {
            rules::allocation_rules
                .filter(rules::household_id.eq(household_id))
                .filter($condition)
                .select((rules::id, rules::revision))
                .first::<(Uuid, i64)>(conn)
                .await
                .optional()?
        };
    }
    let mut rule = None;
    if let Some(id) = variant_id {
        rule = find_rule!(rules::product_variant_id.eq(Some(id)));
    }
    if rule.is_none() {
        if let Some(id) = concept_id {
            rule = find_rule!(rules::product_concept_id.eq(Some(id)));
        }
    }
    if rule.is_none() {
        if let Some(id) = category_id {
            rule = find_rule!(rules::category_id.eq(Some(id)));
        }
    }
    if rule.is_none() && !merchant.trim().is_empty() {
        rule = find_rule!(rules::merchant_key.eq(Some(normalize_merchant(merchant))));
    }
    let Some((rule_id, revision)) = rule else {
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

async fn adjust_purchase_payments(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    purchase_id: Uuid,
    old_item_amount: i64,
    new_item_amount: i64,
    legacy_account_id: Option<Uuid>,
) -> Result<Vec<(Uuid, i64)>, UpdatePurchaseItemPersistenceError> {
    let item_amounts = public::purchase_items::dsl::purchase_items
        .filter(public::purchase_items::dsl::household_id.eq(household_id))
        .filter(public::purchase_items::dsl::purchase_id.eq(purchase_id))
        .select(public::purchase_items::dsl::amount_yen)
        .load::<i64>(conn)
        .await?;
    let current_total = item_amounts
        .into_iter()
        .try_fold(0_i64, |total, amount| total.checked_add(amount))
        .ok_or(UpdatePurchaseItemPersistenceError::InvalidPaymentAllocation)?;
    let new_total = current_total
        .checked_sub(old_item_amount)
        .and_then(|total| total.checked_add(new_item_amount))
        .ok_or(UpdatePurchaseItemPersistenceError::InvalidPaymentAllocation)?;
    let rows = public::purchase_payments::dsl::purchase_payments
        .filter(public::purchase_payments::dsl::household_id.eq(household_id))
        .filter(public::purchase_payments::dsl::purchase_id.eq(purchase_id))
        .order(public::purchase_payments::dsl::line_no)
        .select((
            public::purchase_payments::dsl::account_id,
            public::purchase_payments::dsl::amount_yen,
        ))
        .load::<(Uuid, i64)>(conn)
        .await?;
    let payments = if rows.is_empty() {
        legacy_account_id
            .map(|account_id| vec![(account_id, current_total)])
            .unwrap_or_default()
    } else {
        rows
    };
    if payments.is_empty() {
        return Ok(Vec::new());
    }
    let paid_total = payments
        .iter()
        .try_fold(0_i64, |total, (_, amount)| total.checked_add(*amount))
        .ok_or(UpdatePurchaseItemPersistenceError::InvalidPaymentAllocation)?;
    if paid_total != current_total
        || payments.iter().any(|(_, amount)| *amount <= 0)
        || new_total < 0
    {
        return Err(UpdatePurchaseItemPersistenceError::InvalidPaymentAllocation);
    }
    let denominator = u128::try_from(paid_total)
        .map_err(|_| UpdatePurchaseItemPersistenceError::InvalidPaymentAllocation)?;
    let target = u128::try_from(new_total)
        .map_err(|_| UpdatePurchaseItemPersistenceError::InvalidPaymentAllocation)?;
    let mut apportioned = payments
        .iter()
        .enumerate()
        .map(|(index, (account_id, amount))| {
            let product = target * (*amount as u128);
            (
                *account_id,
                (product / denominator) as i64,
                product % denominator,
                index,
            )
        })
        .collect::<Vec<_>>();
    let assigned = apportioned
        .iter()
        .map(|(_, amount, _, _)| *amount)
        .sum::<i64>();
    let remainder = usize::try_from(new_total - assigned)
        .map_err(|_| UpdatePurchaseItemPersistenceError::InvalidPaymentAllocation)?;
    let mut priority = (0..apportioned.len()).collect::<Vec<_>>();
    priority.sort_by(|left, right| {
        apportioned[*right]
            .2
            .cmp(&apportioned[*left].2)
            .then_with(|| payments[*left].0.cmp(&payments[*right].0))
    });
    for index in priority.into_iter().take(remainder) {
        apportioned[index].1 += 1;
    }
    Ok(apportioned
        .into_iter()
        .filter_map(|(account_id, amount, _, _)| (amount > 0).then_some((account_id, amount)))
        .collect())
}

async fn sync_posted_accounting(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    purchase_id: Uuid,
    occurred_at: DateTime<Utc>,
    merchant: &str,
    revision: i64,
    previous_burdens: &[(Uuid, Uuid, Uuid, i32, i64, Option<String>, String)],
    previous_payment_accounts: &[(Uuid, i64)],
    previous_payment_snapshots: &[(Uuid, Uuid, Uuid, i32, i64, Option<String>, String)],
    payments_changed: bool,
) -> Result<(), UpdatePurchaseItemPersistenceError> {
    let mut burden_basis_rows =
        BTreeMap::<(Uuid, Uuid), Vec<(Uuid, i32, i64, Option<String>)>>::new();
    let burden_sources = previous_burdens
        .iter()
        .map(|(item_id, pool_id, member_id, _, _, _, source)| {
            ((*item_id, *pool_id, *member_id), source.clone())
        })
        .collect::<BTreeMap<_, _>>();
    let payment_sources = previous_payment_snapshots
        .iter()
        .map(|(account_id, pool_id, member_id, _, _, _, source)| {
            ((*account_id, *pool_id, *member_id), source.clone())
        })
        .collect::<BTreeMap<_, _>>();
    for (item_id, pool_id, member_id, version, weight, policy, _) in previous_burdens {
        burden_basis_rows
            .entry((*item_id, *pool_id))
            .or_default()
            .push((*member_id, *version, *weight, policy.clone()));
    }
    let burden_bases = burden_basis_rows
        .into_iter()
        .map(|(key, rows)| Ok((key, funding_basis(rows)?)))
        .collect::<Result<BTreeMap<_, _>, UpdatePurchaseItemPersistenceError>>()?;
    let mut payment_basis_rows =
        BTreeMap::<(Uuid, Uuid), Vec<(Uuid, i32, i64, Option<String>)>>::new();
    for (account_id, pool_id, member_id, version, weight, policy, _) in previous_payment_snapshots {
        payment_basis_rows
            .entry((*account_id, *pool_id))
            .or_default()
            .push((*member_id, *version, *weight, policy.clone()));
    }
    let payment_bases = payment_basis_rows
        .into_iter()
        .map(|(key, rows)| Ok((key, funding_basis(rows)?)))
        .collect::<Result<BTreeMap<_, _>, UpdatePurchaseItemPersistenceError>>()?;
    let previous_account_ids = previous_payment_accounts
        .iter()
        .map(|(account_id, _)| *account_id)
        .collect::<std::collections::BTreeSet<_>>();

    let loaded = crate::post_purchase_loader::load_post_purchase_command(
        conn,
        &household_id.to_string(),
        &purchase_id.to_string(),
        to_u64(revision, "purchase_revision")?,
    )
    .await
    .map_err(|error| match error {
        crate::post_purchase_loader::PostPurchaseLoadError::Database(error) => {
            UpdatePurchaseItemPersistenceError::Database(error)
        }
        _ => UpdatePurchaseItemPersistenceError::InvalidDecision,
    })?;
    let mut command = loaded.command;
    command.draft.status = PurchaseStatus::Draft;
    command.expected_revision = command.draft.revision;
    for allocation in &mut command.allocations {
        let item_id = parse_uuid(allocation.item_id.as_str(), "allocation_item_id")?;
        let pool_id = parse_uuid(allocation.pool_id.as_str(), "allocation_pool_id")?;
        if let Some(basis) = burden_bases.get(&(item_id, pool_id)) {
            allocation.funding = basis.clone();
        }
    }
    for payment in &mut command.payments {
        let account_id = parse_uuid(payment.account_id.as_str(), "payment_account_id")?;
        let pool_id = parse_uuid(payment.funding.pool_id.as_str(), "payment_pool_id")?;
        if let Some(basis) = payment_bases.get(&(account_id, pool_id)).or_else(|| {
            payment_bases
                .iter()
                .find(|((_, saved_pool), _)| *saved_pool == pool_id)
                .map(|(_, basis)| basis)
        }) {
            payment.funding.rule = basis.clone();
        } else if payments_changed && previous_account_ids.contains(&account_id) {
            return Err(UpdatePurchaseItemPersistenceError::InvalidFundingSnapshot);
        }
    }
    let decision = decide_post_purchase(command)
        .map_err(|_| UpdatePurchaseItemPersistenceError::InvalidDecision)?;

    let journals = public::journals::dsl::journals
        .filter(public::journals::dsl::household_id.eq(household_id))
        .filter(public::journals::dsl::source_kind.eq_any(["purchase", "PURCHASE"]))
        .filter(public::journals::dsl::source_id.eq(purchase_id))
        .select(public::journals::dsl::id)
        .load::<Uuid>(conn)
        .await?;
    if journals.len() != 1 {
        return Err(UpdatePurchaseItemPersistenceError::InvalidDecision);
    }
    let journal_id = journals[0];
    diesel::update(
        public::journals::dsl::journals.filter(public::journals::dsl::id.eq(journal_id)),
    )
    .set(public::journals::dsl::occurred_at.eq(occurred_at))
    .execute(&mut *conn)
    .await?;
    diesel::delete(
        public::journal_entries::dsl::journal_entries
            .filter(public::journal_entries::dsl::household_id.eq(household_id))
            .filter(public::journal_entries::dsl::journal_id.eq(journal_id)),
    )
    .execute(&mut *conn)
    .await?;
    for line in &decision.journal_lines {
        let line_no = i32::try_from(line.line_no).map_err(|_| {
            UpdatePurchaseItemPersistenceError::NumericOutOfRange("journal_line_no")
        })?;
        let amount = line.amount.value();
        diesel::insert_into(public::journal_entries::table)
            .values((
                public::journal_entries::household_id.eq(household_id),
                public::journal_entries::journal_id.eq(journal_id),
                public::journal_entries::line_no.eq(line_no),
                public::journal_entries::account_id.eq(line
                    .account_id
                    .as_ref()
                    .map(|id| parse_uuid(id.as_str(), "account_id"))
                    .transpose()?),
                public::journal_entries::pool_id.eq(line
                    .pool_id
                    .as_ref()
                    .map(|id| parse_uuid(id.as_str(), "pool_id"))
                    .transpose()?),
                public::journal_entries::member_id.eq::<Option<Uuid>>(None),
                public::journal_entries::debit_yen.eq(
                    if line.side == bookkeeping_workflows::Side::Debit {
                        amount
                    } else {
                        0
                    },
                ),
                public::journal_entries::credit_yen.eq(
                    if line.side == bookkeeping_workflows::Side::Credit {
                        amount
                    } else {
                        0
                    },
                ),
                public::journal_entries::memo.eq(line.memo),
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
    for snapshot in &decision.member_burden_snapshots {
        let item_id = parse_uuid(snapshot.item_id.as_str(), "item_id")?;
        let pool_id = parse_uuid(snapshot.pool_id.as_str(), "pool_id")?;
        let member_id = parse_uuid(snapshot.member_id.as_str(), "member_id")?;
        let source = burden_sources
            .get(&(item_id, pool_id, member_id))
            .map(String::as_str)
            .unwrap_or("POSTED");
        diesel::insert_into(public::purchase_member_burdens::table)
            .values((
                public::purchase_member_burdens::household_id.eq(household_id),
                public::purchase_member_burdens::purchase_id.eq(purchase_id),
                public::purchase_member_burdens::item_id.eq(item_id),
                public::purchase_member_burdens::pool_id.eq(pool_id),
                public::purchase_member_burdens::member_id.eq(member_id),
                public::purchase_member_burdens::amount_yen.eq(snapshot.amount.value()),
                public::purchase_member_burdens::funding_rule_version.eq(i32::try_from(
                    snapshot.funding_rule_version,
                )
                .map_err(|_| {
                    UpdatePurchaseItemPersistenceError::NumericOutOfRange("funding_rule_version")
                })?),
                public::purchase_member_burdens::funding_weight.eq(i64::try_from(
                    snapshot.funding_weight,
                )
                .map_err(|_| {
                    UpdatePurchaseItemPersistenceError::NumericOutOfRange("funding_weight")
                })?),
                public::purchase_member_burdens::settlement_policy
                    .eq(snapshot.settlement_policy.as_deref()),
                public::purchase_member_burdens::snapshot_source.eq(source),
            ))
            .execute(&mut *conn)
            .await?;
    }
    if payments_changed {
        diesel::delete(
            public::purchase_payment_funding_snapshots::dsl::purchase_payment_funding_snapshots
                .filter(
                    public::purchase_payment_funding_snapshots::dsl::household_id.eq(household_id),
                )
                .filter(
                    public::purchase_payment_funding_snapshots::dsl::purchase_id.eq(purchase_id),
                ),
        )
        .execute(&mut *conn)
        .await?;
    }
    for snapshot in decision
        .payment_funding_snapshots
        .iter()
        .filter(|_| payments_changed)
    {
        let account_id = parse_uuid(snapshot.account_id.as_str(), "account_id")?;
        let pool_id = parse_uuid(snapshot.pool_id.as_str(), "pool_id")?;
        let member_id = parse_uuid(snapshot.member_id.as_str(), "member_id")?;
        let source = payment_sources
            .get(&(account_id, pool_id, member_id))
            .or_else(|| {
                payment_sources
                    .iter()
                    .find(|((_, saved_pool, saved_member), _)| {
                        *saved_pool == pool_id && *saved_member == member_id
                    })
                    .map(|(_, source)| source)
            })
            .map(String::as_str)
            .unwrap_or("POSTED");
        diesel::insert_into(public::purchase_payment_funding_snapshots::table)
            .values((
                public::purchase_payment_funding_snapshots::household_id.eq(household_id),
                public::purchase_payment_funding_snapshots::purchase_id.eq(purchase_id),
                public::purchase_payment_funding_snapshots::payment_line_no.eq(i32::try_from(
                    snapshot.line_no,
                )
                .map_err(|_| {
                    UpdatePurchaseItemPersistenceError::NumericOutOfRange("payment_line_no")
                })?),
                public::purchase_payment_funding_snapshots::account_id.eq(account_id),
                public::purchase_payment_funding_snapshots::pool_id.eq(pool_id),
                public::purchase_payment_funding_snapshots::member_id.eq(member_id),
                public::purchase_payment_funding_snapshots::funding_rule_version.eq(i32::try_from(
                    snapshot.funding_rule_version,
                )
                .map_err(|_| {
                    UpdatePurchaseItemPersistenceError::NumericOutOfRange("funding_rule_version")
                })?),
                public::purchase_payment_funding_snapshots::funding_weight.eq(i64::try_from(
                    snapshot.funding_weight,
                )
                .map_err(|_| {
                    UpdatePurchaseItemPersistenceError::NumericOutOfRange("funding_weight")
                })?),
                public::purchase_payment_funding_snapshots::settlement_policy
                    .eq(snapshot.settlement_policy.as_deref()),
                public::purchase_payment_funding_snapshots::snapshot_source.eq(source),
            ))
            .execute(&mut *conn)
            .await?;
    }
    let amount = decision
        .journal_lines
        .iter()
        .filter(|line| line.side == bookkeeping_workflows::Side::Debit)
        .try_fold(0_i64, |sum, line| sum.checked_add(line.amount.value()))
        .ok_or(UpdatePurchaseItemPersistenceError::NumericOutOfRange(
            "ledger_amount",
        ))?;
    let updated_ledgers = diesel::update(
        public::ledger_entries::dsl::ledger_entries
            .filter(public::ledger_entries::dsl::household_id.eq(household_id))
            .filter(public::ledger_entries::dsl::source_kind.eq_any(["purchase", "PURCHASE"]))
            .filter(public::ledger_entries::dsl::source_id.eq(purchase_id)),
    )
    .set((
        public::ledger_entries::dsl::occurred_at.eq(occurred_at),
        public::ledger_entries::dsl::description.eq(merchant),
        public::ledger_entries::dsl::amount_yen.eq(amount),
    ))
    .execute(&mut *conn)
    .await?;
    if updated_ledgers == 0 {
        return Err(UpdatePurchaseItemPersistenceError::InvalidDecision);
    }
    Ok(())
}

fn funding_basis(
    rows: Vec<(Uuid, i32, i64, Option<String>)>,
) -> Result<MemberFundingBasis, UpdatePurchaseItemPersistenceError> {
    let Some((_, version, _, policy)) = rows.as_slice().first() else {
        return Err(UpdatePurchaseItemPersistenceError::InvalidFundingSnapshot);
    };
    if rows
        .iter()
        .any(|(_, row_version, _, row_policy)| row_version != version || row_policy != policy)
    {
        return Err(UpdatePurchaseItemPersistenceError::InvalidFundingSnapshot);
    }
    let version = u32::try_from(*version)
        .map_err(|_| UpdatePurchaseItemPersistenceError::InvalidFundingSnapshot)?;
    if let Some(policy) = policy {
        let policy = parse_policy(policy)?;
        let weights = rows
            .into_iter()
            .map(|(member_id, _, weight, _)| {
                Ok(MemberWeight::new(
                    MemberId::new(member_id.to_string())
                        .map_err(|_| UpdatePurchaseItemPersistenceError::InvalidFundingSnapshot)?,
                    u64::try_from(weight)
                        .map_err(|_| UpdatePurchaseItemPersistenceError::InvalidFundingSnapshot)?,
                )
                .map_err(|_| UpdatePurchaseItemPersistenceError::InvalidFundingSnapshot)?)
            })
            .collect::<Result<Vec<_>, UpdatePurchaseItemPersistenceError>>()?;
        MemberFundingBasis::shared(version, weights, policy)
            .map_err(|_| UpdatePurchaseItemPersistenceError::InvalidFundingSnapshot)
    } else {
        if rows.len() != 1 || rows[0].2 != 1 {
            return Err(UpdatePurchaseItemPersistenceError::InvalidFundingSnapshot);
        }
        MemberFundingBasis::personal(
            version,
            MemberId::new(rows[0].0.to_string())
                .map_err(|_| UpdatePurchaseItemPersistenceError::InvalidFundingSnapshot)?,
        )
        .map_err(|_| UpdatePurchaseItemPersistenceError::InvalidFundingSnapshot)
    }
}

fn parse_policy(value: &str) -> Result<SettlementPolicy, UpdatePurchaseItemPersistenceError> {
    match value {
        "BALANCE_TO_STANDARD" => Ok(SettlementPolicy::BalanceToStandard),
        "REIMBURSE_PERSONAL" => Ok(SettlementPolicy::ReimbursePersonal),
        _ => Err(UpdatePurchaseItemPersistenceError::InvalidFundingSnapshot),
    }
}

fn measure(
    numerator: Option<i64>,
    denominator: Option<i64>,
    unit: Option<String>,
) -> Result<
    Option<bookkeeping_workflows::create_purchase::Measure>,
    UpdatePurchaseItemPersistenceError,
> {
    match (numerator, denominator, unit) {
        (None, None, None) => Ok(None),
        (Some(numerator), Some(denominator), Some(unit)) => {
            Ok(Some(bookkeeping_workflows::create_purchase::Measure {
                numerator,
                denominator,
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
                basis: bookkeeping_workflows::create_purchase::Measure {
                    numerator,
                    denominator,
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

async fn persist_product_learning(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    actor_id: Uuid,
    purchase_id: Uuid,
    item_id: Uuid,
    raw_name: &str,
    previous_variant_id: Option<Uuid>,
    variant_id: Option<Uuid>,
    variant_changed: bool,
    merchant: &str,
    learning: Option<&ProductLearning>,
) -> Result<(), UpdatePurchaseItemPersistenceError> {
    use public::household_events::dsl as events;
    use public::product_aliases::dsl as aliases;
    use public::product_corrections::dsl as corrections;
    use public::product_variants::dsl as variants;

    if let (Some(ProductLearning::VariantName(name)), Some(variant_id)) = (learning, variant_id) {
        let name = name.trim();
        let normalized_name = normalize_product_name(name);
        if normalized_name.is_empty() || name.len() > 240 {
            return Err(UpdatePurchaseItemPersistenceError::InvalidDecision);
        }
        let revision = diesel::update(
            variants::product_variants
                .filter(variants::household_id.eq(household_id))
                .filter(variants::id.eq(variant_id)),
        )
        .set((
            variants::name.eq(name),
            variants::normalized_name.eq(normalized_name),
            variants::revision.eq(variants::revision + 1),
            variants::updated_at.eq(now),
        ))
        .returning(variants::revision)
        .get_result::<i64>(conn)
        .await?;
        diesel::insert_into(events::household_events)
            .values((
                events::household_id.eq(household_id),
                events::event_type.eq("product.variant.renamed"),
                events::entity_kind.eq("product_variant"),
                events::entity_id.eq(variant_id),
                events::revision.eq(revision),
                events::changed_fields.eq(vec![Some("name".to_owned())]),
                events::actor_id.eq(actor_id),
            ))
            .execute(conn)
            .await?;
    }

    if !variant_changed {
        return Ok(());
    }
    let Some(variant_id) = variant_id else {
        return Ok(());
    };
    let merchant_key = normalize_merchant(merchant);
    let scope = match learning {
        Some(ProductLearning::VariantName(_)) => "VARIANT_NAME",
        Some(ProductLearning::MerchantAlias) => "MERCHANT_ALIAS",
        Some(ProductLearning::ItemOnly) | None => "ITEM_ONLY",
    };
    if scope == "MERCHANT_ALIAS" {
        if merchant_key.is_empty() {
            return Err(UpdatePurchaseItemPersistenceError::MerchantRequired);
        }
        let normalized_alias = normalize_product_name(raw_name);
        let alias = diesel::insert_into(aliases::product_aliases)
            .values((
                aliases::id.eq(Uuid::now_v7()),
                aliases::household_id.eq(household_id),
                aliases::variant_id.eq(variant_id),
                aliases::merchant_key.eq(&merchant_key),
                aliases::alias_text.eq(raw_name),
                aliases::normalized_alias.eq(normalized_alias),
            ))
            .on_conflict((
                aliases::household_id,
                aliases::merchant_key,
                aliases::normalized_alias,
            ))
            .do_update()
            .set((
                aliases::variant_id.eq(variant_id),
                aliases::alias_text.eq(raw_name),
                aliases::revision.eq(aliases::revision + 1),
                aliases::updated_at.eq(now),
            ))
            .returning((aliases::id, aliases::revision))
            .get_result::<(Uuid, i64)>(conn)
            .await?;
        diesel::insert_into(events::household_events)
            .values((
                events::household_id.eq(household_id),
                events::event_type.eq("product.alias.learned"),
                events::entity_kind.eq("product_alias"),
                events::entity_id.eq(alias.0),
                events::revision.eq(alias.1),
                events::changed_fields.eq(vec![
                    Some("alias".to_owned()),
                    Some("variantId".to_owned()),
                    Some("merchant".to_owned()),
                ]),
                events::actor_id.eq(actor_id),
            ))
            .execute(conn)
            .await?;
    }

    diesel::insert_into(corrections::product_corrections)
        .values((
            corrections::id.eq(Uuid::now_v7()),
            corrections::household_id.eq(household_id),
            corrections::purchase_id.eq(purchase_id),
            corrections::item_id.eq(item_id),
            corrections::raw_name.eq(raw_name),
            corrections::previous_product_variant_id.eq(previous_variant_id),
            corrections::product_variant_id.eq(variant_id),
            corrections::learning_scope.eq(scope),
            corrections::merchant_key.eq(merchant_key),
            corrections::actor_id.eq(actor_id),
        ))
        .execute(conn)
        .await?;
    Ok(())
}

fn normalize_product_name(value: &str) -> String {
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
