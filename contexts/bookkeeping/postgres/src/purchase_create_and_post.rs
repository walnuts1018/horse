use bookkeeping_application::create_and_post_purchase as application;
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::{AsyncConnection, RunQueryDsl};
use uuid::Uuid;

use crate::{
    PgPool,
    create_purchase::{
        CreatePurchasePersistenceError, PersistCreatePurchase, PersistedCreatePurchase,
        persist_create_purchase_in_transaction,
    },
    post_purchase::{
        PersistPostPurchase, PersistedPostPurchase, PostPurchasePersistenceError,
        persist_post_purchase_in_transaction,
    },
    post_purchase_loader::{PostPurchaseLoadError, load_post_purchase_command},
    purchase_command_context::{
        PurchaseCommandContextError, PurchaseItemAllocationInput,
        load_purchase_command_context_in_transaction,
    },
    schema::public,
};

#[derive(Debug)]
pub enum CreateAndPostPurchaseError {
    Pool(String),
    Database(DieselError),
    Context(PurchaseCommandContextError),
    Create(CreatePurchasePersistenceError),
    Load(PostPurchaseLoadError),
    Post(PostPurchasePersistenceError),
    CreateDecision(bookkeeping_application::create_purchase::CreatePurchaseError),
    PostDecision(bookkeeping_application::PostPurchaseError),
    AllocationContextMismatch,
    CreatedPurchaseMissing,
}

pub async fn create_and_post_purchase(
    pool: &PgPool,
    command: application::CreateAndPostPurchaseCommand,
) -> Result<application::CreateAndPostPurchaseResult, CreateAndPostPurchaseError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|error| CreateAndPostPurchaseError::Pool(error.to_string()))?;
    (&mut *conn)
        .transaction::<application::CreateAndPostPurchaseResult, CreateAndPostPurchaseError, _>(
            async |conn| {
                let mut operations = PurchaseCreateAndPostOperations { conn };
                application::create_and_post_purchase(&mut operations, command)
                    .await
                    .map_err(map_workflow_error)
            },
        )
        .await
}

struct PurchaseCreateAndPostOperations<'a> {
    conn: &'a mut diesel_async::AsyncPgConnection,
}

impl application::CreateAndPostPurchaseOperations for PurchaseCreateAndPostOperations<'_> {
    type Error = CreateAndPostPurchaseError;

    async fn require_active_member(
        &mut self,
        household_id: &str,
        actor_id: &str,
    ) -> Result<(), Self::Error> {
        use public::household_members::dsl as members;

        let household_id = parse_uuid(household_id, "household_id")?;
        let actor_id = parse_uuid(actor_id, "actor_id")?;
        let member = members::household_members
            .filter(members::household_id.eq(household_id))
            .filter(members::member_id.eq(actor_id))
            .filter(members::left_at.is_null())
            .select(members::member_id)
            .first::<Uuid>(&mut *self.conn)
            .await
            .optional()
            .map_err(CreateAndPostPurchaseError::Database)?
            .is_some();
        if !member {
            return Err(CreateAndPostPurchaseError::Create(
                CreatePurchasePersistenceError::MemberNotFound,
            ));
        }
        Ok(())
    }

    async fn load_allocation_context(
        &mut self,
        household_id: &str,
        merchant_key: &str,
        items: &[application::PurchaseItemAllocationRequest],
    ) -> Result<application::PurchaseAllocationContext, Self::Error> {
        let household_id = parse_uuid(household_id, "household_id")?;
        let inputs = items
            .iter()
            .map(|item| {
                Ok(PurchaseItemAllocationInput {
                    category_id: item
                        .category_id
                        .as_deref()
                        .map(|value| parse_uuid(value, "category_id"))
                        .transpose()?,
                    product_variant_id: item
                        .product_variant_id
                        .as_deref()
                        .map(|value| parse_uuid(value, "product_variant_id"))
                        .transpose()?,
                    merchant_key: merchant_key.to_owned(),
                    allocations: item
                        .allocations
                        .iter()
                        .map(|allocation| {
                            Ok((
                                parse_uuid(&allocation.pool_id, "pool_id")?,
                                allocation.weight,
                            ))
                        })
                        .collect::<Result<Vec<_>, CreateAndPostPurchaseError>>()?,
                })
            })
            .collect::<Result<Vec<_>, CreateAndPostPurchaseError>>()?;
        let context = load_purchase_command_context_in_transaction(
            &mut *self.conn,
            household_id,
            merchant_key,
            &inputs,
        )
        .await
        .map_err(CreateAndPostPurchaseError::Context)?;
        Ok(application::PurchaseAllocationContext {
            allocation_rule_version: context.allocation_rule_version,
            items: context
                .items
                .into_iter()
                .map(|item| application::PurchaseItemAllocationContext {
                    category_id: item.category_id.map(|id| id.to_string()),
                    allocations: item
                        .allocations
                        .into_iter()
                        .map(|allocation| application::ResolvedPurchaseAllocation {
                            pool_id: allocation.pool_id.to_string(),
                            weight: allocation.weight,
                            rule_version: allocation.rule_version,
                            rule_id: allocation.rule_id.map(|id| id.to_string()),
                        })
                        .collect(),
                    allocation_override: item.allocation_override,
                })
                .collect(),
        })
    }

    async fn persist_create(
        &mut self,
        persistence: application::CreatePurchasePersistence,
        decision: bookkeeping_application::create_purchase::CreatePurchaseDecision,
    ) -> Result<application::CreatedPurchase, Self::Error> {
        let persisted = persist_create_purchase_in_transaction(
            &mut *self.conn,
            PersistCreatePurchase {
                household_id: persistence.household_id,
                actor_id: persistence.actor_id,
                idempotency_key: persistence.idempotency_key,
                request_fingerprint: persistence.request_fingerprint,
            },
            decision,
        )
        .await
        .map_err(CreateAndPostPurchaseError::Create)?;
        Ok(created_purchase(persisted))
    }

    async fn load_purchase_state(
        &mut self,
        household_id: &str,
        purchase_id: &str,
    ) -> Result<Option<application::CreatedPurchaseState>, Self::Error> {
        use public::purchases::dsl as purchases;

        let household_id = parse_uuid(household_id, "household_id")?;
        let purchase_id = parse_uuid(purchase_id, "purchase_id")?;
        let state = purchases::purchases
            .filter(purchases::household_id.eq(household_id))
            .filter(purchases::id.eq(purchase_id))
            .select((purchases::revision, purchases::status, purchases::merchant))
            .first::<(i64, String, String)>(&mut *self.conn)
            .await
            .optional()
            .map_err(CreateAndPostPurchaseError::Database)?;
        state
            .map(|(revision, status, merchant)| {
                let revision = u64::try_from(revision).map_err(|_| {
                    CreateAndPostPurchaseError::Create(
                        CreatePurchasePersistenceError::NumericOutOfRange("purchase.revision"),
                    )
                })?;
                let lifecycle = match status.as_str() {
                    "DRAFT" => bookkeeping_application::PurchaseLifecycle::Draft,
                    "POSTED" | "CLOSED" => bookkeeping_application::PurchaseLifecycle::Posted,
                    _ => {
                        return Err(CreateAndPostPurchaseError::Create(
                            CreatePurchasePersistenceError::InvalidDecision,
                        ));
                    }
                };
                Ok(application::CreatedPurchaseState {
                    revision,
                    lifecycle,
                    merchant,
                })
            })
            .transpose()
    }

    async fn load_post_purchase(
        &mut self,
        household_id: &str,
        purchase_id: &str,
        expected_revision: u64,
    ) -> Result<application::LoadedPostPurchase, Self::Error> {
        let loaded = load_post_purchase_command(
            &mut *self.conn,
            household_id,
            purchase_id,
            expected_revision,
        )
        .await
        .map_err(CreateAndPostPurchaseError::Load)?;
        Ok(application::LoadedPostPurchase {
            command: loaded.command,
            resolved_payments: loaded
                .resolved_payments
                .into_iter()
                .map(|payment| application::ResolvedPostPurchasePayment {
                    line_no: payment.line_no,
                    account_id: payment.account_id,
                    amount: payment.amount,
                    is_fallback: payment.is_fallback,
                })
                .collect(),
        })
    }

    async fn persist_post(
        &mut self,
        persistence: application::PostPurchasePersistence,
        decision: bookkeeping_application::PostPurchaseDecision,
    ) -> Result<application::PostedPurchase, Self::Error> {
        let persisted = persist_post_purchase_in_transaction(
            &mut *self.conn,
            PersistPostPurchase {
                household_id: persistence.household_id,
                actor_id: persistence.actor_id,
                expected_revision: persistence.expected_revision,
                ledger_description: persistence.ledger_description,
                idempotency_key: persistence.idempotency_key,
                request_fingerprint: persistence.request_fingerprint,
                resolved_payments: persistence
                    .resolved_payments
                    .into_iter()
                    .map(
                        |payment| crate::post_purchase_loader::ResolvedPostPurchasePayment {
                            line_no: payment.line_no,
                            account_id: payment.account_id,
                            amount: payment.amount,
                            is_fallback: payment.is_fallback,
                        },
                    )
                    .collect(),
            },
            decision,
        )
        .await
        .map_err(CreateAndPostPurchaseError::Post)?;
        Ok(posted_purchase(persisted))
    }
}

fn map_workflow_error(
    error: application::CreateAndPostPurchaseError<CreateAndPostPurchaseError>,
) -> CreateAndPostPurchaseError {
    match error {
        application::CreateAndPostPurchaseError::CreateDecision(error) => {
            CreateAndPostPurchaseError::CreateDecision(error)
        }
        application::CreateAndPostPurchaseError::PostDecision(error) => {
            CreateAndPostPurchaseError::PostDecision(error)
        }
        application::CreateAndPostPurchaseError::AllocationContextMismatch => {
            CreateAndPostPurchaseError::AllocationContextMismatch
        }
        application::CreateAndPostPurchaseError::CreatedPurchaseMissing => {
            CreateAndPostPurchaseError::CreatedPurchaseMissing
        }
        application::CreateAndPostPurchaseError::Operations(error) => error,
    }
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, CreateAndPostPurchaseError> {
    Uuid::parse_str(value).map_err(|_| {
        CreateAndPostPurchaseError::Create(CreatePurchasePersistenceError::InvalidUuid(field))
    })
}

fn created_purchase(value: PersistedCreatePurchase) -> application::CreatedPurchase {
    application::CreatedPurchase {
        purchase_id: value.purchase_id,
        created: value.created,
    }
}

fn posted_purchase(value: PersistedPostPurchase) -> application::PostedPurchase {
    application::PostedPurchase {
        purchase_id: value.purchase_id,
    }
}
