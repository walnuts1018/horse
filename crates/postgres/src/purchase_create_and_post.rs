use diesel::{ExpressionMethods, OptionalExtension, QueryDsl};
use diesel_async::{AsyncConnection, RunQueryDsl};
use uuid::Uuid;

use bookkeeping_workflows::decide_post_purchase;

use crate::{
    PgPool,
    create_purchase::{
        CreatePurchasePersistenceError, PersistCreatePurchase, PersistedCreatePurchase,
        persist_create_purchase_in_transaction,
    },
    post_purchase::{
        PersistPostPurchase, PostPurchasePersistenceError, persist_post_purchase_in_transaction,
    },
    post_purchase_loader::{PostPurchaseLoadError, load_post_purchase_command},
    schema::public,
};

#[derive(Debug)]
pub enum CreateAndPostPurchaseError {
    Pool(String),
    Database(diesel::result::Error),
    Create(CreatePurchasePersistenceError),
    Load(PostPurchaseLoadError),
    Post(PostPurchasePersistenceError),
}

pub async fn persist_create_and_post_purchase(
    pool: &PgPool,
    create: PersistCreatePurchase,
    decision: bookkeeping_workflows::create_purchase::CreatePurchaseDecision,
    post_fingerprint: [u8; 32],
) -> Result<PersistedCreatePurchase, CreateAndPostPurchaseError> {
    let household_id = Uuid::parse_str(&create.household_id)
        .map_err(|_| CreateAndPostPurchaseError::Create(CreatePurchasePersistenceError::InvalidUuid("household_id")))?;
    let actor_id = Uuid::parse_str(&create.actor_id)
        .map_err(|_| CreateAndPostPurchaseError::Create(CreatePurchasePersistenceError::InvalidUuid("actor_id")))?;
    let mut conn = pool
        .get()
        .await
        .map_err(|error| CreateAndPostPurchaseError::Pool(error.to_string()))?;
    (&mut *conn)
        .transaction::<PersistedCreatePurchase, CreateAndPostPurchaseError, _>(async |conn| {
            use public::purchases::dsl as purchases;

            let created = persist_create_purchase_in_transaction(conn, create.clone(), decision)
                .await
                .map_err(CreateAndPostPurchaseError::Create)?;
            let purchase_id = Uuid::parse_str(&created.purchase_id)
                .map_err(|_| CreateAndPostPurchaseError::Create(CreatePurchasePersistenceError::InvalidUuid("purchase_id")))?;
            let purchase = purchases::purchases
                .filter(purchases::household_id.eq(household_id))
                .filter(purchases::id.eq(purchase_id))
                .select((purchases::revision, purchases::status, purchases::merchant))
                .first::<(i64, String, String)>(conn)
                .await
                .optional()
                .map_err(CreateAndPostPurchaseError::Database)?
                .ok_or(CreateAndPostPurchaseError::Create(CreatePurchasePersistenceError::HouseholdNotFound))?;
            if purchase.1 != "DRAFT" {
                return Ok(created);
            }
            let expected_revision = u64::try_from(purchase.0)
                .map_err(|_| CreateAndPostPurchaseError::Create(CreatePurchasePersistenceError::NumericOutOfRange("purchase.revision")))?;
            let loaded = load_post_purchase_command(
                conn,
                &household_id.to_string(),
                &purchase_id.to_string(),
                expected_revision,
            )
            .await
            .map_err(CreateAndPostPurchaseError::Load)?;
            let post_decision = decide_post_purchase(loaded.command)
                .map_err(|_| CreateAndPostPurchaseError::Post(PostPurchasePersistenceError::InvalidDecision))?;
            persist_post_purchase_in_transaction(
                conn,
                PersistPostPurchase {
                    household_id: household_id.to_string(),
                    actor_id: actor_id.to_string(),
                    expected_revision,
                    ledger_description: purchase.2,
                    idempotency_key: create.idempotency_key,
                    request_fingerprint: post_fingerprint,
                    resolved_payments: loaded.resolved_payments,
                },
                post_decision,
            )
            .await
            .map_err(CreateAndPostPurchaseError::Post)?;
            Ok(created)
        })
        .await
}
