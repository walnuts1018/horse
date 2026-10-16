use std::collections::BTreeSet;

use bookkeeping_application::{
    save_purchase_item_edits::{
        SavePurchaseItemEditsCommand, SavePurchaseItemEditsError, decide_save_purchase_item_edits,
    },
    update_purchase::{UpdatePurchaseCommand, UpdatePurchaseDraft, decide_update_purchase},
    update_purchase_item::UpdatePurchaseItemCommand,
};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::{AsyncConnection, RunQueryDsl};
use horse_authorization::{Operation, authorize};
use uuid::Uuid;

use crate::{PgPool, schema::public, update_purchase, update_purchase_item};
use household_postgres::household_access;

#[derive(Debug, Clone)]
pub struct PersistSavePurchaseItemEdits {
    pub household_id: String,
    pub actor_id: String,
    pub purchase_id: String,
}

#[derive(Debug)]
pub enum SavePurchaseItemEditsPersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    PurchaseNotFound,
    ItemNotFound,
    HouseholdMismatch,
    MemberNotFound,
    InvalidWorkflow(SavePurchaseItemEditsError),
    InvalidPurchaseUpdate(update_purchase::UpdatePurchasePersistenceError),
    InvalidItemUpdate(update_purchase_item::UpdatePurchaseItemPersistenceError),
}

impl From<DieselError> for SavePurchaseItemEditsPersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_save_purchase_item_edits<
    C: update_purchase_item::UpdatePurchaseItemReferenceData,
>(
    pool: &PgPool,
    input: PersistSavePurchaseItemEdits,
    items: Vec<UpdatePurchaseItemCommand>,
    purchase_patch: Option<UpdatePurchaseCommand>,
    reference_data: &mut C,
) -> Result<Vec<String>, SavePurchaseItemEditsPersistenceError> {
    let decision = decide_save_purchase_item_edits(SavePurchaseItemEditsCommand {
        purchase_id: input.purchase_id.clone(),
        items: items.clone(),
        purchase_patch_present: purchase_patch.is_some(),
    })
    .map_err(SavePurchaseItemEditsPersistenceError::InvalidWorkflow)?;
    let household_id = parse_uuid(&input.household_id, "household_id")?;
    let actor_id = parse_uuid(&input.actor_id, "actor_id")?;
    let purchase_id = parse_uuid(&input.purchase_id, "purchase_id")?;
    if purchase_patch
        .as_ref()
        .is_some_and(|command| command.purchase_id != input.purchase_id)
    {
        return Err(SavePurchaseItemEditsPersistenceError::InvalidUuid(
            "purchase_id",
        ));
    }
    let item_ids = decision
        .item_ids
        .iter()
        .map(|id| parse_uuid(id, "item_id"))
        .collect::<Result<Vec<_>, _>>()?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| SavePurchaseItemEditsPersistenceError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<Vec<String>, SavePurchaseItemEditsPersistenceError, _>(async move |conn| {
            use public::accounting_periods::dsl as periods;
            use public::purchase_items::dsl as items_table;
            use public::purchase_member_burdens::dsl as burdens;
            use public::purchase_payment_funding_snapshots::dsl as payment_snapshots;
            use public::purchase_payments::dsl as payments;
            use public::purchases::dsl as purchases;

            let (purchase_owner, initial_status) = purchases::purchases
                .filter(purchases::id.eq(purchase_id))
                .for_update()
                .select((purchases::household_id, purchases::status))
                .first::<(Uuid, String)>(conn)
                .await
                .optional()?
                .ok_or(SavePurchaseItemEditsPersistenceError::PurchaseNotFound)?;
            if purchase_owner != household_id {
                return Err(SavePurchaseItemEditsPersistenceError::HouseholdMismatch);
            }
            let actor_role =
                household_access::active_member_role(conn, household_id, actor_id).await?;
            if authorize(actor_role.as_deref(), Operation::FinancialData).is_err() {
                return Err(SavePurchaseItemEditsPersistenceError::MemberNotFound);
            }
            let previous_burdens = burdens::purchase_member_burdens
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
                .load::<(Uuid, Uuid, Uuid, i32, i64, Option<String>, String)>(conn)
                .await?;
            let previous_payment_accounts = payments::purchase_payments
                .filter(payments::household_id.eq(household_id))
                .filter(payments::purchase_id.eq(purchase_id))
                .order(payments::line_no)
                .select((payments::account_id, payments::amount_yen))
                .load::<(Uuid, i64)>(conn)
                .await?;
            let previous_payment_snapshots = payment_snapshots::purchase_payment_funding_snapshots
                .filter(payment_snapshots::household_id.eq(household_id))
                .filter(payment_snapshots::purchase_id.eq(purchase_id))
                .select((
                    payment_snapshots::account_id,
                    payment_snapshots::pool_id,
                    payment_snapshots::member_id,
                    payment_snapshots::funding_rule_version,
                    payment_snapshots::funding_weight,
                    payment_snapshots::settlement_policy,
                    payment_snapshots::snapshot_source,
                ))
                .load::<(Uuid, Uuid, Uuid, i32, i64, Option<String>, String)>(conn)
                .await?;
            if !item_ids.is_empty() {
                let matching_ids = items_table::purchase_items
                    .filter(items_table::id.eq_any(&item_ids))
                    .filter(items_table::household_id.eq(household_id))
                    .filter(items_table::purchase_id.eq(purchase_id))
                    .order(items_table::id.asc())
                    .select(items_table::id)
                    .load::<Uuid>(conn)
                    .await?;
                if matching_ids.len() != item_ids.len() {
                    return Err(SavePurchaseItemEditsPersistenceError::ItemNotFound);
                }
            }
            let mut items_by_id =
                items
                    .into_iter()
                    .map(|command| {
                        let id = parse_uuid(&command.item_id, "item_id")?;
                        Ok((id, command))
                    })
                    .collect::<Result<
                        std::collections::BTreeMap<_, _>,
                        SavePurchaseItemEditsPersistenceError,
                    >>()?;
            let item_lock_order = item_ids.iter().copied().collect::<BTreeSet<_>>();
            let mut accounting_changed = false;
            let mut payments_changed = false;
            for item_id in item_lock_order {
                let command = items_by_id
                    .remove(&item_id)
                    .ok_or(SavePurchaseItemEditsPersistenceError::ItemNotFound)?;
                let persisted = update_purchase_item::persist_update_purchase_item_in_transaction(
                    conn,
                    household_id,
                    actor_id,
                    command,
                    true,
                    reference_data,
                )
                .await
                .map_err(SavePurchaseItemEditsPersistenceError::InvalidItemUpdate)?;
                accounting_changed |= persisted.accounting_changed;
                payments_changed |= persisted.payments_changed;
            }
            let purchase_patch_changes_payments = purchase_patch
                .as_ref()
                .is_some_and(|command| command.payment_allocations.is_some());
            if let Some(command) = purchase_patch {
                let (revision, status, occurred_at, date_precision, merchant, field_revisions) =
                    purchases::purchases
                        .filter(purchases::id.eq(purchase_id))
                        .filter(purchases::household_id.eq(household_id))
                        .for_update()
                        .select((
                            purchases::revision,
                            purchases::status,
                            purchases::occurred_at,
                            purchases::date_precision,
                            purchases::merchant,
                            purchases::field_revisions,
                        ))
                        .first::<(
                            i64,
                            String,
                            chrono::DateTime<chrono::Utc>,
                            String,
                            String,
                            serde_json::Value,
                        )>(conn)
                        .await
                        .optional()?
                        .ok_or(SavePurchaseItemEditsPersistenceError::PurchaseNotFound)?;
                let lifecycle = match status.as_str() {
                    "DRAFT" => bookkeeping_application::PurchaseLifecycle::Draft,
                    "POSTED" => bookkeeping_application::PurchaseLifecycle::Posted,
                    _ => {
                        return Err(SavePurchaseItemEditsPersistenceError::InvalidPurchaseUpdate(
                            update_purchase::UpdatePurchasePersistenceError::InvalidDecision(
                                bookkeeping_application::update_purchase::UpdatePurchaseError::NotEditable,
                            ),
                        ));
                    }
                };
                let period_start = update_purchase::period_start(occurred_at)
                    .map_err(SavePurchaseItemEditsPersistenceError::InvalidPurchaseUpdate)?;
                let (period_status, period_revision) = periods::accounting_periods
                    .filter(periods::household_id.eq(household_id))
                    .filter(periods::period_start.eq(period_start))
                    .select((periods::status, periods::revision))
                    .first::<(String, i64)>(conn)
                    .await?;
                let accounting_period = update_purchase::accounting_period(
                    &period_status,
                    period_revision,
                    occurred_at,
                )
                .map_err(SavePurchaseItemEditsPersistenceError::InvalidPurchaseUpdate)?;
                let current = UpdatePurchaseDraft {
                    revision: u64::try_from(revision).map_err(|_| {
                        SavePurchaseItemEditsPersistenceError::InvalidPurchaseUpdate(
                            update_purchase::UpdatePurchasePersistenceError::NumericOutOfRange(
                                "revision",
                            ),
                        )
                    })?,
                    lifecycle,
                    accounting_period,
                    occurred_at: occurred_at.to_rfc3339(),
                    date_precision,
                    merchant,
                    field_revisions: update_purchase::json_revisions(field_revisions)
                        .map_err(SavePurchaseItemEditsPersistenceError::InvalidPurchaseUpdate)?,
                };
                let decision =
                    decide_update_purchase(command.clone(), current).map_err(|error| {
                        SavePurchaseItemEditsPersistenceError::InvalidPurchaseUpdate(
                            update_purchase::UpdatePurchasePersistenceError::InvalidDecision(error),
                        )
                    })?;
                update_purchase::persist_update_purchase_in_transaction(
                    conn,
                    household_id,
                    actor_id,
                    command,
                    decision,
                    accounting_changed,
                    reference_data,
                )
                .await
                .map_err(SavePurchaseItemEditsPersistenceError::InvalidPurchaseUpdate)?;
            }
            if initial_status == "POSTED" && accounting_changed {
                let (revision, occurred_at, merchant) = purchases::purchases
                    .filter(purchases::id.eq(purchase_id))
                    .filter(purchases::household_id.eq(household_id))
                    .select((
                        purchases::revision,
                        purchases::occurred_at,
                        purchases::merchant,
                    ))
                    .first::<(i64, chrono::DateTime<chrono::Utc>, String)>(conn)
                    .await
                    .optional()?
                    .ok_or(SavePurchaseItemEditsPersistenceError::PurchaseNotFound)?;
                update_purchase_item::sync_posted_accounting(
                    conn,
                    update_purchase_item::PostedAccountingSync {
                        household_id,
                        purchase_id,
                        occurred_at,
                        merchant: &merchant,
                        revision,
                        previous_burdens: &previous_burdens,
                        previous_payment_accounts: &previous_payment_accounts,
                        previous_payment_snapshots: &previous_payment_snapshots,
                        payments_changed: payments_changed || purchase_patch_changes_payments,
                    },
                    reference_data,
                )
                .await
                .map_err(SavePurchaseItemEditsPersistenceError::InvalidItemUpdate)?;
            }
            Ok(decision.item_ids)
        })
        .await
}

fn parse_uuid(
    value: &str,
    field: &'static str,
) -> Result<Uuid, SavePurchaseItemEditsPersistenceError> {
    Uuid::parse_str(value).map_err(|_| SavePurchaseItemEditsPersistenceError::InvalidUuid(field))
}
