use chrono::{DateTime, Utc};
use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use serde_json::Value;
use uuid::Uuid;

use bookkeeping_application::shared_draft::{
    CreateSharedPurchaseDraftCommand, DraftReference, DraftReferenceKind,
    GetSharedPurchaseDraftCommand, SharedPurchaseDraftError, UpdateSharedPurchaseDraftCommand,
    decide_create_shared_purchase_draft, decide_get_shared_purchase_draft,
    decide_update_shared_purchase_draft, validate_shared_purchase_draft_payload,
};

use crate::{PgPool, schema::public};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SharedPurchaseDraft {
    pub id: String,
    pub household_id: String,
    pub revision: i64,
    pub payload: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdateSharedPurchaseDraftResult {
    pub draft: SharedPurchaseDraft,
    pub conflict: bool,
}

#[derive(Debug)]
pub enum SharedPurchaseDraftPersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    InvalidWorkflow(SharedPurchaseDraftError),
    InvalidPayload(&'static str),
    HouseholdNotFound,
    DraftNotFound,
    RevisionOutOfRange,
}

impl From<DieselError> for SharedPurchaseDraftPersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

type DraftRow = (Uuid, Uuid, i64, Value, DateTime<Utc>, DateTime<Utc>);

pub async fn create_shared_purchase_draft(
    pool: &PgPool,
    actor_id: &str,
    household_id: &str,
    payload: &str,
) -> Result<SharedPurchaseDraft, SharedPurchaseDraftPersistenceError> {
    let actor_id = parse_uuid(actor_id, "actor_id")?;
    let household_id = parse_uuid(household_id, "household_id")?;
    let (canonical_payload, references) = parse_payload(payload)?;
    let draft_id = Uuid::now_v7();
    let decision = decide_create_shared_purchase_draft(CreateSharedPurchaseDraftCommand {
        actor_id: actor_id.to_string(),
        household_id: household_id.to_string(),
        draft_id: draft_id.to_string(),
        payload: payload.to_owned(),
        references,
    })
    .map_err(SharedPurchaseDraftPersistenceError::InvalidWorkflow)?;

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| SharedPurchaseDraftPersistenceError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<SharedPurchaseDraft, SharedPurchaseDraftPersistenceError, _>(
            async move |conn| {
                validate_household_and_membership(conn, household_id, actor_id).await?;
                validate_references(conn, household_id, &decision.references).await?;
                use public::shared_purchase_drafts::dsl as drafts;
                diesel::insert_into(public::shared_purchase_drafts::table)
                    .values((
                        drafts::id.eq(draft_id),
                        drafts::household_id.eq(household_id),
                        drafts::revision.eq(1_i64),
                        drafts::payload.eq(serde_json::from_str::<Value>(&canonical_payload)
                            .map_err(|_| {
                                SharedPurchaseDraftPersistenceError::InvalidPayload(
                                    "payload must be a JSON object",
                                )
                            })?),
                    ))
                    .execute(&mut *conn)
                    .await?;
                load_draft(conn, draft_id)
                    .await?
                    .ok_or(SharedPurchaseDraftPersistenceError::DraftNotFound)
            },
        )
        .await
}

pub async fn get_shared_purchase_draft(
    pool: &PgPool,
    actor_id: &str,
    draft_id: &str,
) -> Result<SharedPurchaseDraft, SharedPurchaseDraftPersistenceError> {
    let decision = decide_get_shared_purchase_draft(GetSharedPurchaseDraftCommand {
        actor_id: actor_id.to_owned(),
        draft_id: draft_id.to_owned(),
    })
    .map_err(SharedPurchaseDraftPersistenceError::InvalidWorkflow)?;
    let actor_id = parse_uuid(&decision.actor_id, "actor_id")?;
    let draft_id = parse_uuid(&decision.draft_id, "draft_id")?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| SharedPurchaseDraftPersistenceError::Pool(error.to_string()))?;
    let conn = &mut *pooled;
    let draft = load_draft(conn, draft_id)
        .await?
        .ok_or(SharedPurchaseDraftPersistenceError::DraftNotFound)?;
    validate_membership(
        conn,
        Uuid::parse_str(&draft.household_id).expect("database UUID"),
        actor_id,
    )
    .await?;
    Ok(draft)
}

pub async fn update_shared_purchase_draft(
    pool: &PgPool,
    actor_id: &str,
    draft_id: &str,
    expected_revision: i64,
    payload: &str,
) -> Result<UpdateSharedPurchaseDraftResult, SharedPurchaseDraftPersistenceError> {
    let decision = decide_update_shared_purchase_draft(UpdateSharedPurchaseDraftCommand {
        actor_id: actor_id.to_owned(),
        draft_id: draft_id.to_owned(),
        expected_revision,
        payload: payload.to_owned(),
        references: Vec::new(),
    })
    .map_err(SharedPurchaseDraftPersistenceError::InvalidWorkflow)?;
    let actor_id = parse_uuid(&decision.actor_id, "actor_id")?;
    let draft_id = parse_uuid(&decision.draft_id, "draft_id")?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| SharedPurchaseDraftPersistenceError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<UpdateSharedPurchaseDraftResult, SharedPurchaseDraftPersistenceError, _>(
            async move |conn| {
                use public::shared_purchase_drafts::dsl as drafts;
                let current = public::shared_purchase_drafts::table
                    .filter(drafts::id.eq(draft_id))
                    .for_update()
                    .select((
                        drafts::id,
                        drafts::household_id,
                        drafts::revision,
                        drafts::payload,
                        drafts::created_at,
                        drafts::updated_at,
                    ))
                    .first::<DraftRow>(conn)
                    .await
                    .optional()?
                    .ok_or(SharedPurchaseDraftPersistenceError::DraftNotFound)?;
                validate_membership(conn, current.1, actor_id).await?;
                if current.2 != decision.expected_revision {
                    return Ok(UpdateSharedPurchaseDraftResult {
                        draft: draft_from_row(current),
                        conflict: true,
                    });
                }

                let (canonical_payload, references) = parse_payload(&decision.payload)?;
                validate_shared_purchase_draft_payload(&decision.payload, &references)
                    .map_err(SharedPurchaseDraftPersistenceError::InvalidWorkflow)?;
                validate_references(conn, current.1, &references).await?;
                let revision = current
                    .2
                    .checked_add(1)
                    .ok_or(SharedPurchaseDraftPersistenceError::RevisionOutOfRange)?;
                let payload = serde_json::from_str::<Value>(&canonical_payload).map_err(|_| {
                    SharedPurchaseDraftPersistenceError::InvalidPayload(
                        "payload must be a JSON object",
                    )
                })?;
                diesel::update(
                    public::shared_purchase_drafts::table.filter(drafts::id.eq(draft_id)),
                )
                .set((
                    drafts::revision.eq(revision),
                    drafts::payload.eq(payload),
                    drafts::updated_at.eq(now),
                ))
                .execute(&mut *conn)
                .await?;
                let updated = load_draft(conn, draft_id)
                    .await?
                    .ok_or(SharedPurchaseDraftPersistenceError::DraftNotFound)?;
                Ok(UpdateSharedPurchaseDraftResult {
                    draft: updated,
                    conflict: false,
                })
            },
        )
        .await
}

async fn validate_household_and_membership(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    actor_id: Uuid,
) -> Result<(), SharedPurchaseDraftPersistenceError> {
    use public::households::dsl as households;
    let exists = households::households
        .filter(households::id.eq(household_id))
        .select(households::id)
        .first::<Uuid>(conn)
        .await
        .optional()?;
    if exists.is_none() {
        return Err(SharedPurchaseDraftPersistenceError::HouseholdNotFound);
    }
    validate_membership(conn, household_id, actor_id).await
}

async fn validate_membership(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    actor_id: Uuid,
) -> Result<(), SharedPurchaseDraftPersistenceError> {
    use public::household_members::dsl as members;
    let member = members::household_members
        .filter(members::household_id.eq(household_id))
        .filter(members::member_id.eq(actor_id))
        .filter(members::left_at.is_null())
        .for_share()
        .select(members::member_id)
        .first::<Uuid>(conn)
        .await
        .optional()?;
    if member.is_none() {
        return Err(SharedPurchaseDraftPersistenceError::DraftNotFound);
    }
    Ok(())
}

async fn validate_references(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    references: &[DraftReference],
) -> Result<(), SharedPurchaseDraftPersistenceError> {
    for kind in [
        DraftReferenceKind::Account,
        DraftReferenceKind::Category,
        DraftReferenceKind::Pool,
        DraftReferenceKind::ProductVariant,
    ] {
        let ids = references
            .iter()
            .filter(|reference| reference.kind == kind)
            .map(|reference| parse_uuid(&reference.id, "payload_reference"))
            .collect::<Result<Vec<_>, _>>()?;
        if ids.is_empty() {
            continue;
        }
        let found = match kind {
            DraftReferenceKind::Account => {
                use public::accounts::dsl as table;
                table::accounts
                    .filter(table::household_id.eq(household_id))
                    .filter(table::id.eq_any(&ids))
                    .select(table::id)
                    .load::<Uuid>(conn)
                    .await?
            }
            DraftReferenceKind::Category => {
                use public::categories::dsl as table;
                table::categories
                    .filter(table::household_id.eq(household_id))
                    .filter(table::id.eq_any(&ids))
                    .select(table::id)
                    .load::<Uuid>(conn)
                    .await?
            }
            DraftReferenceKind::Pool => {
                use public::pools::dsl as table;
                table::pools
                    .filter(table::household_id.eq(household_id))
                    .filter(table::id.eq_any(&ids))
                    .select(table::id)
                    .load::<Uuid>(conn)
                    .await?
            }
            DraftReferenceKind::ProductVariant => {
                use public::product_variants::dsl as table;
                table::product_variants
                    .filter(table::household_id.eq(household_id))
                    .filter(table::id.eq_any(&ids))
                    .select(table::id)
                    .load::<Uuid>(conn)
                    .await?
            }
        };
        if found.len() != ids.len() {
            return Err(SharedPurchaseDraftPersistenceError::InvalidPayload(
                reference_error(kind),
            ));
        }
    }
    Ok(())
}

async fn load_draft(
    conn: &mut crate::AsyncPgConnection,
    draft_id: Uuid,
) -> Result<Option<SharedPurchaseDraft>, SharedPurchaseDraftPersistenceError> {
    use public::shared_purchase_drafts::dsl as drafts;
    let row = public::shared_purchase_drafts::table
        .filter(drafts::id.eq(draft_id))
        .select((
            drafts::id,
            drafts::household_id,
            drafts::revision,
            drafts::payload,
            drafts::created_at,
            drafts::updated_at,
        ))
        .first::<DraftRow>(conn)
        .await
        .optional()?;
    Ok(row.map(draft_from_row))
}

fn draft_from_row(row: DraftRow) -> SharedPurchaseDraft {
    SharedPurchaseDraft {
        id: row.0.to_string(),
        household_id: row.1.to_string(),
        revision: row.2,
        payload: row.3.to_string(),
        created_at: row.4,
        updated_at: row.5,
    }
}

fn parse_payload(
    payload: &str,
) -> Result<(String, Vec<DraftReference>), SharedPurchaseDraftPersistenceError> {
    use DraftReferenceKind::{Account, Category, Pool, ProductVariant};
    if payload.is_empty() || payload.len() > 256 * 1024 {
        return Err(SharedPurchaseDraftPersistenceError::InvalidPayload(
            "shared purchase draft payload has invalid size",
        ));
    }
    let root: Value = serde_json::from_str(payload).map_err(|_| {
        SharedPurchaseDraftPersistenceError::InvalidPayload(
            "shared purchase draft payload must be a JSON object",
        )
    })?;
    let object = root
        .as_object()
        .ok_or(SharedPurchaseDraftPersistenceError::InvalidPayload(
            "shared purchase draft payload must be a JSON object",
        ))?;
    check_keys(
        object,
        &[
            "merchant",
            "occurredAt",
            "paymentAccountId",
            "paymentAllocations",
            "paymentAutoTotal",
            "paymentAmountManuallyEdited",
            "itemName",
            "itemAmount",
            "selectedProductVariantId",
            "categoryId",
            "items",
        ],
    )?;
    for field in [
        "merchant",
        "occurredAt",
        "itemName",
        "itemAmount",
        "categoryId",
    ] {
        required_string(object, field)?;
    }
    for field in ["paymentAccountId", "selectedProductVariantId"] {
        optional_string(object, field)?;
    }
    for field in ["paymentAutoTotal", "paymentAmountManuallyEdited"] {
        if let Some(value) = object.get(field)
            && !value.is_boolean()
        {
            return Err(SharedPurchaseDraftPersistenceError::InvalidPayload(
                "optional payment fields must be booleans",
            ));
        }
    }
    let mut references = Vec::new();
    add_reference(object, "paymentAccountId", Account, &mut references)?;
    add_reference(
        object,
        "selectedProductVariantId",
        ProductVariant,
        &mut references,
    )?;
    add_reference(object, "categoryId", Category, &mut references)?;

    if let Some(allocations) = object.get("paymentAllocations") {
        let allocations =
            allocations
                .as_array()
                .ok_or(SharedPurchaseDraftPersistenceError::InvalidPayload(
                    "paymentAllocations must be an array",
                ))?;
        for allocation in allocations {
            let allocation = allocation.as_object().ok_or(
                SharedPurchaseDraftPersistenceError::InvalidPayload(
                    "payment allocation must be an object",
                ),
            )?;
            check_keys(allocation, &["paymentAccountId", "amountYen"])?;
            required_string(allocation, "paymentAccountId")?;
            required_string(allocation, "amountYen")?;
            add_reference(allocation, "paymentAccountId", Account, &mut references)?;
        }
    }

    let items = object.get("items").and_then(Value::as_array).ok_or(
        SharedPurchaseDraftPersistenceError::InvalidPayload("items must be an array"),
    )?;
    for item in items {
        let item = item
            .as_object()
            .ok_or(SharedPurchaseDraftPersistenceError::InvalidPayload(
                "purchase draft item must be an object",
            ))?;
        check_keys(
            item,
            &[
                "key",
                "name",
                "amount",
                "quantity",
                "quantityUnit",
                "categoryId",
                "productVariantId",
                "allocations",
            ],
        )?;
        for field in ["key", "name", "amount", "categoryId"] {
            required_string(item, field)?;
        }
        for field in ["quantity", "quantityUnit", "productVariantId"] {
            optional_string(item, field)?;
        }
        add_reference(item, "categoryId", Category, &mut references)?;
        add_reference(item, "productVariantId", ProductVariant, &mut references)?;
        let allocations = item.get("allocations").and_then(Value::as_array).ok_or(
            SharedPurchaseDraftPersistenceError::InvalidPayload("allocations must be an array"),
        )?;
        for allocation in allocations {
            let allocation = allocation.as_object().ok_or(
                SharedPurchaseDraftPersistenceError::InvalidPayload("allocation must be an object"),
            )?;
            check_keys(allocation, &["poolId", "weight"])?;
            required_string(allocation, "poolId")?;
            if !allocation.get("weight").is_some_and(Value::is_number) {
                return Err(SharedPurchaseDraftPersistenceError::InvalidPayload(
                    "allocation weight must be a finite number",
                ));
            }
            add_reference(allocation, "poolId", Pool, &mut references)?;
        }
    }

    references
        .sort_by(|left, right| (left.kind as u8, &left.id).cmp(&(right.kind as u8, &right.id)));
    references.dedup();
    let canonical = serde_json::to_string(&root).map_err(|_| {
        SharedPurchaseDraftPersistenceError::InvalidPayload(
            "could not encode shared purchase draft payload",
        )
    })?;
    Ok((canonical, references))
}

fn check_keys(
    object: &serde_json::Map<String, Value>,
    allowed: &[&str],
) -> Result<(), SharedPurchaseDraftPersistenceError> {
    if object.keys().any(|key| !allowed.contains(&key.as_str())) {
        return Err(SharedPurchaseDraftPersistenceError::InvalidPayload(
            "unsupported purchase draft field",
        ));
    }
    Ok(())
}

fn required_string<'a>(
    object: &'a serde_json::Map<String, Value>,
    field: &'static str,
) -> Result<&'a str, SharedPurchaseDraftPersistenceError> {
    object.get(field).and_then(Value::as_str).ok_or(
        SharedPurchaseDraftPersistenceError::InvalidPayload(
            "required purchase draft field must be a string",
        ),
    )
}

fn optional_string<'a>(
    object: &'a serde_json::Map<String, Value>,
    field: &'static str,
) -> Result<Option<&'a str>, SharedPurchaseDraftPersistenceError> {
    object
        .get(field)
        .map(|value| {
            value
                .as_str()
                .ok_or(SharedPurchaseDraftPersistenceError::InvalidPayload(
                    "optional purchase draft field must be a string",
                ))
        })
        .transpose()
}

fn add_reference(
    object: &serde_json::Map<String, Value>,
    field: &'static str,
    kind: DraftReferenceKind,
    references: &mut Vec<DraftReference>,
) -> Result<(), SharedPurchaseDraftPersistenceError> {
    if let Some(value) = optional_string(object, field)?
        && !value.is_empty()
    {
        let id = Uuid::parse_str(value).map_err(|_| {
            SharedPurchaseDraftPersistenceError::InvalidPayload(
                "invalid purchase draft reference UUID",
            )
        })?;
        references.push(DraftReference {
            kind,
            id: id.to_string(),
        });
    }
    Ok(())
}

fn reference_error(kind: DraftReferenceKind) -> &'static str {
    match kind {
        DraftReferenceKind::Account => "account reference does not belong to household",
        DraftReferenceKind::Category => "category reference does not belong to household",
        DraftReferenceKind::Pool => "pool reference does not belong to household",
        DraftReferenceKind::ProductVariant => {
            "product variant reference does not belong to household"
        }
    }
}

fn parse_uuid(
    value: &str,
    field: &'static str,
) -> Result<Uuid, SharedPurchaseDraftPersistenceError> {
    Uuid::parse_str(value).map_err(|_| SharedPurchaseDraftPersistenceError::InvalidUuid(field))
}
