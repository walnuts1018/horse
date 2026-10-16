use bookkeeping_application::workflow_failure::{WorkflowFailure, WorkflowRejection};
use bookkeeping_postgres::{
    bulk_delete_purchase_items::{
        BulkDeletePurchaseItemsPersistenceError, PersistBulkDeletePurchaseItems,
        PersistedBulkDeletePurchaseItems,
    },
    post_purchase::PostPurchaseWorkflowAdapterError,
    post_purchase_loader::PostPurchaseReferenceData,
    save_purchase_item_edits::{
        PersistSavePurchaseItemEdits, SavePurchaseItemEditsPersistenceError,
    },
    schema::public,
    shared_draft::{
        SharedDraftReferenceData, SharedPurchaseDraft, SharedPurchaseDraftPersistenceError,
        UpdateSharedPurchaseDraftResult,
    },
    update_purchase::{
        PersistUpdatePurchase, PersistedUpdatePurchase, UpdatePurchasePersistenceError,
    },
    update_purchase_item::{
        PersistUpdatePurchaseItem, PersistedUpdatePurchaseItem, ProductLearningRequest,
        UpdatePurchaseItemPersistenceError, UpdatePurchaseItemReferenceData,
    },
};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

pub struct UpdatePurchaseItemReferences;

struct ProductLearningPostgresOperations<'a> {
    conn: &'a mut diesel_async::AsyncPgConnection,
}

impl catalog_application::product_learning::ProductLearningOperations
    for ProductLearningPostgresOperations<'_>
{
    type Error = DieselError;

    async fn persist_product_learning(
        &mut self,
        decision: catalog_application::product_learning::ProductLearningDecision,
    ) -> Result<(), Self::Error> {
        catalog_postgres::product_learning::persist_product_learning_in_transaction(
            self.conn, decision,
        )
        .await
    }
}

pub async fn persist_bulk_update_purchase_items(
    pool: &horse_postgres_common::PgPool,
    input: bookkeeping_postgres::bulk_update_purchase_items::PersistBulkUpdatePurchaseItems,
    targets: Vec<bookkeeping_domain::purchase_commands::BulkUpdateItemTarget>,
    patch: bookkeeping_domain::purchase_commands::BulkUpdateItemPatch,
) -> Result<
    bookkeeping_postgres::bulk_update_purchase_items::PersistedBulkUpdatePurchaseItems,
    WorkflowFailure<
        WorkflowRejection,
        bookkeeping_postgres::bulk_update_purchase_items::BulkUpdatePurchaseItemsPersistenceError,
    >,
> {
    bookkeeping_postgres::bulk_update_purchase_items::persist_bulk_update_purchase_items(
        pool,
        input,
        targets,
        patch,
        &mut UpdatePurchaseItemReferences,
    )
    .await
    .map_err(classify_bulk_update_purchase_items_error)
}

pub async fn persist_bulk_update_purchases(
    pool: &horse_postgres_common::PgPool,
    input: bookkeeping_postgres::bulk_update_purchases::PersistBulkUpdatePurchases,
    commands: Vec<bookkeeping_domain::purchase_commands::UpdatePurchaseCommand>,
) -> Result<
    Vec<String>,
    WorkflowFailure<
        WorkflowRejection,
        bookkeeping_postgres::bulk_update_purchases::BulkUpdatePurchasesPersistenceError,
    >,
> {
    bookkeeping_postgres::bulk_update_purchases::persist_bulk_update_purchases(
        pool,
        input,
        commands,
        &mut UpdatePurchaseItemReferences,
    )
    .await
    .map_err(classify_bulk_update_purchases_error)
}

pub async fn post_purchase_workflow(
    pool: &horse_postgres_common::PgPool,
    command: bookkeeping_application::post_purchase::PostPurchaseWorkflowCommand,
) -> Result<
    bookkeeping_application::post_purchase::PostPurchaseWorkflowResult,
    bookkeeping_application::post_purchase::PostPurchaseWorkflowError<
        PostPurchaseWorkflowAdapterError,
    >,
> {
    bookkeeping_postgres::post_purchase::post_purchase_workflow(
        pool,
        command,
        &mut UpdatePurchaseItemReferences,
    )
    .await
}

pub async fn persist_update_purchase(
    pool: &horse_postgres_common::PgPool,
    input: PersistUpdatePurchase,
    command: bookkeeping_domain::purchase_commands::UpdatePurchaseCommand,
    decision: bookkeeping_domain::purchase_commands::UpdatePurchaseDecision,
) -> Result<
    PersistedUpdatePurchase,
    WorkflowFailure<WorkflowRejection, UpdatePurchasePersistenceError>,
> {
    bookkeeping_postgres::update_purchase::persist_update_purchase(
        pool,
        input,
        command,
        decision,
        &mut UpdatePurchaseItemReferences,
    )
    .await
    .map_err(classify_update_purchase_error)
}

pub async fn persist_bulk_delete_purchase_items(
    pool: &horse_postgres_common::PgPool,
    input: PersistBulkDeletePurchaseItems,
    targets: Vec<bookkeeping_domain::purchase_commands::BulkDeleteItemTarget>,
) -> Result<
    PersistedBulkDeletePurchaseItems,
    WorkflowFailure<WorkflowRejection, BulkDeletePurchaseItemsPersistenceError>,
> {
    bookkeeping_postgres::bulk_delete_purchase_items::persist_bulk_delete_purchase_items(
        pool,
        input,
        targets,
        &mut UpdatePurchaseItemReferences,
    )
    .await
    .map_err(classify_bulk_delete_purchase_items_error)
}

pub async fn persist_save_purchase_item_edits(
    pool: &horse_postgres_common::PgPool,
    input: PersistSavePurchaseItemEdits,
    items: Vec<bookkeeping_domain::purchase_commands::UpdatePurchaseItemCommand>,
    purchase_patch: Option<bookkeeping_domain::purchase_commands::UpdatePurchaseCommand>,
) -> Result<Vec<String>, WorkflowFailure<WorkflowRejection, SavePurchaseItemEditsPersistenceError>>
{
    bookkeeping_postgres::save_purchase_item_edits::persist_save_purchase_item_edits(
        pool,
        input,
        items,
        purchase_patch,
        &mut UpdatePurchaseItemReferences,
    )
    .await
    .map_err(classify_save_purchase_item_edits_error)
}

pub async fn persist_update_purchase_item(
    pool: &horse_postgres_common::PgPool,
    input: PersistUpdatePurchaseItem,
    command: bookkeeping_domain::purchase_commands::UpdatePurchaseItemCommand,
) -> Result<
    PersistedUpdatePurchaseItem,
    WorkflowFailure<WorkflowRejection, UpdatePurchaseItemPersistenceError>,
> {
    bookkeeping_postgres::update_purchase_item::persist_update_purchase_item(
        pool,
        input,
        command,
        &mut UpdatePurchaseItemReferences,
    )
    .await
    .map_err(classify_update_purchase_item_error)
}

fn classify_update_purchase_error(
    error: UpdatePurchasePersistenceError,
) -> WorkflowFailure<WorkflowRejection, UpdatePurchasePersistenceError> {
    use UpdatePurchasePersistenceError as E;
    match error {
        E::InvalidUuid(_)
        | E::InvalidOccurredAt
        | E::NumericOutOfRange(_)
        | E::PaymentTotalMismatch => WorkflowFailure::Decision(WorkflowRejection::InvalidInput),
        E::HouseholdMismatch | E::MemberNotFound | E::PurchaseNotFound | E::AccountNotFound => {
            WorkflowFailure::NotFound
        }
        E::AccountingPeriodClosed | E::InvalidFundingSnapshot => WorkflowFailure::Conflict,
        E::InvalidDecision(error) => match error {
            bookkeeping_domain::purchase_commands::UpdatePurchaseError::Conflict
            | bookkeeping_domain::purchase_commands::UpdatePurchaseError::NotEditable => {
                WorkflowFailure::Conflict
            }
            _ => WorkflowFailure::Decision(WorkflowRejection::InvalidInput),
        },
        error @ (E::Pool(_) | E::Database(_)) => WorkflowFailure::Infrastructure(error),
    }
}

fn classify_update_purchase_item_error(
    error: UpdatePurchaseItemPersistenceError,
) -> WorkflowFailure<WorkflowRejection, UpdatePurchaseItemPersistenceError> {
    use UpdatePurchaseItemPersistenceError as E;
    match error {
        E::InvalidUuid(_)
        | E::NumericOutOfRange(_)
        | E::InvalidPaymentAllocation
        | E::MerchantRequired
        | E::InvalidDecision => WorkflowFailure::Decision(WorkflowRejection::InvalidInput),
        E::PurchaseNotFound
        | E::ItemNotFound
        | E::HouseholdMismatch
        | E::MemberNotFound
        | E::ReferenceNotFound(_) => WorkflowFailure::NotFound,
        E::AccountingPeriodClosed | E::InvalidFundingSnapshot => WorkflowFailure::Conflict,
        E::InvalidWorkflow(error) => match error {
            bookkeeping_domain::purchase_commands::UpdatePurchaseItemError::Conflict => {
                WorkflowFailure::Conflict
            }
            _ => WorkflowFailure::Decision(WorkflowRejection::InvalidInput),
        },
        error @ (E::Pool(_) | E::Database(_)) => WorkflowFailure::Infrastructure(error),
    }
}

fn classify_bulk_update_purchase_items_error(
    error: bookkeeping_postgres::bulk_update_purchase_items::BulkUpdatePurchaseItemsPersistenceError,
) -> WorkflowFailure<
    WorkflowRejection,
    bookkeeping_postgres::bulk_update_purchase_items::BulkUpdatePurchaseItemsPersistenceError,
> {
    use bookkeeping_postgres::bulk_update_purchase_items::BulkUpdatePurchaseItemsPersistenceError as E;
    match error {
        E::InvalidUuid(_) | E::NumericOutOfRange(_) | E::InvalidDecision => {
            WorkflowFailure::Decision(WorkflowRejection::InvalidInput)
        }
        E::ItemNotFound
        | E::PurchaseNotFound
        | E::HouseholdMismatch
        | E::MemberNotFound
        | E::ReferenceNotFound(_) => WorkflowFailure::NotFound,
        E::AccountingPeriodClosed => WorkflowFailure::Conflict,
        E::InvalidWorkflow(error) => match error {
            bookkeeping_domain::purchase_commands::BulkUpdateItemsError::MissingItem => {
                WorkflowFailure::NotFound
            }
            bookkeeping_domain::purchase_commands::BulkUpdateItemsError::Conflict => {
                WorkflowFailure::Conflict
            }
            _ => WorkflowFailure::Decision(WorkflowRejection::InvalidInput),
        },
        error @ (E::Pool(_) | E::Database(_)) => WorkflowFailure::Infrastructure(error),
    }
}

fn classify_bulk_delete_purchase_items_error(
    error: BulkDeletePurchaseItemsPersistenceError,
) -> WorkflowFailure<WorkflowRejection, BulkDeletePurchaseItemsPersistenceError> {
    use BulkDeletePurchaseItemsPersistenceError as E;
    match error {
        E::InvalidUuid(_) | E::NumericOutOfRange(_) | E::InvalidDecision => {
            WorkflowFailure::Decision(WorkflowRejection::InvalidInput)
        }
        E::ItemNotFound | E::PurchaseNotFound | E::HouseholdMismatch | E::MemberNotFound => {
            WorkflowFailure::NotFound
        }
        E::AccountingPeriodClosed | E::InvalidPaymentAllocation | E::InvalidFundingSnapshot => {
            WorkflowFailure::Conflict
        }
        E::InvalidWorkflow(error) => match error {
            bookkeeping_domain::purchase_commands::BulkDeleteItemsError::MissingItem => {
                WorkflowFailure::NotFound
            }
            bookkeeping_domain::purchase_commands::BulkDeleteItemsError::Conflict
            | bookkeeping_domain::purchase_commands::BulkDeleteItemsError::CorrectedItem
            | bookkeeping_domain::purchase_commands::BulkDeleteItemsError::LastItem => {
                WorkflowFailure::Conflict
            }
            _ => WorkflowFailure::Decision(WorkflowRejection::InvalidInput),
        },
        error @ (E::Pool(_) | E::Database(_)) => WorkflowFailure::Infrastructure(error),
    }
}

fn classify_bulk_update_purchases_error(
    error: bookkeeping_postgres::bulk_update_purchases::BulkUpdatePurchasesPersistenceError,
) -> WorkflowFailure<
    WorkflowRejection,
    bookkeeping_postgres::bulk_update_purchases::BulkUpdatePurchasesPersistenceError,
> {
    use bookkeeping_postgres::bulk_update_purchases::BulkUpdatePurchasesPersistenceError as E;
    match error {
        E::InvalidUuid(_) => WorkflowFailure::Decision(WorkflowRejection::InvalidInput),
        E::HouseholdMismatch | E::MemberNotFound | E::PurchaseNotFound => WorkflowFailure::NotFound,
        E::RevisionConflict | E::AccountingPeriodClosed => WorkflowFailure::Conflict,
        E::InvalidWorkflow(error) => match error {
            bookkeeping_domain::purchase_commands::BulkUpdatePurchasesError::InvalidTarget(
                bookkeeping_domain::purchase_commands::UpdatePurchaseError::Conflict
                | bookkeeping_domain::purchase_commands::UpdatePurchaseError::NotEditable,
            ) => WorkflowFailure::Conflict,
            _ => WorkflowFailure::Decision(WorkflowRejection::InvalidInput),
        },
        E::InvalidUpdate(error) => match classify_update_purchase_error(error) {
            WorkflowFailure::Decision(error) => WorkflowFailure::Decision(error),
            WorkflowFailure::NotFound => WorkflowFailure::NotFound,
            WorkflowFailure::Conflict => WorkflowFailure::Conflict,
            WorkflowFailure::Infrastructure(error) => {
                WorkflowFailure::Infrastructure(E::InvalidUpdate(error))
            }
        },
        error @ (E::Pool(_) | E::Database(_)) => WorkflowFailure::Infrastructure(error),
    }
}

fn classify_save_purchase_item_edits_error(
    error: SavePurchaseItemEditsPersistenceError,
) -> WorkflowFailure<WorkflowRejection, SavePurchaseItemEditsPersistenceError> {
    use SavePurchaseItemEditsPersistenceError as E;
    match error {
        E::InvalidUuid(_) | E::InvalidWorkflow(_) => {
            WorkflowFailure::Decision(WorkflowRejection::InvalidInput)
        }
        E::PurchaseNotFound | E::ItemNotFound | E::HouseholdMismatch | E::MemberNotFound => {
            WorkflowFailure::NotFound
        }
        E::InvalidPurchaseUpdate(error) => match classify_update_purchase_error(error) {
            WorkflowFailure::Decision(error) => WorkflowFailure::Decision(error),
            WorkflowFailure::NotFound => WorkflowFailure::NotFound,
            WorkflowFailure::Conflict => WorkflowFailure::Conflict,
            WorkflowFailure::Infrastructure(error) => {
                WorkflowFailure::Infrastructure(E::InvalidPurchaseUpdate(error))
            }
        },
        E::InvalidItemUpdate(error) => match classify_update_purchase_item_error(error) {
            WorkflowFailure::Decision(error) => WorkflowFailure::Decision(error),
            WorkflowFailure::NotFound => WorkflowFailure::NotFound,
            WorkflowFailure::Conflict => WorkflowFailure::Conflict,
            WorkflowFailure::Infrastructure(error) => {
                WorkflowFailure::Infrastructure(E::InvalidItemUpdate(error))
            }
        },
        error @ (E::Pool(_) | E::Database(_)) => WorkflowFailure::Infrastructure(error),
    }
}

pub async fn create_shared_purchase_draft(
    pool: &horse_postgres_common::PgPool,
    actor_id: &str,
    household_id: &str,
    payload: &str,
) -> Result<SharedPurchaseDraft, SharedPurchaseDraftPersistenceError> {
    bookkeeping_postgres::shared_draft::create_shared_purchase_draft(
        pool,
        actor_id,
        household_id,
        payload,
        &mut SharedDraftReferences,
    )
    .await
}

pub async fn update_shared_purchase_draft(
    pool: &horse_postgres_common::PgPool,
    actor_id: &str,
    draft_id: &str,
    expected_revision: i64,
    payload: &str,
) -> Result<UpdateSharedPurchaseDraftResult, SharedPurchaseDraftPersistenceError> {
    bookkeeping_postgres::shared_draft::update_shared_purchase_draft(
        pool,
        actor_id,
        draft_id,
        expected_revision,
        payload,
        &mut SharedDraftReferences,
    )
    .await
}

pub async fn get_shared_purchase_draft(
    pool: &horse_postgres_common::PgPool,
    actor_id: &str,
    draft_id: &str,
) -> Result<SharedPurchaseDraft, SharedPurchaseDraftPersistenceError> {
    bookkeeping_postgres::shared_draft::get_shared_purchase_draft(pool, actor_id, draft_id).await
}

#[allow(async_fn_in_trait)]
impl UpdatePurchaseItemReferenceData for UpdatePurchaseItemReferences {
    async fn load_default_allocation(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_id: Uuid,
        version: i64,
    ) -> Result<Vec<(Uuid, i64)>, DieselError> {
        public::allocation_defaults::table
            .filter(public::allocation_defaults::household_id.eq(household_id))
            .filter(public::allocation_defaults::version.eq(version))
            .order(public::allocation_defaults::pool_id)
            .select((
                public::allocation_defaults::pool_id,
                public::allocation_defaults::weight,
            ))
            .load::<(Uuid, i64)>(conn)
            .await
    }

    async fn load_automatic_allocation(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_id: Uuid,
        category_id: Option<Uuid>,
        variant_id: Option<Uuid>,
        merchant: &str,
    ) -> Result<Option<(Uuid, i64, Vec<(Uuid, i64)>)>, DieselError> {
        use public::allocation_rules::dsl as rules;

        let concept_id = if let Some(variant_id) = variant_id {
            public::product_variants::table
                .filter(public::product_variants::household_id.eq(household_id))
                .filter(public::product_variants::id.eq(variant_id))
                .select(public::product_variants::concept_id)
                .first::<Uuid>(conn)
                .await
                .optional()?
        } else {
            None
        };
        let mut rule = if let Some(id) = variant_id {
            rules::allocation_rules
                .filter(rules::household_id.eq(household_id))
                .filter(rules::product_variant_id.eq(Some(id)))
                .select((rules::id, rules::revision))
                .first::<(Uuid, i64)>(conn)
                .await
                .optional()?
        } else {
            None
        };
        if rule.is_none()
            && let Some(id) = concept_id
        {
            rule = rules::allocation_rules
                .filter(rules::household_id.eq(household_id))
                .filter(rules::product_concept_id.eq(Some(id)))
                .select((rules::id, rules::revision))
                .first::<(Uuid, i64)>(conn)
                .await
                .optional()?;
        }
        if rule.is_none()
            && let Some(id) = category_id
        {
            rule = rules::allocation_rules
                .filter(rules::household_id.eq(household_id))
                .filter(rules::category_id.eq(Some(id)))
                .select((rules::id, rules::revision))
                .first::<(Uuid, i64)>(conn)
                .await
                .optional()?;
        }
        if rule.is_none() && !merchant.is_empty() {
            rule = rules::allocation_rules
                .filter(rules::household_id.eq(household_id))
                .filter(rules::merchant_key.eq(Some(merchant.to_owned())))
                .select((rules::id, rules::revision))
                .first::<(Uuid, i64)>(conn)
                .await
                .optional()?;
        }
        let Some((rule_id, revision)) = rule else {
            return Ok(None);
        };
        let weights = public::allocation_rule_weights::table
            .filter(public::allocation_rule_weights::household_id.eq(household_id))
            .filter(public::allocation_rule_weights::rule_id.eq(rule_id))
            .order(public::allocation_rule_weights::pool_id)
            .select((
                public::allocation_rule_weights::pool_id,
                public::allocation_rule_weights::weight,
            ))
            .load::<(Uuid, i64)>(conn)
            .await?;
        Ok(Some((rule_id, revision, weights)))
    }

    async fn active_category(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_id: Uuid,
        category_id: Uuid,
    ) -> Result<bool, DieselError> {
        Ok(public::categories::table
            .filter(public::categories::household_id.eq(household_id))
            .filter(public::categories::id.eq(category_id))
            .filter(public::categories::archived.eq(false))
            .select(public::categories::id)
            .first::<Uuid>(conn)
            .await
            .optional()?
            .is_some())
    }

    async fn active_variant(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_id: Uuid,
        variant_id: Uuid,
    ) -> Result<bool, DieselError> {
        Ok(public::product_variants::table
            .filter(public::product_variants::household_id.eq(household_id))
            .filter(public::product_variants::id.eq(variant_id))
            .filter(public::product_variants::archived.eq(false))
            .select(public::product_variants::id)
            .first::<Uuid>(conn)
            .await
            .optional()?
            .is_some())
    }

    async fn active_tags(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_id: Uuid,
        tag_ids: Vec<Uuid>,
    ) -> Result<Vec<Uuid>, DieselError> {
        public::tags::table
            .filter(public::tags::household_id.eq(household_id))
            .filter(public::tags::id.eq_any(tag_ids))
            .filter(public::tags::archived.eq(false))
            .select(public::tags::id)
            .load::<Uuid>(conn)
            .await
    }

    async fn persist_product_learning(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        context: ProductLearningRequest,
    ) -> Result<(), UpdatePurchaseItemPersistenceError> {
        use catalog_application::product_learning::{ProductLearningCommand, ProductLearningKind};
        let learning = context.learning.map(|learning| match learning {
            bookkeeping_domain::purchase_commands::ProductLearning::VariantName(name) => {
                ProductLearningKind::VariantName(name)
            }
            bookkeeping_domain::purchase_commands::ProductLearning::MerchantAlias => {
                ProductLearningKind::MerchantAlias
            }
            bookkeeping_domain::purchase_commands::ProductLearning::ItemOnly => {
                ProductLearningKind::ItemOnly
            }
        });
        let command = ProductLearningCommand {
            household_id: context.household_id.into(),
            actor_id: context.actor_id.into(),
            purchase_id: context.purchase_id.into(),
            item_id: context.item_id.into(),
            raw_name: context.raw_name,
            previous_variant_id: context.previous_variant_id.map(Into::into),
            variant_id: context.variant_id.map(Into::into),
            variant_changed: context.variant_changed,
            merchant: context.merchant,
            learning,
        };
        let mut operations = ProductLearningPostgresOperations { conn };
        catalog_application::product_learning::learn_product(&mut operations, command)
            .await
            .map_err(|error| match error {
                catalog_application::product_learning::ProductLearningWorkflowError::Decision(
                    catalog_application::product_learning::ProductLearningError::MerchantRequired,
                ) => UpdatePurchaseItemPersistenceError::MerchantRequired,
                catalog_application::product_learning::ProductLearningWorkflowError::Decision(
                    catalog_application::product_learning::ProductLearningError::InvalidVariantName
                    | catalog_application::product_learning::ProductLearningError::InvalidMerchant
                    | catalog_application::product_learning::ProductLearningError::InvalidAlias,
                ) => UpdatePurchaseItemPersistenceError::InvalidDecision,
                catalog_application::product_learning::ProductLearningWorkflowError::Operations(
                    error,
                ) => error.into(),
            })
    }
}

impl PostPurchaseReferenceData for UpdatePurchaseItemReferences {
    async fn load_funding_rule(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_id: Uuid,
        pool_id: Uuid,
        version: i32,
    ) -> Result<Option<(Option<Uuid>, Option<String>, Vec<(Uuid, i64)>)>, DieselError> {
        let rule = public::pool_funding_rules::table
            .filter(public::pool_funding_rules::household_id.eq(household_id))
            .filter(public::pool_funding_rules::pool_id.eq(pool_id))
            .filter(public::pool_funding_rules::version.eq(version))
            .for_update()
            .select((
                public::pool_funding_rules::owner_member_id,
                public::pool_funding_rules::settlement_policy,
            ))
            .first::<(Option<Uuid>, Option<String>)>(conn)
            .await
            .optional()?;
        let Some((owner, policy)) = rule else {
            return Ok(None);
        };
        let shares = public::pool_funding_shares::table
            .filter(public::pool_funding_shares::household_id.eq(household_id))
            .filter(public::pool_funding_shares::pool_id.eq(pool_id))
            .filter(public::pool_funding_shares::version.eq(version))
            .for_update()
            .select((
                public::pool_funding_shares::member_id,
                public::pool_funding_shares::weight,
            ))
            .load::<(Uuid, i64)>(conn)
            .await?;
        Ok(Some((owner, policy, shares)))
    }
}

pub struct SharedDraftReferences;

#[allow(async_fn_in_trait)]
impl SharedDraftReferenceData for SharedDraftReferences {
    async fn find_references(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_id: Uuid,
        kind: bookkeeping_domain::purchase_commands::DraftReferenceKind,
        ids: &[Uuid],
    ) -> Result<Vec<Uuid>, DieselError> {
        use bookkeeping_domain::purchase_commands::DraftReferenceKind;
        match kind {
            DraftReferenceKind::Account => {
                public::accounts::table
                    .filter(public::accounts::household_id.eq(household_id))
                    .filter(public::accounts::id.eq_any(ids))
                    .select(public::accounts::id)
                    .load::<Uuid>(conn)
                    .await
            }
            DraftReferenceKind::Category => {
                public::categories::table
                    .filter(public::categories::household_id.eq(household_id))
                    .filter(public::categories::id.eq_any(ids))
                    .select(public::categories::id)
                    .load::<Uuid>(conn)
                    .await
            }
            DraftReferenceKind::Pool => {
                public::pools::table
                    .filter(public::pools::household_id.eq(household_id))
                    .filter(public::pools::id.eq_any(ids))
                    .select(public::pools::id)
                    .load::<Uuid>(conn)
                    .await
            }
            DraftReferenceKind::ProductVariant => {
                public::product_variants::table
                    .filter(public::product_variants::household_id.eq(household_id))
                    .filter(public::product_variants::id.eq_any(ids))
                    .select(public::product_variants::id)
                    .load::<Uuid>(conn)
                    .await
            }
        }
    }
}
