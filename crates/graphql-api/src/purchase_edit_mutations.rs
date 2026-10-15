use async_graphql::{
    Context, ID, InputObject, MaybeUndefined, OneofObject, Result, SimpleObject, Union,
};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use bookkeeping_workflows::{
    bulk_delete_items::BulkDeleteItemTarget,
    bulk_update_items::{BulkUpdateItemPatch, BulkUpdateItemTarget, ItemAllocationWeight},
    update_purchase::{
        UpdatePurchaseCommand, UpdatePurchaseDraft, UpdatePurchasePayment, decide_update_purchase,
    },
    update_purchase_item::{
        ItemAllocationWeight as UpdateItemAllocationWeight, PrintedUnitPrice, ProductLearning,
        UpdatePurchaseItemCommand, UpdatePurchaseItemError,
    },
};
use horse_postgres::{
    PgPool,
    bulk_delete_purchase_items::{self, PersistBulkDeletePurchaseItems},
    bulk_update_purchase_items::{self, PersistBulkUpdatePurchaseItems},
    household_access, purchase_command_result,
    update_purchase::{self, PersistUpdatePurchase},
    update_purchase_item::{self, PersistUpdatePurchaseItem},
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
    Success(UpdatePurchaseSuccess),
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
    Success(UpdatePurchaseItemSuccess),
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
    Success(BulkUpdatePurchaseItemsSuccess),
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
    let pool = ctx.data::<PgPool>()?;
    let household_id =
        match purchase_command_result::load_purchase_household_id(pool, purchase_id).await {
            Ok(Some(id)) => id,
            Ok(None) => return Ok(update_error(MutationErrorCode::NotFound).into()),
            Err(error) => return Err(crate::internal_error(error)),
        };
    if !active_member(pool, household_id, actor.0).await? {
        return Ok(update_error(MutationErrorCode::NotFound).into());
    }
    let context =
        match purchase_command_result::load_purchase_edit_context(pool, household_id, purchase_id)
            .await
        {
            Ok(Some(value)) => value,
            Ok(None) => return Ok(update_error(MutationErrorCode::NotFound).into()),
            Err(error) => return Err(crate::internal_error(error)),
        };
    let mut command = match update_purchase_command(input, purchase_id) {
        Ok(value) => value,
        Err(()) => return Ok(update_error(MutationErrorCode::BadInput).into()),
    };
    command.base_revision = base_revision;
    let current = match update_purchase_draft(context) {
        Ok(current) => current,
        Err(()) => return Err(crate::internal_error("invalid purchase edit context")),
    };
    let decision = match bookkeeping_workflows::update_purchase::decide_update_purchase(
        command.clone(),
        current,
    ) {
        Ok(value) => value,
        Err(error) => return Ok(update_error(update_error_code(error)).into()),
    };
    let persisted = update_purchase::persist_update_purchase(
        pool,
        PersistUpdatePurchase {
            household_id: household_id.to_string(),
            actor_id: actor.0.to_string(),
        },
        command,
        decision,
    )
    .await;
    match persisted {
        Ok(value) => Ok(UpdatePurchaseSuccess {
            result: load_purchase(pool, household_id, purchase_id).await?,
        }
        .into()),
        Err(error) => match update_persistence_error(&error) {
            Some(code) => Ok(update_error(code).into()),
            None => Err(crate::internal_error(error)),
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
    let pool = ctx.data::<PgPool>()?;
    let household_id = match purchase_command_result::load_purchase_item_household_id(pool, item_id)
        .await
        .map_err(crate::internal_error)?
    {
        Some(id) => id,
        None => return Ok(update_item_error(MutationErrorCode::NotFound).into()),
    };
    if !active_member(pool, household_id, actor.0).await? {
        return Ok(update_item_error(MutationErrorCode::NotFound).into());
    }
    let command = match item_update_command(input, item_id, base_revision) {
        Ok(command) => command,
        Err(()) => return Ok(update_item_error(MutationErrorCode::BadInput).into()),
    };
    match update_purchase_item::persist_update_purchase_item(
        pool,
        PersistUpdatePurchaseItem {
            household_id: household_id.to_string(),
            actor_id: actor.0.to_string(),
        },
        command,
    )
    .await
    {
        Ok(value) => {
            let purchase_id = parse_uuid(&value.purchase_id)?;
            let purchase = load_purchase(pool, household_id, purchase_id).await?;
            let Some(item) = purchase
                .items
                .into_iter()
                .find(|item| item.id == encode_node_id("PurchaseItem", item_id))
            else {
                return Ok(update_item_error(MutationErrorCode::NotFound).into());
            };
            Ok(UpdatePurchaseItemSuccess { result: item }.into())
        }
        Err(error) => match update_item_persistence_error(&error) {
            Some(code) => Ok(update_item_error(code).into()),
            None => Err(crate::internal_error(error)),
        },
    }
}

pub async fn bulk_update_purchase_items(
    ctx: &Context<'_>,
    input: BulkUpdatePurchaseItemsInput,
) -> Result<BulkUpdatePurchaseItemsResult> {
    let actor = crate::actor(ctx)?;
    let pool = ctx.data::<PgPool>()?;
    let targets = match parse_targets(input.targets) {
        Ok(targets) => targets,
        Err(()) => return Ok(bulk_update_error(MutationErrorCode::BadInput).into()),
    };
    let household_id = match household_for_targets(pool, &targets).await? {
        Some(id) => id,
        None => return Ok(bulk_update_error(MutationErrorCode::NotFound).into()),
    };
    if !active_member(pool, household_id, actor.0).await? {
        return Ok(bulk_update_error(MutationErrorCode::NotFound).into());
    }
    let patch = match bulk_update_patch(input.patch) {
        Ok(patch) => patch,
        Err(()) => return Ok(bulk_update_error(MutationErrorCode::BadInput).into()),
    };
    match bulk_update_purchase_items::persist_bulk_update_purchase_items(
        pool,
        PersistBulkUpdatePurchaseItems {
            household_id: household_id.to_string(),
            actor_id: actor.0.to_string(),
        },
        targets,
        patch,
    )
    .await
    {
        Ok(value) => {
            let mut purchases = BTreeMap::new();
            let mut items = Vec::with_capacity(value.items.len());
            for (item_id, purchase_id) in value.items {
                let item_id = parse_uuid(&item_id)?;
                let purchase_id = parse_uuid(&purchase_id)?;
                if !purchases.contains_key(&purchase_id) {
                    purchases.insert(
                        purchase_id,
                        load_purchase(pool, household_id, purchase_id).await?,
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
            Ok(BulkUpdatePurchaseItemsSuccess { items }.into())
        }
        Err(error) => match bulk_update_persistence_error(&error) {
            Some(code) => Ok(bulk_update_error(code).into()),
            None => Err(crate::internal_error(error)),
        },
    }
}

pub async fn bulk_delete_purchase_items(
    ctx: &Context<'_>,
    input: BulkDeletePurchaseItemsInput,
) -> Result<BulkDeletePurchaseItemsResult> {
    let actor = crate::actor(ctx)?;
    let pool = ctx.data::<PgPool>()?;
    let targets = match parse_delete_targets(input.targets) {
        Ok(targets) => targets,
        Err(()) => return Ok(bulk_delete_error(MutationErrorCode::BadInput).into()),
    };
    let household_targets = targets
        .iter()
        .map(|target| BulkUpdateItemTarget {
            item_id: target.item_id.clone(),
            base_revision: target.base_revision,
        })
        .collect::<Vec<_>>();
    let household_id = match household_for_targets(pool, &household_targets).await? {
        Some(id) => id,
        None => return Ok(bulk_delete_error(MutationErrorCode::NotFound).into()),
    };
    if !active_member(pool, household_id, actor.0).await? {
        return Ok(bulk_delete_error(MutationErrorCode::NotFound).into());
    }
    match bulk_delete_purchase_items::persist_bulk_delete_purchase_items(
        pool,
        PersistBulkDeletePurchaseItems {
            household_id: household_id.to_string(),
            actor_id: actor.0.to_string(),
        },
        targets,
    )
    .await
    {
        Ok(value) => Ok(BulkDeletePurchaseItemsSuccess {
            deleted_item_ids: value
                .deleted_item_ids
                .iter()
                .map(|id| parse_uuid(id).map(|id| encode_node_id("PurchaseItem", id)))
                .collect::<Result<Vec<_>>>()?,
        }
        .into()),
        Err(error) => match bulk_delete_persistence_error(&error) {
            Some(code) => Ok(bulk_delete_error(code).into()),
            None => Err(crate::internal_error(error)),
        },
    }
}

fn update_purchase_command(
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

fn update_purchase_draft(
    value: purchase_command_result::PurchaseEditContext,
) -> std::result::Result<UpdatePurchaseDraft, ()> {
    let field_revisions = value
        .field_revisions
        .as_object()
        .ok_or(())?
        .iter()
        .map(|(key, value)| {
            value
                .as_u64()
                .map(|revision| (key.clone(), revision))
                .ok_or(())
        })
        .collect::<std::result::Result<BTreeMap<_, _>, _>>()?;
    Ok(UpdatePurchaseDraft {
        revision: u64::try_from(value.revision).map_err(|_| ())?,
        status: value.status,
        occurred_at: value.occurred_at.to_rfc3339(),
        date_precision: value.date_precision,
        merchant: value.merchant,
        field_revisions,
    })
}

fn item_update_command(
    input: UpdatePurchaseItemInput,
    item_id: Uuid,
    base_revision: u64,
) -> std::result::Result<UpdatePurchaseItemCommand, ()> {
    let mut changed_fields = Vec::new();
    let name = match input.name {
        MaybeUndefined::Undefined => None,
        MaybeUndefined::Null => return Err(()),
        MaybeUndefined::Value(value) => {
            changed_fields.push("name".to_owned());
            Some(value)
        }
    };
    let amount_yen = match input.amount_yen {
        MaybeUndefined::Undefined => None,
        MaybeUndefined::Null => return Err(()),
        MaybeUndefined::Value(value) => {
            changed_fields.push("amountYen".to_owned());
            Some(value.0)
        }
    };
    let (quantity_set, quantity) = match input.quantity {
        MaybeUndefined::Undefined => (false, None),
        MaybeUndefined::Null => {
            changed_fields.push("quantity".to_owned());
            (true, None)
        }
        MaybeUndefined::Value(value) => {
            changed_fields.push("quantity".to_owned());
            (true, Some(value.into()))
        }
    };
    let (printed_unit_price_set, printed_unit_price) = match input.printed_unit_price {
        MaybeUndefined::Undefined => (false, None),
        MaybeUndefined::Null => {
            changed_fields.push("printedUnitPrice".to_owned());
            (true, None)
        }
        MaybeUndefined::Value(value) => {
            changed_fields.push("printedUnitPrice".to_owned());
            (true, Some(value.into()))
        }
    };
    let (category_id_set, category_id) = match input.category_id {
        MaybeUndefined::Undefined => (false, None),
        MaybeUndefined::Null => {
            changed_fields.push("categoryId".to_owned());
            (true, None)
        }
        MaybeUndefined::Value(value) => {
            changed_fields.push("categoryId".to_owned());
            (true, Some(decode_node_id(&value, "Category")?.to_string()))
        }
    };
    let (product_variant_id_set, product_variant_id) = match input.product_variant_id {
        MaybeUndefined::Undefined => (false, None),
        MaybeUndefined::Null => {
            changed_fields.push("productVariantId".to_owned());
            (true, None)
        }
        MaybeUndefined::Value(value) => {
            changed_fields.push("productVariantId".to_owned());
            (
                true,
                Some(decode_node_id(&value, "ProductVariant")?.to_string()),
            )
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
    let (allocations_set, allocations) = match input.allocations {
        MaybeUndefined::Undefined => (false, Vec::new()),
        MaybeUndefined::Null => return Err(()),
        MaybeUndefined::Value(values) => {
            changed_fields.push("allocations".to_owned());
            (
                true,
                values
                    .into_iter()
                    .map(|value| {
                        Ok(UpdateItemAllocationWeight {
                            pool_id: decode_node_id(&value.pool_id, "Pool")?.to_string(),
                            weight: value.weight.0,
                        })
                    })
                    .collect::<std::result::Result<Vec<_>, ()>>()?,
            )
        }
    };
    Ok(UpdatePurchaseItemCommand {
        item_id: item_id.to_string(),
        base_revision,
        changed_fields,
        name,
        amount_yen,
        quantity_set,
        quantity,
        printed_unit_price_set,
        printed_unit_price,
        category_id_set,
        category_id,
        product_variant_id_set,
        product_variant_id,
        product_learning,
        allocations_set,
        allocations,
    })
}

impl From<UpdatePurchaseItemQuantityInput> for bookkeeping_workflows::create_purchase::Measure {
    fn from(value: UpdatePurchaseItemQuantityInput) -> Self {
        Self {
            numerator: value.numerator,
            denominator: value.denominator,
            unit: value.unit,
        }
    }
}

impl From<UpdatePurchaseItemPrintedUnitPriceInput> for PrintedUnitPrice {
    fn from(value: UpdatePurchaseItemPrintedUnitPriceInput) -> Self {
        Self {
            amount_yen: value.amount_yen.0,
            basis: value.basis.into(),
        }
    }
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
                    Ok(ItemAllocationWeight {
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

async fn household_for_targets(
    pool: &PgPool,
    targets: &[BulkUpdateItemTarget],
) -> Result<Option<Uuid>> {
    let ids = targets
        .iter()
        .map(|target| Uuid::parse_str(&target.item_id).map_err(crate::internal_error))
        .collect::<Result<Vec<_>>>()?;
    let rows = purchase_command_result::load_purchase_item_household_ids(pool, &ids)
        .await
        .map_err(crate::internal_error)?;
    if rows.len() != ids.len() {
        return Ok(None);
    }
    let households = rows
        .into_iter()
        .map(|(_, household_id)| household_id)
        .collect::<BTreeSet<_>>();
    if households.len() != 1 {
        return Ok(None);
    }
    Ok(households.into_iter().next())
}

async fn active_member(pool: &PgPool, household_id: Uuid, member_id: Uuid) -> Result<bool> {
    household_access::is_active_member(pool, household_id, member_id)
        .await
        .map_err(crate::internal_error)
}

async fn load_purchase(
    pool: &PgPool,
    household_id: Uuid,
    purchase_id: Uuid,
) -> Result<purchase_mutations::PurchaseObject> {
    let projection =
        purchase_command_result::load_purchase_command_result(pool, household_id, purchase_id)
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

fn parse_uuid(value: &str) -> Result<Uuid> {
    Uuid::parse_str(value).map_err(crate::internal_error)
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

fn update_error_code(
    error: bookkeeping_workflows::update_purchase::UpdatePurchaseError,
) -> MutationErrorCode {
    use bookkeeping_workflows::update_purchase::UpdatePurchaseError as E;
    match error {
        E::Conflict | E::NotEditable => MutationErrorCode::Conflict,
        _ => MutationErrorCode::BadInput,
    }
}

fn update_persistence_error(
    error: &update_purchase::UpdatePurchasePersistenceError,
) -> Option<MutationErrorCode> {
    use update_purchase::UpdatePurchasePersistenceError as E;
    match error {
        E::InvalidUuid(_) | E::InvalidOccurredAt | E::NumericOutOfRange(_) => {
            Some(MutationErrorCode::BadInput)
        }
        E::HouseholdMismatch | E::MemberNotFound | E::PurchaseNotFound | E::AccountNotFound => {
            Some(MutationErrorCode::NotFound)
        }
        E::AccountingPeriodClosed | E::InvalidFundingSnapshot => Some(MutationErrorCode::Conflict),
        E::PaymentTotalMismatch => Some(MutationErrorCode::BadInput),
        E::InvalidDecision(error) => Some(update_error_code(*error)),
        E::Pool(_) | E::Database(_) => None,
    }
}

fn update_item_persistence_error(
    error: &update_purchase_item::UpdatePurchaseItemPersistenceError,
) -> Option<MutationErrorCode> {
    use update_purchase_item::UpdatePurchaseItemPersistenceError as E;
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
        E::InvalidWorkflow(error) => Some(update_item_workflow_error_code(*error)),
        E::Pool(_) | E::Database(_) => None,
    }
}

fn bulk_update_persistence_error(
    error: &bulk_update_purchase_items::BulkUpdatePurchaseItemsPersistenceError,
) -> Option<MutationErrorCode> {
    use bulk_update_purchase_items::BulkUpdatePurchaseItemsPersistenceError as E;
    match error {
        E::InvalidUuid(_) | E::NumericOutOfRange(_) | E::InvalidDecision => {
            Some(MutationErrorCode::BadInput)
        }
        E::ItemNotFound
        | E::PurchaseNotFound
        | E::HouseholdMismatch
        | E::MemberNotFound
        | E::ReferenceNotFound(_) => Some(MutationErrorCode::NotFound),
        E::AccountingPeriodClosed => Some(MutationErrorCode::Conflict),
        E::InvalidWorkflow(error) => Some(bulk_update_workflow_error_code(*error)),
        E::Pool(_) | E::Database(_) => None,
    }
}

fn bulk_delete_persistence_error(
    error: &bulk_delete_purchase_items::BulkDeletePurchaseItemsPersistenceError,
) -> Option<MutationErrorCode> {
    use bulk_delete_purchase_items::BulkDeletePurchaseItemsPersistenceError as E;
    match error {
        E::InvalidUuid(_) | E::NumericOutOfRange(_) | E::InvalidDecision => {
            Some(MutationErrorCode::BadInput)
        }
        E::ItemNotFound | E::PurchaseNotFound | E::HouseholdMismatch | E::MemberNotFound => {
            Some(MutationErrorCode::NotFound)
        }
        E::AccountingPeriodClosed | E::InvalidPaymentAllocation | E::InvalidFundingSnapshot => {
            Some(MutationErrorCode::Conflict)
        }
        E::InvalidWorkflow(error) => Some(bulk_delete_workflow_error_code(*error)),
        E::Pool(_) | E::Database(_) => None,
    }
}

fn update_item_workflow_error_code(error: UpdatePurchaseItemError) -> MutationErrorCode {
    use UpdatePurchaseItemError as E;
    match error {
        E::Conflict | E::ClosedPurchase => MutationErrorCode::Conflict,
        _ => MutationErrorCode::BadInput,
    }
}

fn bulk_update_workflow_error_code(
    error: bookkeeping_workflows::bulk_update_items::BulkUpdateItemsError,
) -> MutationErrorCode {
    use bookkeeping_workflows::bulk_update_items::BulkUpdateItemsError as E;
    match error {
        E::MissingItem => MutationErrorCode::NotFound,
        E::Conflict | E::ClosedPurchase => MutationErrorCode::Conflict,
        _ => MutationErrorCode::BadInput,
    }
}

fn bulk_delete_workflow_error_code(
    error: bookkeeping_workflows::bulk_delete_items::BulkDeleteItemsError,
) -> MutationErrorCode {
    use bookkeeping_workflows::bulk_delete_items::BulkDeleteItemsError as E;
    match error {
        E::MissingItem => MutationErrorCode::NotFound,
        E::Conflict | E::ClosedPurchase | E::CorrectedItem | E::LastItem => {
            MutationErrorCode::Conflict
        }
        _ => MutationErrorCode::BadInput,
    }
}
