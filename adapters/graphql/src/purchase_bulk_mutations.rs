use async_graphql::{Context, ID, InputObject, MaybeUndefined, Result, SimpleObject, Union};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use bookkeeping_domain::purchase_commands::{UpdatePurchaseCommand, UpdatePurchasePayment};
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
    let commands = match bulk_update_commands(input.targets) {
        Ok(commands) => commands,
        Err(()) => return Ok(bulk_update_error(MutationErrorCode::BadInput).into()),
    };
    let handler = ctx.data::<std::sync::Arc<
        dyn cross_context_application::purchase_bulk_mutations::PurchaseBulkMutationHandler,
    >>()?;
    let updated = match handler
        .bulk_update_purchases(
            cross_context_application::purchase_bulk_mutations::BulkUpdatePurchasesRequest {
                actor_id: actor.0,
                commands,
            },
        )
        .await
    {
        Ok(updated) => updated,
        Err(error) => return map_bulk_failure(error),
    };
    let query = ctx.data::<
        std::sync::Arc<dyn bookkeeping_application::purchase_search_query::PurchaseSearchQuery>,
    >()?;
    let mut purchases = Vec::with_capacity(updated.purchase_ids.len());
    for id in updated.purchase_ids {
        purchases.push(
            purchase_mutations::purchase_result(
                query.as_ref(),
                actor.0,
                updated.household_id,
                &id.to_string(),
            )
            .await?,
        );
    }
    Ok(BulkUpdatePurchasesSuccess { purchases }.into())
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
    let handler = ctx.data::<std::sync::Arc<
        dyn cross_context_application::purchase_bulk_mutations::PurchaseBulkMutationHandler,
    >>()?;
    let household_id = match handler.authorize_purchase(actor.0, purchase_id).await {
        Ok(household_id) => household_id,
        Err(error) => return map_save_failure(error),
    };
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
    let updated = match handler
        .save_purchase_item_edits(
            cross_context_application::purchase_bulk_mutations::SavePurchaseItemEditsRequest {
                actor_id: actor.0,
                household_id,
                purchase_id,
                items,
                purchase_patch,
            },
        )
        .await
    {
        Ok(updated) => updated,
        Err(error) => return map_save_failure(error),
    };
    let query = ctx.data::<
        std::sync::Arc<dyn bookkeeping_application::purchase_search_query::PurchaseSearchQuery>,
    >()?;
    let purchase = purchase_mutations::purchase_result(
        query.as_ref(),
        actor.0,
        updated.household_id,
        &purchase_id.to_string(),
    )
    .await?;
    let items_by_id = purchase
        .items
        .into_iter()
        .map(|item| (item.id.to_string(), item))
        .collect::<std::collections::BTreeMap<_, _>>();
    let items = updated
        .item_ids
        .into_iter()
        .map(|id| {
            let node_id = encode_node_id("PurchaseItem", id);
            items_by_id
                .get(node_id.as_str())
                .cloned()
                .ok_or_else(|| crate::internal_error("updated purchase item missing from result"))
        })
        .collect::<Result<Vec<_>>>()?;
    Ok(SavePurchaseItemEditsSuccess { items }.into())
}

fn map_bulk_failure(
    error: cross_context_application::purchase_bulk_mutations::PurchaseBulkMutationFailure,
) -> Result<BulkUpdatePurchasesResult> {
    match error {
        cross_context_application::purchase_bulk_mutations::PurchaseBulkMutationFailure::InvalidInput => {
            Ok(bulk_update_error(MutationErrorCode::BadInput).into())
        }
        cross_context_application::purchase_bulk_mutations::PurchaseBulkMutationFailure::NotFound => {
            Ok(bulk_update_error(MutationErrorCode::NotFound).into())
        }
        cross_context_application::purchase_bulk_mutations::PurchaseBulkMutationFailure::Conflict => {
            Ok(bulk_update_error(MutationErrorCode::Conflict).into())
        }
        cross_context_application::purchase_bulk_mutations::PurchaseBulkMutationFailure::Infrastructure(error) => {
            Err(crate::internal_error(error))
        }
    }
}

fn map_save_failure(
    error: cross_context_application::purchase_bulk_mutations::PurchaseBulkMutationFailure,
) -> Result<SavePurchaseItemEditsResult> {
    match error {
        cross_context_application::purchase_bulk_mutations::PurchaseBulkMutationFailure::InvalidInput => {
            Ok(save_edits_error(MutationErrorCode::BadInput).into())
        }
        cross_context_application::purchase_bulk_mutations::PurchaseBulkMutationFailure::NotFound => {
            Ok(save_edits_error(MutationErrorCode::NotFound).into())
        }
        cross_context_application::purchase_bulk_mutations::PurchaseBulkMutationFailure::Conflict => {
            Ok(save_edits_error(MutationErrorCode::Conflict).into())
        }
        cross_context_application::purchase_bulk_mutations::PurchaseBulkMutationFailure::Infrastructure(error) => {
            Err(crate::internal_error(error))
        }
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
