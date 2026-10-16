use bookkeeping_application::create_and_post_purchase::{
    CreatePurchasePersistence, CreatedPurchase, PurchaseAllocationContext,
    PurchaseItemAllocationRequest,
};
use bookkeeping_application::create_purchase_workflow as application;
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::{AsyncConnection, RunQueryDsl};
use uuid::Uuid;

use crate::{PgPool, schema::public};
use bookkeeping_postgres::create_purchase::{
    CreatePurchasePersistenceError, PersistCreatePurchase, persist_create_purchase_in_transaction,
};

#[derive(Debug)]
pub enum CreatePurchaseWorkflowAdapterError {
    Database(DieselError),
    Context(crate::purchase_command_context::PurchaseCommandContextError),
    Persistence(CreatePurchasePersistenceError),
}

const OPERATION: &str = "createPurchase";

pub async fn create_purchase_workflow(
    pool: &PgPool,
    command: application::CreatePurchaseWorkflowCommand,
) -> Result<
    CreatedPurchase,
    application::CreatePurchaseWorkflowError<CreatePurchaseWorkflowAdapterError>,
> {
    let mut pooled = pool.get().await.map_err(|error| {
        application::CreatePurchaseWorkflowError::Operations(
            CreatePurchaseWorkflowAdapterError::Persistence(CreatePurchasePersistenceError::Pool(
                error.to_string(),
            )),
        )
    })?;
    (*pooled)
        .transaction::<
            Result<_, application::CreatePurchaseWorkflowError<CreatePurchaseWorkflowAdapterError>>,
            DieselError,
            _,
        >(async |conn| {
            let mut operations = PgCreatePurchaseOperations { conn };
            Ok(application::create_purchase(&mut operations, command).await)
        })
        .await
        .map_err(|error| {
            application::CreatePurchaseWorkflowError::Operations(
                CreatePurchaseWorkflowAdapterError::Database(error),
            )
        })?
}

struct PgCreatePurchaseOperations<'a> {
    conn: &'a mut diesel_async::AsyncPgConnection,
}

impl application::CreatePurchaseOperations for PgCreatePurchaseOperations<'_> {
    type Error = CreatePurchaseWorkflowAdapterError;

    async fn require_active_member(
        &mut self,
        household_id: &str,
        actor_id: &str,
    ) -> Result<(), Self::Error> {
        use public::household_members::dsl as members;

        let household_id = parse_uuid(household_id, "household_id")
            .map_err(CreatePurchaseWorkflowAdapterError::Persistence)?;
        let actor_id = parse_uuid(actor_id, "actor_id")
            .map_err(CreatePurchaseWorkflowAdapterError::Persistence)?;
        let role = members::household_members
            .filter(members::household_id.eq(household_id))
            .filter(members::member_id.eq(actor_id))
            .filter(members::left_at.is_null())
            .select(members::role)
            .first::<String>(&mut *self.conn)
            .await
            .optional()
            .map_err(CreatePurchaseWorkflowAdapterError::Database)?;
        if horse_authorization::authorize(
            role.as_deref(),
            horse_authorization::Operation::FinancialData,
        )
        .is_err()
        {
            return Err(CreatePurchaseWorkflowAdapterError::Persistence(
                CreatePurchasePersistenceError::MemberNotFound,
            ));
        }
        Ok(())
    }

    async fn replay_existing(
        &mut self,
        persistence: &CreatePurchasePersistence,
    ) -> Result<Option<CreatedPurchase>, Self::Error> {
        if persistence.idempotency_key.is_empty() || persistence.idempotency_key.len() > 200 {
            return Err(CreatePurchaseWorkflowAdapterError::Persistence(
                CreatePurchasePersistenceError::InvalidIdempotencyKey,
            ));
        }
        let household_id = parse_uuid(&persistence.household_id, "household_id")
            .map_err(CreatePurchaseWorkflowAdapterError::Persistence)?;
        use public::idempotency_records::dsl as idem;
        let existing = idem::idempotency_records
            .filter(idem::household_id.eq(household_id))
            .filter(idem::operation.eq(OPERATION))
            .filter(idem::key.eq(&persistence.idempotency_key))
            .select((
                idem::request_fingerprint,
                idem::resource_kind,
                idem::resource_id,
            ))
            .first::<(Vec<u8>, String, Uuid)>(&mut *self.conn)
            .await
            .optional()
            .map_err(CreatePurchaseWorkflowAdapterError::Database)?;
        let Some((fingerprint, resource_kind, resource_id)) = existing else {
            return Ok(None);
        };
        if fingerprint.as_slice() != persistence.request_fingerprint || resource_kind != "purchase"
        {
            return Err(CreatePurchaseWorkflowAdapterError::Persistence(
                CreatePurchasePersistenceError::IdempotencyConflict,
            ));
        }
        Ok(Some(CreatedPurchase {
            purchase_id: resource_id.to_string(),
            created: false,
        }))
    }

    async fn load_allocation_context(
        &mut self,
        household_id: &str,
        merchant_key: &str,
        items: &[PurchaseItemAllocationRequest],
    ) -> Result<PurchaseAllocationContext, Self::Error> {
        let household_id = parse_uuid(household_id, "household_id")
            .map_err(CreatePurchaseWorkflowAdapterError::Persistence)?;
        let item_inputs = items
            .iter()
            .map(|item| {
                Ok(
                    crate::purchase_command_context::PurchaseItemAllocationInput {
                        category_id: item
                            .category_id
                            .as_deref()
                            .map(|id| parse_uuid(id, "category_id"))
                            .transpose()
                            .map_err(CreatePurchaseWorkflowAdapterError::Persistence)?,
                        product_variant_id: item
                            .product_variant_id
                            .as_deref()
                            .map(|id| parse_uuid(id, "product_variant_id"))
                            .transpose()
                            .map_err(CreatePurchaseWorkflowAdapterError::Persistence)?,
                        merchant_key: merchant_key.to_owned(),
                        allocations: item
                            .allocations
                            .iter()
                            .map(|allocation| {
                                Ok((
                                    parse_uuid(&allocation.pool_id, "pool_id")
                                        .map_err(CreatePurchaseWorkflowAdapterError::Persistence)?,
                                    allocation.weight,
                                ))
                            })
                            .collect::<Result<Vec<_>, Self::Error>>()?,
                    },
                )
            })
            .collect::<Result<Vec<_>, Self::Error>>()?;
        let context =
            crate::purchase_command_context::load_purchase_command_context_in_transaction(
                &mut *self.conn,
                household_id,
                merchant_key,
                &item_inputs,
            )
            .await
            .map_err(CreatePurchaseWorkflowAdapterError::Context)?;
        Ok(PurchaseAllocationContext {
            allocation_rule_version: context.allocation_rule_version,
            items: context
                .items
                .into_iter()
                .map(|item| {
                    bookkeeping_application::create_and_post_purchase::PurchaseItemAllocationContext {
                        category_id: item.category_id.map(|id| id.to_string()),
                        allocations: item
                            .allocations
                            .into_iter()
                            .map(|allocation| {
                                bookkeeping_application::create_and_post_purchase::ResolvedPurchaseAllocation {
                                    pool_id: allocation.pool_id.to_string(),
                                    weight: allocation.weight,
                                    rule_version: allocation.rule_version,
                                    rule_id: allocation.rule_id.map(|id| id.to_string()),
                                }
                            })
                            .collect(),
                        allocation_override: item.allocation_override,
                    }
                })
                .collect(),
        })
    }

    async fn persist_create(
        &mut self,
        persistence: CreatePurchasePersistence,
        decision: bookkeeping_application::create_purchase::CreatePurchaseDecision,
    ) -> Result<CreatedPurchase, Self::Error> {
        let mut cross_context = crate::purchase_creation_context::PurchaseCreationContext;
        let persisted = persist_create_purchase_in_transaction(
            &mut *self.conn,
            PersistCreatePurchase {
                household_id: persistence.household_id,
                actor_id: persistence.actor_id,
                idempotency_key: persistence.idempotency_key,
                request_fingerprint: persistence.request_fingerprint,
            },
            decision,
            &mut cross_context,
        )
        .await
        .map_err(CreatePurchaseWorkflowAdapterError::Persistence)?;
        Ok(CreatedPurchase {
            purchase_id: persisted.purchase_id,
            created: persisted.created,
        })
    }
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, CreatePurchasePersistenceError> {
    Uuid::parse_str(value).map_err(|_| CreatePurchasePersistenceError::InvalidUuid(field))
}
