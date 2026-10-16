use async_graphql::{
    Context, ID, InputObject, MaybeUndefined, OneofObject, Result, SimpleObject, Union,
};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use bookkeeping_application::purchase_search_query::PurchaseSearchQuery;
use bookkeeping_domain::purchase_commands::{
    BulkDeleteItemTarget, BulkUpdateItemPatch, BulkUpdateItemTarget,
    ItemAllocationWeight as BulkItemAllocationWeight, OptionalValueChange, ProductLearning,
    UpdatePurchaseCommand, UpdatePurchaseItemCommand, UpdatePurchasePayment,
    UpdatedItemAllocationWeight as UpdateItemAllocationWeight,
    UpdatedPrintedUnitPrice as PrintedUnitPrice, ValueChange,
};
use std::collections::{BTreeMap, BTreeSet};
use uuid::Uuid;

use crate::{DateTime, Long, MutationErrorCode, purchase_mutations};

#[derive(Clone, Debug, InputObject)]
pub struct UpdatePurchaseInput {
    pub purchase_id: ID,
    pub base_revision: i32,
    pub patch: PurchasePatch,
}

#[derive(Clone, Debug, InputObject)]
pub struct PurchasePatch {
    pub occurred_at: MaybeUndefined<DateTime>,
    pub date_precision: MaybeUndefined<purchase_mutations::PurchaseDatePrecision>,
    pub merchant: MaybeUndefined<String>,
    pub payment_allocations: MaybeUndefined<Vec<UpdatePurchasePaymentInput>>,
}

#[derive(Clone, Debug, InputObject)]
pub struct UpdatePurchasePaymentInput {
    pub payment_account_id: ID,
    pub amount_yen: Long,
}

#[derive(Clone, Debug, InputObject)]
pub struct UpdatePurchaseItemInput {
    pub item_id: ID,
    pub base_revision: i32,
    pub name: MaybeUndefined<String>,
    pub amount_yen: MaybeUndefined<Long>,
    pub quantity: MaybeUndefined<UpdatePurchaseItemQuantityInput>,
    pub printed_unit_price: MaybeUndefined<UpdatePurchaseItemPrintedUnitPriceInput>,
    pub category_id: MaybeUndefined<ID>,
    pub product_variant_id: MaybeUndefined<ID>,
    pub product_learning: MaybeUndefined<ProductLearningConfigurationInput>,
    pub allocations: MaybeUndefined<Vec<UpdatePurchaseItemAllocationInput>>,
}

#[derive(Clone, Debug, InputObject)]
pub struct UpdatePurchaseItemQuantityInput {
    pub numerator: String,
    pub denominator: String,
    pub unit: String,
}

#[derive(Clone, Debug, InputObject)]
pub struct UpdatePurchaseItemPrintedUnitPriceInput {
    pub amount_yen: Long,
    pub basis: UpdatePurchaseItemQuantityInput,
}

#[derive(Clone, Debug, InputObject)]
pub struct UpdatePurchaseItemAllocationInput {
    pub pool_id: ID,
    pub weight: Long,
}

#[derive(Clone, Debug, OneofObject)]
#[graphql(
    name = "ProductLearningConfigurationInput",
    rename_fields = "camelCase"
)]
pub enum ProductLearningConfigurationInput {
    ItemOnly(bool),
    VariantName(String),
    MerchantAlias(bool),
}

#[derive(Clone, Debug, InputObject)]
pub struct PurchaseItemTargetInput {
    pub item_id: ID,
    pub base_revision: i32,
}

#[derive(Clone, Debug, InputObject)]
pub struct BulkUpdatePurchaseItemsInput {
    pub targets: Vec<PurchaseItemTargetInput>,
    pub patch: BulkPurchaseItemPatch,
}

#[derive(Clone, Debug, InputObject)]
pub struct BulkPurchaseItemPatch {
    pub name: MaybeUndefined<String>,
    pub category_id: MaybeUndefined<ID>,
    pub product_variant_id: MaybeUndefined<ID>,
    pub allocations: MaybeUndefined<Vec<BulkUpdatePurchaseItemsAllocationInput>>,
    pub tag_ids: MaybeUndefined<Vec<ID>>,
}

#[derive(Clone, Debug, InputObject)]
pub struct BulkUpdatePurchaseItemsAllocationInput {
    pub pool_id: ID,
    pub weight: Long,
}

#[derive(Clone, Debug, InputObject)]
pub struct BulkDeletePurchaseItemsInput {
    pub targets: Vec<PurchaseItemTargetInput>,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct UpdatePurchaseSuccess {
    pub result: purchase_mutations::PurchaseObject,
}
#[derive(Clone, Debug, SimpleObject)]
pub struct UpdatePurchaseUserError {
    pub code: MutationErrorCode,
    pub message: String,
}
#[derive(Union)]
pub enum UpdatePurchaseResult {
    Success(Box<UpdatePurchaseSuccess>),
    UserError(UpdatePurchaseUserError),
}

#[derive(Clone, Debug, SimpleObject)]
pub struct UpdatePurchaseItemSuccess {
    pub result: purchase_mutations::PurchaseItem,
}
#[derive(Clone, Debug, SimpleObject)]
pub struct UpdatePurchaseItemUserError {
    pub code: MutationErrorCode,
    pub message: String,
}
#[derive(Union)]
pub enum UpdatePurchaseItemResult {
    Success(Box<UpdatePurchaseItemSuccess>),
    UserError(UpdatePurchaseItemUserError),
}

#[derive(Clone, Debug, SimpleObject)]
pub struct BulkUpdatePurchaseItemsSuccess {
    pub items: Vec<purchase_mutations::PurchaseItem>,
}
#[derive(Clone, Debug, SimpleObject)]
pub struct BulkUpdatePurchaseItemsUserError {
    pub code: MutationErrorCode,
    pub message: String,
}
#[derive(Union)]
pub enum BulkUpdatePurchaseItemsResult {
    Success(Box<BulkUpdatePurchaseItemsSuccess>),
    UserError(BulkUpdatePurchaseItemsUserError),
}

#[derive(Clone, Debug, SimpleObject)]
pub struct BulkDeletePurchaseItemsSuccess {
    pub deleted_item_ids: Vec<ID>,
}
#[derive(Clone, Debug, SimpleObject)]
pub struct BulkDeletePurchaseItemsUserError {
    pub code: MutationErrorCode,
    pub message: String,
}
#[derive(Union)]
pub enum BulkDeletePurchaseItemsResult {
    Success(BulkDeletePurchaseItemsSuccess),
    UserError(BulkDeletePurchaseItemsUserError),
}

pub async fn update_purchase(
    ctx: &Context<'_>,
    input: UpdatePurchaseInput,
) -> Result<UpdatePurchaseResult> {
    let actor = crate::actor(ctx)?;
    let purchase_id = match decode_node_id(&input.purchase_id, "Purchase") {
        Ok(id) => id,
        Err(()) => return Ok(update_error(MutationErrorCode::BadInput).into()),
    };
    let base_revision = match u64::try_from(input.base_revision) {
        Ok(revision) if revision > 0 => revision,
        _ => return Ok(update_error(MutationErrorCode::BadInput).into()),
    };
    let command = match update_purchase_command(input, purchase_id) {
        Ok(value) => value,
        Err(()) => return Ok(update_error(MutationErrorCode::BadInput).into()),
    };
    let mut command = command;
    command.base_revision = base_revision;
    let handler = ctx.data::<std::sync::Arc<
        dyn bookkeeping_application::purchase_edit_mutations::PurchaseEditMutationHandler,
    >>()?;
    match handler
        .update_purchase(
            bookkeeping_application::purchase_edit_mutations::PurchaseEditRequest {
                actor_id: actor.0,
                household_id: None,
                value: command,
            },
        )
        .await
    {
        Ok(value) => Ok(Box::new(UpdatePurchaseSuccess {
            result: load_purchase(ctx, actor.0, value.household_id, value.purchase_id).await?,
        })
        .into()),
        Err(error) => match purchase_edit_error(error)? {
            Some(code) => Ok(update_error(code).into()),
            None => unreachable!(),
        },
    }
}

pub async fn update_purchase_item(
    ctx: &Context<'_>,
    input: UpdatePurchaseItemInput,
) -> Result<UpdatePurchaseItemResult> {
    let actor = crate::actor(ctx)?;
    let item_id = match decode_node_id(&input.item_id, "PurchaseItem") {
        Ok(id) => id,
        Err(()) => return Ok(update_item_error(MutationErrorCode::BadInput).into()),
    };
    let base_revision = match u64::try_from(input.base_revision) {
        Ok(revision) if revision > 0 => revision,
        _ => return Ok(update_item_error(MutationErrorCode::BadInput).into()),
    };
    let command = match item_update_command(input, item_id, base_revision) {
        Ok(command) => command,
        Err(()) => return Ok(update_item_error(MutationErrorCode::BadInput).into()),
    };
    let handler = ctx.data::<std::sync::Arc<
        dyn bookkeeping_application::purchase_edit_mutations::PurchaseEditMutationHandler,
    >>()?;
    match handler
        .update_purchase_item(
            bookkeeping_application::purchase_edit_mutations::PurchaseEditRequest {
                actor_id: actor.0,
                household_id: None,
                value: command,
            },
        )
        .await
    {
        Ok(value) => {
            let purchase =
                load_purchase(ctx, actor.0, value.household_id, value.purchase_id).await?;
            let Some(item) = purchase
                .items
                .into_iter()
                .find(|item| item.id == encode_node_id("PurchaseItem", value.item_id))
            else {
                return Ok(update_item_error(MutationErrorCode::NotFound).into());
            };
            Ok(Box::new(UpdatePurchaseItemSuccess { result: item }).into())
        }
        Err(error) => match purchase_edit_error(error)? {
            Some(code) => Ok(update_item_error(code).into()),
            None => unreachable!(),
        },
    }
}

pub async fn bulk_update_purchase_items(
    ctx: &Context<'_>,
    input: BulkUpdatePurchaseItemsInput,
) -> Result<BulkUpdatePurchaseItemsResult> {
    let actor = crate::actor(ctx)?;
    let targets = match parse_targets(input.targets) {
        Ok(targets) => targets,
        Err(()) => return Ok(bulk_update_error(MutationErrorCode::BadInput).into()),
    };
    let patch = match bulk_update_patch(input.patch) {
        Ok(patch) => patch,
        Err(()) => return Ok(bulk_update_error(MutationErrorCode::BadInput).into()),
    };
    let handler = ctx.data::<std::sync::Arc<
        dyn bookkeeping_application::purchase_edit_mutations::PurchaseEditMutationHandler,
    >>()?;
    match handler
        .bulk_update_purchase_items(
            bookkeeping_application::purchase_edit_mutations::PurchaseEditRequest {
                actor_id: actor.0,
                household_id: None,
                value: (targets, patch),
            },
        )
        .await
    {
        Ok(value) => {
            let mut purchases = BTreeMap::new();
            let mut items = Vec::with_capacity(value.items.len());
            for updated in value.items {
                let item_id = updated.item_id;
                let purchase_id = updated.purchase_id;
                if let std::collections::btree_map::Entry::Vacant(entry) =
                    purchases.entry(purchase_id)
                {
                    entry.insert(
                        load_purchase(ctx, actor.0, updated.household_id, purchase_id).await?,
                    );
                }
                let Some(item) = purchases[&purchase_id]
                    .items
                    .iter()
                    .find(|item| item.id == encode_node_id("PurchaseItem", item_id))
                    .cloned()
                else {
                    return Ok(bulk_update_error(MutationErrorCode::NotFound).into());
                };
                items.push(item);
            }
            Ok(Box::new(BulkUpdatePurchaseItemsSuccess { items }).into())
        }
        Err(error) => match purchase_edit_error(error)? {
            Some(code) => Ok(bulk_update_error(code).into()),
            None => unreachable!(),
        },
    }
}

pub async fn bulk_delete_purchase_items(
    ctx: &Context<'_>,
    input: BulkDeletePurchaseItemsInput,
) -> Result<BulkDeletePurchaseItemsResult> {
    let actor = crate::actor(ctx)?;
    let targets = match parse_delete_targets(input.targets) {
        Ok(targets) => targets,
        Err(()) => return Ok(bulk_delete_error(MutationErrorCode::BadInput).into()),
    };
    let handler = ctx.data::<std::sync::Arc<
        dyn bookkeeping_application::purchase_edit_mutations::PurchaseEditMutationHandler,
    >>()?;
    match handler
        .bulk_delete_purchase_items(
            bookkeeping_application::purchase_edit_mutations::PurchaseEditRequest {
                actor_id: actor.0,
                household_id: None,
                value: targets,
            },
        )
        .await
    {
        Ok(value) => Ok(BulkDeletePurchaseItemsSuccess {
            deleted_item_ids: value
                .item_ids
                .iter()
                .map(|id| encode_node_id("PurchaseItem", *id))
                .collect(),
        }
        .into()),
        Err(error) => match purchase_edit_error(error)? {
            Some(code) => Ok(bulk_delete_error(code).into()),
            None => unreachable!(),
        },
    }
}

pub(crate) fn update_purchase_command(
    input: UpdatePurchaseInput,
    purchase_id: Uuid,
) -> std::result::Result<UpdatePurchaseCommand, ()> {
    let mut changed_fields = Vec::new();
    let occurred_at = match input.patch.occurred_at {
        MaybeUndefined::Undefined => None,
        MaybeUndefined::Null => return Err(()),
        MaybeUndefined::Value(value) => {
            changed_fields.push("occurredAt".to_owned());
            Some(value.parse_utc().map_err(|_| ())?)
        }
    };
    let date_precision = match input.patch.date_precision {
        MaybeUndefined::Undefined => None,
        MaybeUndefined::Null => return Err(()),
        MaybeUndefined::Value(value) => {
            if occurred_at.is_none() {
                return Err(());
            }
            Some(match value {
                purchase_mutations::PurchaseDatePrecision::DateTime => "DATE_TIME".to_owned(),
                purchase_mutations::PurchaseDatePrecision::DateOnly => "DATE_ONLY".to_owned(),
            })
        }
    };
    let merchant = match input.patch.merchant {
        MaybeUndefined::Undefined => None,
        MaybeUndefined::Null => return Err(()),
        MaybeUndefined::Value(value) => {
            changed_fields.push("merchant".to_owned());
            Some(value)
        }
    };
    let payment_allocations = match input.patch.payment_allocations {
        MaybeUndefined::Undefined => None,
        MaybeUndefined::Null => return Err(()),
        MaybeUndefined::Value(values) => {
            changed_fields.push("paymentAllocations".to_owned());
            Some(
                values
                    .into_iter()
                    .map(|payment| {
                        Ok(UpdatePurchasePayment {
                            account_id: decode_node_id(&payment.payment_account_id, "Account")?
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
        base_revision: u64::try_from(input.base_revision).map_err(|_| ())?,
        changed_fields,
        occurred_at: occurred_at.map(|value| value.to_rfc3339()),
        date_precision,
        merchant,
        payment_allocations,
    })
}

pub(crate) fn item_update_command(
    input: UpdatePurchaseItemInput,
    item_id: Uuid,
    base_revision: u64,
) -> std::result::Result<UpdatePurchaseItemCommand, ()> {
    let name = match input.name {
        MaybeUndefined::Undefined => ValueChange::Unchanged,
        MaybeUndefined::Null => return Err(()),
        MaybeUndefined::Value(value) => ValueChange::Set(value),
    };
    let amount_yen = match input.amount_yen {
        MaybeUndefined::Undefined => ValueChange::Unchanged,
        MaybeUndefined::Null => return Err(()),
        MaybeUndefined::Value(value) => ValueChange::Set(value.0),
    };
    let quantity = match input.quantity {
        MaybeUndefined::Undefined => OptionalValueChange::Unchanged,
        MaybeUndefined::Null => OptionalValueChange::Clear,
        MaybeUndefined::Value(value) => OptionalValueChange::Set(parse_measure(value)?),
    };
    let printed_unit_price = match input.printed_unit_price {
        MaybeUndefined::Undefined => OptionalValueChange::Unchanged,
        MaybeUndefined::Null => OptionalValueChange::Clear,
        MaybeUndefined::Value(value) => OptionalValueChange::Set(parse_printed_unit_price(value)?),
    };
    let category_id = match input.category_id {
        MaybeUndefined::Undefined => OptionalValueChange::Unchanged,
        MaybeUndefined::Null => OptionalValueChange::Clear,
        MaybeUndefined::Value(value) => {
            OptionalValueChange::Set(decode_node_id(&value, "Category")?.to_string())
        }
    };
    let product_variant_id = match input.product_variant_id {
        MaybeUndefined::Undefined => OptionalValueChange::Unchanged,
        MaybeUndefined::Null => OptionalValueChange::Clear,
        MaybeUndefined::Value(value) => {
            OptionalValueChange::Set(decode_node_id(&value, "ProductVariant")?.to_string())
        }
    };
    let product_learning = match input.product_learning {
        MaybeUndefined::Undefined => None,
        MaybeUndefined::Null => return Err(()),
        MaybeUndefined::Value(value) => Some(match value {
            ProductLearningConfigurationInput::ItemOnly(true) => ProductLearning::ItemOnly,
            ProductLearningConfigurationInput::ItemOnly(false) => return Err(()),
            ProductLearningConfigurationInput::VariantName(value) => {
                ProductLearning::VariantName(value)
            }
            ProductLearningConfigurationInput::MerchantAlias(true) => {
                ProductLearning::MerchantAlias
            }
            ProductLearningConfigurationInput::MerchantAlias(false) => return Err(()),
        }),
    };
    let allocations = match input.allocations {
        MaybeUndefined::Undefined => ValueChange::Unchanged,
        MaybeUndefined::Null => return Err(()),
        MaybeUndefined::Value(values) => ValueChange::Set(
            values
                .into_iter()
                .map(|value| {
                    Ok(UpdateItemAllocationWeight {
                        pool_id: decode_node_id(&value.pool_id, "Pool")?.to_string(),
                        weight: value.weight.0,
                    })
                })
                .collect::<std::result::Result<Vec<_>, ()>>()?,
        ),
    };
    Ok(UpdatePurchaseItemCommand {
        item_id: item_id.to_string(),
        base_revision,
        name,
        amount_yen,
        quantity,
        printed_unit_price,
        category_id,
        product_variant_id,
        product_learning,
        allocations,
    })
}

fn parse_measure(
    value: UpdatePurchaseItemQuantityInput,
) -> std::result::Result<bookkeeping_domain::purchase_commands::Measure, ()> {
    let unit =
        bookkeeping_domain::purchase_commands::QuantityUnit::parse(&value.unit).map_err(|_| ())?;
    bookkeeping_domain::purchase_commands::Measure::new(
        value.numerator.parse().map_err(|_| ())?,
        value.denominator.parse().map_err(|_| ())?,
        unit,
    )
    .map_err(|_| ())
}

fn parse_printed_unit_price(
    value: UpdatePurchaseItemPrintedUnitPriceInput,
) -> std::result::Result<PrintedUnitPrice, ()> {
    Ok(PrintedUnitPrice {
        amount_yen: value.amount_yen.0,
        basis: parse_measure(value.basis)?,
    })
}

fn parse_targets(
    values: Vec<PurchaseItemTargetInput>,
) -> std::result::Result<Vec<BulkUpdateItemTarget>, ()> {
    let targets = values
        .into_iter()
        .map(|value| {
            Ok(BulkUpdateItemTarget {
                item_id: decode_node_id(&value.item_id, "PurchaseItem")?.to_string(),
                base_revision: u64::try_from(value.base_revision).map_err(|_| ())?,
            })
        })
        .collect::<std::result::Result<Vec<_>, _>>()?;
    validate_target_set(
        targets
            .iter()
            .map(|target| (&target.item_id, target.base_revision)),
    )?;
    Ok(targets)
}

fn bulk_update_patch(value: BulkPurchaseItemPatch) -> std::result::Result<BulkUpdateItemPatch, ()> {
    let (name_set, name) = match value.name {
        MaybeUndefined::Undefined => (false, None),
        MaybeUndefined::Null => return Err(()),
        MaybeUndefined::Value(value) => (true, Some(value)),
    };
    let (category_id_set, category_id) = match value.category_id {
        MaybeUndefined::Undefined => (false, None),
        MaybeUndefined::Null => (true, None),
        MaybeUndefined::Value(value) => {
            (true, Some(decode_node_id(&value, "Category")?.to_string()))
        }
    };
    let (product_variant_id_set, product_variant_id) = match value.product_variant_id {
        MaybeUndefined::Undefined => (false, None),
        MaybeUndefined::Null => (true, None),
        MaybeUndefined::Value(value) => (
            true,
            Some(decode_node_id(&value, "ProductVariant")?.to_string()),
        ),
    };
    let (allocations_set, allocations) = match value.allocations {
        MaybeUndefined::Undefined => (false, Vec::new()),
        MaybeUndefined::Null => return Err(()),
        MaybeUndefined::Value(values) => (
            true,
            values
                .into_iter()
                .map(|value| {
                    Ok(BulkItemAllocationWeight {
                        pool_id: decode_node_id(&value.pool_id, "Pool")?.to_string(),
                        weight: value.weight.0,
                    })
                })
                .collect::<std::result::Result<Vec<_>, ()>>()?,
        ),
    };
    let (tag_ids_set, tag_ids) = match value.tag_ids {
        MaybeUndefined::Undefined => (false, Vec::new()),
        MaybeUndefined::Null => return Err(()),
        MaybeUndefined::Value(values) => (
            true,
            values
                .iter()
                .map(|value| decode_node_id(value, "Tag").map(|id| id.to_string()))
                .collect::<std::result::Result<Vec<_>, _>>()?,
        ),
    };
    Ok(BulkUpdateItemPatch {
        name_set,
        name,
        category_id_set,
        category_id,
        product_variant_id_set,
        product_variant_id,
        allocations_set,
        allocations,
        tag_ids_set,
        tag_ids,
    })
}

fn parse_delete_targets(
    values: Vec<PurchaseItemTargetInput>,
) -> std::result::Result<Vec<BulkDeleteItemTarget>, ()> {
    let targets = values
        .into_iter()
        .map(|value| {
            Ok(BulkDeleteItemTarget {
                item_id: decode_node_id(&value.item_id, "PurchaseItem")?.to_string(),
                base_revision: u64::try_from(value.base_revision).map_err(|_| ())?,
            })
        })
        .collect::<std::result::Result<Vec<_>, _>>()?;
    validate_target_set(
        targets
            .iter()
            .map(|target| (&target.item_id, target.base_revision)),
    )?;
    Ok(targets)
}

fn validate_target_set<'a>(
    targets: impl IntoIterator<Item = (&'a String, u64)>,
) -> std::result::Result<(), ()> {
    let mut ids = BTreeSet::new();
    let mut count = 0;
    for (id, revision) in targets {
        count += 1;
        if revision == 0 || !ids.insert(id.as_str()) {
            return Err(());
        }
    }
    if !(1..=100).contains(&count) {
        return Err(());
    }
    Ok(())
}

async fn load_purchase(
    ctx: &Context<'_>,
    actor_id: Uuid,
    household_id: Uuid,
    purchase_id: Uuid,
) -> Result<purchase_mutations::PurchaseObject> {
    let query = ctx.data::<std::sync::Arc<dyn PurchaseSearchQuery>>()?;
    let projection = query
        .purchase(actor_id, household_id, purchase_id)
        .await
        .map_err(crate::internal_error)?;
    projection.try_into().map_err(crate::internal_error)
}

fn decode_node_id(value: &ID, expected_kind: &str) -> std::result::Result<Uuid, ()> {
    let decoded = URL_SAFE_NO_PAD.decode(value.as_str()).map_err(|_| ())?;
    let separator = decoded.iter().position(|byte| *byte == b':').ok_or(())?;
    let (kind, payload) = decoded.split_at(separator);
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

fn update_error(code: MutationErrorCode) -> UpdatePurchaseUserError {
    UpdatePurchaseUserError {
        code,
        message: error_message(code),
    }
}
fn update_item_error(code: MutationErrorCode) -> UpdatePurchaseItemUserError {
    UpdatePurchaseItemUserError {
        code,
        message: error_message(code),
    }
}
fn bulk_update_error(code: MutationErrorCode) -> BulkUpdatePurchaseItemsUserError {
    BulkUpdatePurchaseItemsUserError {
        code,
        message: error_message(code),
    }
}
fn bulk_delete_error(code: MutationErrorCode) -> BulkDeletePurchaseItemsUserError {
    BulkDeletePurchaseItemsUserError {
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

fn purchase_edit_error(
    error: bookkeeping_application::purchase_edit_mutations::PurchaseEditFailure,
) -> Result<Option<MutationErrorCode>> {
    use bookkeeping_application::purchase_edit_mutations::PurchaseEditFailure as Failure;
    match error {
        Failure::InvalidInput => Ok(Some(MutationErrorCode::BadInput)),
        Failure::NotFound => Ok(Some(MutationErrorCode::NotFound)),
        Failure::Conflict => Ok(Some(MutationErrorCode::Conflict)),
        Failure::Infrastructure(error) => Err(crate::internal_error(error)),
    }
}
