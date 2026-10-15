use std::collections::BTreeSet;

use bookkeeping_workflows::{
    save_purchase_item_edits::{
        SavePurchaseItemEditsCommand, SavePurchaseItemEditsError, decide_save_purchase_item_edits,
    },
    update_purchase::{UpdatePurchaseCommand, UpdatePurchaseDraft, decide_update_purchase},
    update_purchase_item::UpdatePurchaseItemCommand,
};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::{AsyncConnection, RunQueryDsl};
use uuid::Uuid;

use crate::{PgPool, schema::public, update_purchase, update_purchase_item};

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

pub async fn persist_save_purchase_item_edits(
    pool: &PgPool,
    input: PersistSavePurchaseItemEdits,
    items: Vec<UpdatePurchaseItemCommand>,
    purchase_patch: Option<UpdatePurchaseCommand>,
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
    let item_ids = decision
        .item_ids
        .iter()
        .map(|id| parse_uuid(id, "item_id"))
        .collect::<Result<Vec<_>, _>>()?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| SavePurchaseItemEditsPersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<Vec<String>, SavePurchaseItemEditsPersistenceError, _>(async move |conn| {
            use public::household_members::dsl as members;
            use public::purchase_items::dsl as items_table;
            use public::purchases::dsl as purchases;

            let purchase_owner = purchases::purchases
                .filter(purchases::id.eq(purchase_id))
                .select(purchases::household_id)
                .first::<Uuid>(conn)
                .await
                .optional()?
                .ok_or(SavePurchaseItemEditsPersistenceError::PurchaseNotFound)?;
            if purchase_owner != household_id {
                return Err(SavePurchaseItemEditsPersistenceError::HouseholdMismatch);
            }
            let active_member = members::household_members
                .filter(members::household_id.eq(household_id))
                .filter(members::member_id.eq(actor_id))
                .filter(members::left_at.is_null())
                .select(members::member_id)
                .first::<Uuid>(conn)
                .await
                .optional()?
                .is_some();
            if !active_member {
                return Err(SavePurchaseItemEditsPersistenceError::MemberNotFound);
            }
            if item_ids.len() != 0 {
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
            for item_id in item_lock_order {
                let command = items_by_id
                    .remove(&item_id)
                    .ok_or(SavePurchaseItemEditsPersistenceError::ItemNotFound)?;
                update_purchase_item::persist_update_purchase_item_in_transaction(
                    conn,
                    household_id,
                    actor_id,
                    command,
                )
                .await
                .map_err(SavePurchaseItemEditsPersistenceError::InvalidItemUpdate)?;
            }
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
                let current = UpdatePurchaseDraft {
                    revision: u64::try_from(revision).map_err(|_| {
                        SavePurchaseItemEditsPersistenceError::InvalidPurchaseUpdate(
                            update_purchase::UpdatePurchasePersistenceError::NumericOutOfRange(
                                "revision",
                            ),
                        )
                    })?,
                    status,
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
                )
                .await
                .map_err(SavePurchaseItemEditsPersistenceError::InvalidPurchaseUpdate)?;
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
