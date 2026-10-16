use async_graphql::{Context, ID, InputObject, MaybeUndefined, Result, SimpleObject, Union};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use bookkeeping_domain::purchase_commands::{UpdatePurchaseCommand, UpdatePurchasePayment};
use bookkeeping_postgres::{
    bulk_update_purchases,
    bulk_update_purchases::PersistBulkUpdatePurchases,
    save_purchase_item_edits::{self, PersistSavePurchaseItemEdits},
};
use cross_context_postgres::purchase_command_result;
use horse_postgres_common::PgPool;
use household_postgres::household_access;
use uuid::Uuid;

use crate::{DateTime, Long, MutationErrorCode, purchase_edit_mutations, purchase_mutations};

#[derive(Clone, Debug, InputObject)]
pub struct BulkUpdatePurchasesInput {
    pub targets: Vec<PurchaseUpdateTargetInput>,
}

#[derive(Clone, Debug, InputObject)]
pub struct PurchaseUpdateTargetInput {
    pub purchase_id: ID,
    pub base_revision: i32,
    pub patch: BulkPurchasePatch,
}

#[derive(Clone, Debug, InputObject)]
pub struct BulkPurchasePatch {
    pub occurred_at: MaybeUndefined<DateTime>,
    pub date_precision: MaybeUndefined<purchase_mutations::PurchaseDatePrecision>,
    pub merchant: MaybeUndefined<String>,
    pub payment_allocations: MaybeUndefined<Vec<BulkUpdatePurchasePaymentInput>>,
}

#[derive(Clone, Debug, InputObject)]
pub struct BulkUpdatePurchasePaymentInput {
    pub payment_account_id: ID,
    pub amount_yen: Long,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct BulkUpdatePurchasesSuccess {
    pub purchases: Vec<purchase_mutations::PurchaseObject>,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct BulkUpdatePurchasesUserError {
    pub code: MutationErrorCode,
    pub message: String,
}

#[derive(Union)]
pub enum BulkUpdatePurchasesResult {
    Success(BulkUpdatePurchasesSuccess),
    UserError(BulkUpdatePurchasesUserError),
}

#[derive(Clone, Debug, InputObject)]
pub struct SavePurchaseItemEditsInput {
    pub purchase_id: ID,
    pub purchase_base_revision: i32,
    pub purchase_patch: MaybeUndefined<purchase_edit_mutations::PurchasePatch>,
    pub items: Vec<purchase_edit_mutations::UpdatePurchaseItemInput>,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct SavePurchaseItemEditsSuccess {
    pub items: Vec<purchase_mutations::PurchaseItem>,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct SavePurchaseItemEditsUserError {
    pub code: MutationErrorCode,
    pub message: String,
}

#[derive(Union)]
pub enum SavePurchaseItemEditsResult {
    Success(SavePurchaseItemEditsSuccess),
    UserError(SavePurchaseItemEditsUserError),
}

pub async fn bulk_update_purchases(
    ctx: &Context<'_>,
    input: BulkUpdatePurchasesInput,
) -> Result<BulkUpdatePurchasesResult> {
    let actor = crate::actor(ctx)?;
    let pool = ctx.data::<PgPool>()?;
    let commands = match bulk_update_commands(input.targets) {
        Ok(commands) => commands,
        Err(()) => return Ok(bulk_update_error(MutationErrorCode::BadInput).into()),
    };
    let Some(first_id) = commands
        .first()
        .and_then(|command| Uuid::parse_str(&command.purchase_id).ok())
    else {
        return Ok(bulk_update_error(MutationErrorCode::BadInput).into());
    };
    let household_id = match purchase_command_result::load_purchase_household_id(pool, first_id)
        .await
        .map_err(crate::internal_error)?
    {
        Some(id) => id,
        None => return Ok(bulk_update_error(MutationErrorCode::NotFound).into()),
    };
    if !household_access::is_active_member(pool, household_id, actor.0)
        .await
        .map_err(crate::internal_error)?
    {
        return Ok(bulk_update_error(MutationErrorCode::NotFound).into());
    }
    match cross_context_postgres::reference_data::persist_bulk_update_purchases(
        pool,
        PersistBulkUpdatePurchases {
            household_id: household_id.to_string(),
            actor_id: actor.0.to_string(),
        },
        commands,
    )
    .await
    {
        Ok(ids) => {
            let mut purchases = Vec::with_capacity(ids.len());
            for id in ids {
                purchases.push(purchase_mutations::purchase_result(pool, household_id, &id).await?);
            }
            Ok(BulkUpdatePurchasesSuccess { purchases }.into())
        }
        Err(error) => match bulk_update_persistence_error(&error) {
            Some(code) => Ok(bulk_update_error(code).into()),
            None => Err(crate::internal_error(error)),
        },
    }
}

pub async fn save_purchase_item_edits(
    ctx: &Context<'_>,
    input: SavePurchaseItemEditsInput,
) -> Result<SavePurchaseItemEditsResult> {
    let actor = crate::actor(ctx)?;
    let purchase_id = match decode_node_id(&input.purchase_id, "Purchase") {
        Ok(id) => id,
        Err(()) => return Ok(save_edits_error(MutationErrorCode::BadInput).into()),
    };
    let pool = ctx.data::<PgPool>()?;
    let household_id = match purchase_command_result::load_purchase_household_id(pool, purchase_id)
        .await
        .map_err(crate::internal_error)?
    {
        Some(id) => id,
        None => return Ok(save_edits_error(MutationErrorCode::NotFound).into()),
    };
    if !household_access::is_active_member(pool, household_id, actor.0)
        .await
        .map_err(crate::internal_error)?
    {
        return Ok(save_edits_error(MutationErrorCode::NotFound).into());
    }
    let base_revision = match u64::try_from(input.purchase_base_revision) {
        Ok(value) if value > 0 => value,
        _ => return Ok(save_edits_error(MutationErrorCode::BadInput).into()),
    };
    let items = match input
        .items
        .into_iter()
        .map(|item| {
            let base_revision = u64::try_from(item.base_revision).map_err(|_| ())?;
            let item_id = decode_node_id(&item.item_id, "PurchaseItem")?;
            purchase_edit_mutations::item_update_command(item, item_id, base_revision)
        })
        .collect::<std::result::Result<Vec<_>, _>>()
    {
        Ok(items) => items,
        Err(()) => return Ok(save_edits_error(MutationErrorCode::BadInput).into()),
    };
    let purchase_patch = match input.purchase_patch {
        MaybeUndefined::Undefined => None,
        MaybeUndefined::Null => return Ok(save_edits_error(MutationErrorCode::BadInput).into()),
        MaybeUndefined::Value(patch) => {
            match purchase_edit_mutations::update_purchase_command(
                purchase_edit_mutations::UpdatePurchaseInput {
                    purchase_id: input.purchase_id.clone(),
                    base_revision: input.purchase_base_revision,
                    patch,
                },
                purchase_id,
            ) {
                Ok(mut command) => {
                    command.base_revision = base_revision;
                    Some(command)
                }
                Err(()) => return Ok(save_edits_error(MutationErrorCode::BadInput).into()),
            }
        }
    };
    match cross_context_postgres::reference_data::persist_save_purchase_item_edits(
        pool,
        PersistSavePurchaseItemEdits {
            household_id: household_id.to_string(),
            actor_id: actor.0.to_string(),
            purchase_id: purchase_id.to_string(),
        },
        items,
        purchase_patch,
    )
    .await
    {
        Ok(item_ids) => {
            let purchase =
                purchase_mutations::purchase_result(pool, household_id, &purchase_id.to_string())
                    .await?;
            let items_by_id = purchase
                .items
                .into_iter()
                .map(|item| (item.id.to_string(), item))
                .collect::<std::collections::BTreeMap<_, _>>();
            let items = item_ids
                .into_iter()
                .map(|id| {
                    let node_id = encode_node_id(
                        "PurchaseItem",
                        Uuid::parse_str(&id).map_err(crate::internal_error)?,
                    );
                    items_by_id.get(node_id.as_str()).cloned().ok_or_else(|| {
                        crate::internal_error("updated purchase item missing from result")
                    })
                })
                .collect::<Result<Vec<_>>>()?;
            Ok(SavePurchaseItemEditsSuccess { items }.into())
        }
        Err(error) => match save_edits_persistence_error(&error) {
            Some(code) => Ok(save_edits_error(code).into()),
            None => Err(crate::internal_error(error)),
        },
    }
}

fn bulk_update_commands(
    targets: Vec<PurchaseUpdateTargetInput>,
) -> std::result::Result<Vec<UpdatePurchaseCommand>, ()> {
    targets
        .into_iter()
        .map(|target| {
            let purchase_id = decode_node_id(&target.purchase_id, "Purchase")?;
            let mut changed_fields = Vec::new();
            let occurred_at = match target.patch.occurred_at {
                MaybeUndefined::Undefined => None,
                MaybeUndefined::Null => return Err(()),
                MaybeUndefined::Value(value) => {
                    value.parse_utc().map_err(|_| ())?;
                    changed_fields.push("occurredAt".to_owned());
                    Some(value.as_str().to_owned())
                }
            };
            let date_precision = match target.patch.date_precision {
                MaybeUndefined::Undefined => None,
                MaybeUndefined::Null => return Err(()),
                MaybeUndefined::Value(value) => {
                    if occurred_at.is_none() {
                        return Err(());
                    }
                    Some(
                        match value {
                            purchase_mutations::PurchaseDatePrecision::DateTime => "DATE_TIME",
                            purchase_mutations::PurchaseDatePrecision::DateOnly => "DATE_ONLY",
                        }
                        .to_owned(),
                    )
                }
            };
            let merchant = match target.patch.merchant {
                MaybeUndefined::Undefined => None,
                MaybeUndefined::Null => return Err(()),
                MaybeUndefined::Value(value) => {
                    changed_fields.push("merchant".to_owned());
                    Some(value)
                }
            };
            let payment_allocations = match target.patch.payment_allocations {
                MaybeUndefined::Undefined => None,
                MaybeUndefined::Null => return Err(()),
                MaybeUndefined::Value(values) => {
                    changed_fields.push("paymentAllocations".to_owned());
                    Some(
                        values
                            .into_iter()
                            .map(|payment| {
                                Ok(UpdatePurchasePayment {
                                    account_id: decode_node_id(
                                        &payment.payment_account_id,
                                        "Account",
                                    )?
                                    .to_string(),
                                    amount_yen: payment.amount_yen.0,
                                })
                            })
                            .collect::<std::result::Result<Vec<_>, ()>>()?,
                    )
                }
            };
            Ok(UpdatePurchaseCommand {
                purchase_id: purchase_id.to_string(),
                base_revision: u64::try_from(target.base_revision).map_err(|_| ())?,
                changed_fields,
                occurred_at,
                date_precision,
                merchant,
                payment_allocations,
            })
        })
        .collect()
}

fn decode_node_id(value: &ID, expected_kind: &str) -> std::result::Result<Uuid, ()> {
    let bytes = URL_SAFE_NO_PAD.decode(value.as_str()).map_err(|_| ())?;
    let separator = bytes.iter().position(|byte| *byte == b':').ok_or(())?;
    let (kind, payload) = bytes.split_at(separator);
    let id = &payload[1..];
    if kind != expected_kind.as_bytes() || id.len() != 16 {
        return Err(());
    }
    let id = Uuid::from_slice(id).map_err(|_| ())?;
    if id.is_nil() {
        return Err(());
    }
    Ok(id)
}

fn encode_node_id(kind: &str, id: Uuid) -> ID {
    let mut bytes = Vec::with_capacity(kind.len() + 17);
    bytes.extend_from_slice(kind.as_bytes());
    bytes.push(b':');
    bytes.extend_from_slice(id.as_bytes());
    ID(URL_SAFE_NO_PAD.encode(bytes))
}

fn bulk_update_error(code: MutationErrorCode) -> BulkUpdatePurchasesUserError {
    BulkUpdatePurchasesUserError {
        code,
        message: error_message(code),
    }
}

fn save_edits_error(code: MutationErrorCode) -> SavePurchaseItemEditsUserError {
    SavePurchaseItemEditsUserError {
        code,
        message: error_message(code),
    }
}

fn error_message(code: MutationErrorCode) -> String {
    match code {
        MutationErrorCode::BadInput => "入力を確認してください。".to_owned(),
        MutationErrorCode::Conflict => "最新の状態を読み込んでから再試行してください。".to_owned(),
        MutationErrorCode::NotFound => "対象が見つかりません。".to_owned(),
        MutationErrorCode::MailForwardingDisabled => "この操作は現在利用できません。".to_owned(),
    }
}

fn bulk_update_persistence_error(
    error: &bulk_update_purchases::BulkUpdatePurchasesPersistenceError,
) -> Option<MutationErrorCode> {
    use bulk_update_purchases::BulkUpdatePurchasesPersistenceError as E;
    match error {
        E::InvalidUuid(_) => Some(MutationErrorCode::BadInput),
        E::HouseholdMismatch | E::MemberNotFound | E::PurchaseNotFound => {
            Some(MutationErrorCode::NotFound)
        }
        E::RevisionConflict => Some(MutationErrorCode::Conflict),
        E::AccountingPeriodClosed => Some(MutationErrorCode::Conflict),
        E::InvalidWorkflow(error) => Some(match error {
            bookkeeping_domain::purchase_commands::BulkUpdatePurchasesError::InvalidTarget(
                bookkeeping_domain::purchase_commands::UpdatePurchaseError::Conflict
                | bookkeeping_domain::purchase_commands::UpdatePurchaseError::NotEditable,
            ) => MutationErrorCode::Conflict,
            _ => MutationErrorCode::BadInput,
        }),
        E::InvalidUpdate(error) => update_purchase_persistence_error(error),
        E::Pool(_) | E::Database(_) => None,
    }
}

fn save_edits_persistence_error(
    error: &save_purchase_item_edits::SavePurchaseItemEditsPersistenceError,
) -> Option<MutationErrorCode> {
    use save_purchase_item_edits::SavePurchaseItemEditsPersistenceError as E;
    match error {
        E::InvalidUuid(_) => Some(MutationErrorCode::BadInput),
        E::PurchaseNotFound | E::ItemNotFound | E::HouseholdMismatch | E::MemberNotFound => {
            Some(MutationErrorCode::NotFound)
        }
        E::InvalidWorkflow(_) => Some(MutationErrorCode::BadInput),
        E::InvalidPurchaseUpdate(error) => update_purchase_persistence_error(error),
        E::InvalidItemUpdate(error) => update_item_persistence_error(error),
        E::Pool(_) | E::Database(_) => None,
    }
}

fn update_purchase_persistence_error(
    error: &bookkeeping_postgres::update_purchase::UpdatePurchasePersistenceError,
) -> Option<MutationErrorCode> {
    use bookkeeping_postgres::update_purchase::UpdatePurchasePersistenceError as E;
    match error {
        E::InvalidUuid(_) | E::InvalidOccurredAt | E::NumericOutOfRange(_) => {
            Some(MutationErrorCode::BadInput)
        }
        E::HouseholdMismatch | E::MemberNotFound | E::PurchaseNotFound | E::AccountNotFound => {
            Some(MutationErrorCode::NotFound)
        }
        E::AccountingPeriodClosed | E::InvalidFundingSnapshot => Some(MutationErrorCode::Conflict),
        E::PaymentTotalMismatch => Some(MutationErrorCode::BadInput),
        E::InvalidDecision(error) => Some(match error {
            bookkeeping_domain::purchase_commands::UpdatePurchaseError::Conflict
            | bookkeeping_domain::purchase_commands::UpdatePurchaseError::NotEditable => {
                MutationErrorCode::Conflict
            }
            _ => MutationErrorCode::BadInput,
        }),
        E::Pool(_) | E::Database(_) => None,
    }
}

fn update_item_persistence_error(
    error: &bookkeeping_postgres::update_purchase_item::UpdatePurchaseItemPersistenceError,
) -> Option<MutationErrorCode> {
    use bookkeeping_postgres::update_purchase_item::UpdatePurchaseItemPersistenceError as E;
    match error {
        E::InvalidUuid(_) | E::NumericOutOfRange(_) | E::InvalidDecision => {
            Some(MutationErrorCode::BadInput)
        }
        E::PurchaseNotFound
        | E::ItemNotFound
        | E::HouseholdMismatch
        | E::MemberNotFound
        | E::ReferenceNotFound(_) => Some(MutationErrorCode::NotFound),
        E::AccountingPeriodClosed | E::InvalidFundingSnapshot => Some(MutationErrorCode::Conflict),
        E::InvalidPaymentAllocation | E::MerchantRequired => Some(MutationErrorCode::BadInput),
        E::InvalidWorkflow(error) => Some(match error {
            bookkeeping_domain::purchase_commands::UpdatePurchaseItemError::Conflict => {
                MutationErrorCode::Conflict
            }
            _ => MutationErrorCode::BadInput,
        }),
        E::Pool(_) | E::Database(_) => None,
    }
}
