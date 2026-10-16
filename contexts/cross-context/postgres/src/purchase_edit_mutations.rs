use std::{collections::BTreeSet, future::Future, pin::Pin};

use bookkeeping_application::{
    AccountingPeriod, PurchaseLifecycle, YearMonth,
    purchase_edit_mutations::{
        DeletedPurchaseItems, PurchaseEditFailure, PurchaseEditMutationHandler,
        PurchaseEditRequest, UpdatedPurchase, UpdatedPurchaseItem, UpdatedPurchaseItems,
    },
    workflow_failure::WorkflowFailure,
};
use bookkeeping_domain::purchase_commands::{UpdatePurchaseDraft, decide_update_purchase};
use bookkeeping_postgres::{
    bulk_delete_purchase_items::PersistBulkDeletePurchaseItems,
    bulk_update_purchase_items::PersistBulkUpdatePurchaseItems,
    update_purchase::PersistUpdatePurchase, update_purchase_item::PersistUpdatePurchaseItem,
};
use chrono::Datelike;
use uuid::Uuid;

use crate::{PgPool, purchase_command_result};

pub struct PostgresPurchaseEditMutationHandler {
    pool: PgPool,
}

impl PostgresPurchaseEditMutationHandler {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl PurchaseEditMutationHandler for PostgresPurchaseEditMutationHandler {
    fn update_purchase<'a>(
        &'a self,
        request: PurchaseEditRequest<bookkeeping_domain::purchase_commands::UpdatePurchaseCommand>,
    ) -> Pin<Box<dyn Future<Output = Result<UpdatedPurchase, PurchaseEditFailure>> + Send + 'a>>
    {
        Box::pin(async move {
            let purchase_id = parse_id(&request.value.purchase_id)?;
            let household_id =
                purchase_command_result::load_purchase_household_id(&self.pool, purchase_id)
                    .await
                    .map_err(infrastructure)?
                    .ok_or(PurchaseEditFailure::NotFound)?;
            if request
                .household_id
                .is_some_and(|expected| household_id != expected)
            {
                return Err(PurchaseEditFailure::NotFound);
            }
            require_member(&self.pool, household_id, request.actor_id).await?;
            let context = purchase_command_result::load_purchase_edit_context(
                &self.pool,
                household_id,
                purchase_id,
            )
            .await
            .map_err(infrastructure)?
            .ok_or(PurchaseEditFailure::NotFound)?;
            if context.status == "CLOSED" {
                return Err(PurchaseEditFailure::Conflict);
            }
            let draft = update_purchase_draft(context)?;
            let command = request.value;
            let decision = decide_update_purchase(command.clone(), draft)
                .map_err(|_| PurchaseEditFailure::InvalidInput)?;
            let persisted = crate::reference_data::persist_update_purchase(
                &self.pool,
                PersistUpdatePurchase {
                    household_id: household_id.to_string(),
                    actor_id: request.actor_id.to_string(),
                },
                command,
                decision,
            )
            .await
            .map_err(workflow_failure)?;
            Ok(UpdatedPurchase {
                household_id,
                purchase_id: parse_id(&persisted.purchase_id)?,
            })
        })
    }

    fn update_purchase_item<'a>(
        &'a self,
        request: PurchaseEditRequest<
            bookkeeping_domain::purchase_commands::UpdatePurchaseItemCommand,
        >,
    ) -> Pin<Box<dyn Future<Output = Result<UpdatedPurchaseItem, PurchaseEditFailure>> + Send + 'a>>
    {
        Box::pin(async move {
            let item_id = parse_id(&request.value.item_id)?;
            let household_id =
                purchase_command_result::load_purchase_item_household_id(&self.pool, item_id)
                    .await
                    .map_err(infrastructure)?
                    .ok_or(PurchaseEditFailure::NotFound)?;
            if request
                .household_id
                .is_some_and(|expected| household_id != expected)
            {
                return Err(PurchaseEditFailure::NotFound);
            }
            require_member(&self.pool, household_id, request.actor_id).await?;
            let persisted = crate::reference_data::persist_update_purchase_item(
                &self.pool,
                PersistUpdatePurchaseItem {
                    household_id: household_id.to_string(),
                    actor_id: request.actor_id.to_string(),
                },
                request.value,
            )
            .await
            .map_err(workflow_failure)?;
            Ok(UpdatedPurchaseItem {
                household_id,
                item_id,
                purchase_id: parse_id(&persisted.purchase_id)?,
            })
        })
    }

    fn bulk_update_purchase_items<'a>(
        &'a self,
        request: PurchaseEditRequest<(
            Vec<bookkeeping_domain::purchase_commands::BulkUpdateItemTarget>,
            bookkeeping_domain::purchase_commands::BulkUpdateItemPatch,
        )>,
    ) -> Pin<Box<dyn Future<Output = Result<UpdatedPurchaseItems, PurchaseEditFailure>> + Send + 'a>>
    {
        Box::pin(async move {
            let (targets, patch) = request.value;
            let household_id = household_for_targets(
                &self.pool,
                request.household_id,
                &targets
                    .iter()
                    .map(|target| target.item_id.as_str())
                    .collect::<Vec<_>>(),
            )
            .await?;
            require_member(&self.pool, household_id, request.actor_id).await?;
            let persisted = crate::reference_data::persist_bulk_update_purchase_items(
                &self.pool,
                PersistBulkUpdatePurchaseItems {
                    household_id: household_id.to_string(),
                    actor_id: request.actor_id.to_string(),
                },
                targets,
                patch,
            )
            .await
            .map_err(workflow_failure)?;
            let items = persisted
                .items
                .into_iter()
                .map(|(item_id, purchase_id)| {
                    Ok(UpdatedPurchaseItem {
                        household_id,
                        item_id: parse_id(&item_id)?,
                        purchase_id: parse_id(&purchase_id)?,
                    })
                })
                .collect::<Result<Vec<_>, PurchaseEditFailure>>()?;
            Ok(UpdatedPurchaseItems { items })
        })
    }

    fn bulk_delete_purchase_items<'a>(
        &'a self,
        request: PurchaseEditRequest<
            Vec<bookkeeping_domain::purchase_commands::BulkDeleteItemTarget>,
        >,
    ) -> Pin<Box<dyn Future<Output = Result<DeletedPurchaseItems, PurchaseEditFailure>> + Send + 'a>>
    {
        Box::pin(async move {
            let household_id = household_for_targets(
                &self.pool,
                request.household_id,
                &request
                    .value
                    .iter()
                    .map(|target| target.item_id.as_str())
                    .collect::<Vec<_>>(),
            )
            .await?;
            require_member(&self.pool, household_id, request.actor_id).await?;
            let persisted = crate::reference_data::persist_bulk_delete_purchase_items(
                &self.pool,
                PersistBulkDeletePurchaseItems {
                    household_id: household_id.to_string(),
                    actor_id: request.actor_id.to_string(),
                },
                request.value,
            )
            .await
            .map_err(workflow_failure)?;
            let item_ids = persisted
                .deleted_item_ids
                .iter()
                .map(|id| parse_id(id))
                .collect::<Result<Vec<_>, _>>()?;
            Ok(DeletedPurchaseItems {
                household_id,
                item_ids,
            })
        })
    }
}

async fn household_for_targets(
    pool: &PgPool,
    expected_household_id: Option<Uuid>,
    item_ids: &[&str],
) -> Result<Uuid, PurchaseEditFailure> {
    if item_ids.is_empty() {
        return Err(PurchaseEditFailure::InvalidInput);
    }
    let ids = item_ids
        .iter()
        .map(|id| parse_id(id))
        .collect::<Result<Vec<_>, _>>()?;
    let rows = purchase_command_result::load_purchase_item_household_ids(pool, &ids)
        .await
        .map_err(infrastructure)?;
    if rows.len() != ids.len() {
        return Err(PurchaseEditFailure::NotFound);
    }
    let households = rows
        .into_iter()
        .map(|(_, household_id)| household_id)
        .collect::<BTreeSet<_>>();
    if households.len() != 1 {
        return Err(PurchaseEditFailure::NotFound);
    }
    let household_id = *households
        .iter()
        .next()
        .ok_or(PurchaseEditFailure::NotFound)?;
    if expected_household_id.is_some_and(|expected| household_id != expected) {
        return Err(PurchaseEditFailure::NotFound);
    }
    Ok(household_id)
}

async fn require_member(
    pool: &PgPool,
    household_id: Uuid,
    actor_id: Uuid,
) -> Result<(), PurchaseEditFailure> {
    household_postgres::household_access::is_active_member(pool, household_id, actor_id)
        .await
        .map_err(infrastructure)?
        .then_some(())
        .ok_or(PurchaseEditFailure::NotFound)
}

fn update_purchase_draft(
    value: purchase_command_result::PurchaseEditContext,
) -> Result<UpdatePurchaseDraft, PurchaseEditFailure> {
    let field_revisions = value
        .field_revisions
        .as_object()
        .ok_or_else(|| PurchaseEditFailure::Infrastructure("invalid field revisions".into()))?
        .iter()
        .map(|(key, value)| {
            value
                .as_u64()
                .map(|revision| (key.clone(), revision))
                .ok_or_else(|| PurchaseEditFailure::Infrastructure("invalid field revision".into()))
        })
        .collect::<Result<_, _>>()?;
    let occurred_at = value.occurred_at;
    let local_date = occurred_at
        .with_timezone(
            &chrono::FixedOffset::east_opt(9 * 60 * 60)
                .ok_or_else(|| PurchaseEditFailure::Infrastructure("invalid time zone".into()))?,
        )
        .date_naive();
    let month = YearMonth::new(
        u16::try_from(local_date.year())
            .map_err(|_| PurchaseEditFailure::Infrastructure("invalid purchase year".into()))?,
        u8::try_from(local_date.month())
            .map_err(|_| PurchaseEditFailure::Infrastructure("invalid purchase month".into()))?,
    )
    .map_err(|_| PurchaseEditFailure::Infrastructure("invalid purchase period".into()))?;
    Ok(UpdatePurchaseDraft {
        revision: u64::try_from(value.revision)
            .map_err(|_| PurchaseEditFailure::Infrastructure("invalid purchase revision".into()))?,
        lifecycle: match value.status.as_str() {
            "DRAFT" => PurchaseLifecycle::Draft,
            "POSTED" | "CLOSED" => PurchaseLifecycle::Posted,
            _ => {
                return Err(PurchaseEditFailure::Infrastructure(
                    "invalid purchase status".into(),
                ));
            }
        },
        accounting_period: AccountingPeriod::open(month, 1)
            .map_err(|_| PurchaseEditFailure::Infrastructure("invalid purchase period".into()))?
            .into_open()
            .map_err(|_| PurchaseEditFailure::Infrastructure("invalid purchase period".into()))?,
        occurred_at: occurred_at.to_rfc3339(),
        date_precision: value.date_precision,
        merchant: value.merchant,
        field_revisions,
    })
}

fn parse_id(value: &str) -> Result<Uuid, PurchaseEditFailure> {
    Uuid::parse_str(value).map_err(|_| PurchaseEditFailure::InvalidInput)
}

fn infrastructure(error: impl std::fmt::Debug) -> PurchaseEditFailure {
    PurchaseEditFailure::Infrastructure(format!("{error:?}"))
}

fn workflow_failure<E: std::fmt::Debug>(
    error: WorkflowFailure<bookkeeping_application::workflow_failure::WorkflowRejection, E>,
) -> PurchaseEditFailure {
    match error {
        WorkflowFailure::Decision(_) => PurchaseEditFailure::InvalidInput,
        WorkflowFailure::NotFound => PurchaseEditFailure::NotFound,
        WorkflowFailure::Conflict => PurchaseEditFailure::Conflict,
        WorkflowFailure::Infrastructure(error) => infrastructure(error),
    }
}
