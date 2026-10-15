use bookkeeping_application::create_purchase::CreatePurchaseDecision;
use bookkeeping_application::create_purchase_workflow as application;
use bookkeeping_application::create_and_post_purchase::{
    CreatePurchasePersistence, CreatedPurchase, PurchaseAllocationContext,
    PurchaseItemAllocationRequest,
};
use chrono::{Datelike, FixedOffset, NaiveDate};
use diesel::{
    ExpressionMethods, JoinOnDsl, OptionalExtension, QueryDsl, dsl::now,
    result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use uuid::Uuid;

use crate::{PgPool, schema::public};

const OPERATION: &str = "createPurchase";

#[derive(Debug, Clone)]
pub struct PersistCreatePurchase {
    pub household_id: String,
    pub actor_id: String,
    pub idempotency_key: String,
    pub request_fingerprint: [u8; 32],
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PersistedCreatePurchase {
    pub purchase_id: String,
    pub created: bool,
}

#[derive(Debug)]
pub enum CreatePurchasePersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    InvalidOccurredAt,
    InvalidIdempotencyKey,
    IdempotencyConflict,
    NumericOutOfRange(&'static str),
    HouseholdNotFound,
    MemberNotFound,
    AccountingPeriodClosed,
    ReferenceNotFound(&'static str),
    AllocationRuleMismatch,
    MissingDefaultAllocation,
    SourceAlreadyConfirmedAsCorrection,
    ReceiptConfirmationInvalid,
    InvalidDecision,
}

impl From<DieselError> for CreatePurchasePersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

#[derive(Debug)]
pub enum CreatePurchaseWorkflowAdapterError {
    Database(DieselError),
    Context(crate::purchase_command_context::PurchaseCommandContextError),
    Persistence(CreatePurchasePersistenceError),
}

pub async fn create_purchase_workflow(
    pool: &PgPool,
    command: application::CreatePurchaseWorkflowCommand,
) -> Result<CreatedPurchase, application::CreatePurchaseWorkflowError<CreatePurchaseWorkflowAdapterError>> {
    let mut pooled = pool.get().await.map_err(|error| {
        application::CreatePurchaseWorkflowError::Operations(
            CreatePurchaseWorkflowAdapterError::Persistence(
                CreatePurchasePersistenceError::Pool(error.to_string()),
            ),
        )
    })?;
    let result = (&mut *pooled)
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
        })?;
    result
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

        let household_id = parse_uuid(household_id, "household_id").map_err(|error| {
            CreatePurchaseWorkflowAdapterError::Persistence(error)
        })?;
        let actor_id = parse_uuid(actor_id, "actor_id")
            .map_err(CreatePurchaseWorkflowAdapterError::Persistence)?;
        members::household_members
            .filter(members::household_id.eq(household_id))
            .filter(members::member_id.eq(actor_id))
            .filter(members::left_at.is_null())
            .select(members::member_id)
            .first::<Uuid>(&mut *self.conn)
            .await
            .optional()
            .map_err(CreatePurchaseWorkflowAdapterError::Database)?
            .ok_or(CreatePurchaseWorkflowAdapterError::Persistence(
                CreatePurchasePersistenceError::MemberNotFound,
            ))?;
        Ok(())
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
                Ok(crate::purchase_command_context::PurchaseItemAllocationInput {
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
                    allocations: item
                        .allocations
                        .iter()
                        .map(|allocation| {
                            Ok((
                                parse_uuid(&allocation.pool_id, "pool_id").map_err(
                                    CreatePurchaseWorkflowAdapterError::Persistence,
                                )?,
                                allocation.weight,
                            ))
                        })
                        .collect::<Result<Vec<_>, Self::Error>>()?,
                })
            })
            .collect::<Result<Vec<_>, Self::Error>>()?;
        crate::purchase_command_context::load_purchase_command_context_in_transaction(
            &mut *self.conn,
            household_id,
            merchant_key,
            &item_inputs,
        )
        .await
        .map_err(CreatePurchaseWorkflowAdapterError::Context)
    }

    async fn persist_create(
        &mut self,
        persistence: CreatePurchasePersistence,
        decision: bookkeeping_application::create_purchase::CreatePurchaseDecision,
    ) -> Result<CreatedPurchase, Self::Error> {
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
        .map_err(CreatePurchaseWorkflowAdapterError::Persistence)?;
        Ok(CreatedPurchase {
            purchase_id: persisted.purchase_id,
            created: persisted.created,
        })
    }
}

pub async fn persist_create_purchase(
    pool: &PgPool,
    input: PersistCreatePurchase,
    decision: CreatePurchaseDecision,
) -> Result<PersistedCreatePurchase, CreatePurchasePersistenceError> {
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| CreatePurchasePersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<PersistedCreatePurchase, CreatePurchasePersistenceError, _>(async |conn| {
            persist_create_purchase_in_transaction(conn, input, decision).await
        })
        .await
}

pub async fn persist_create_purchase_in_transaction(
    conn: &mut diesel_async::AsyncPgConnection,
    input: PersistCreatePurchase,
    mut decision: CreatePurchaseDecision,
) -> Result<PersistedCreatePurchase, CreatePurchasePersistenceError> {
    if input.idempotency_key.is_empty() || input.idempotency_key.len() > 200 {
        return Err(CreatePurchasePersistenceError::InvalidIdempotencyKey);
    }
    let household_id = parse_uuid(&input.household_id, "household_id")?;
    let actor_id = parse_uuid(&input.actor_id, "actor_id")?;
    let purchase_id = parse_uuid(&decision.purchase_id, "purchase_id")?;
    let occurred_at = decision.occurred_at.value();
    let tokyo_offset = FixedOffset::east_opt(9 * 60 * 60)
        .ok_or(CreatePurchasePersistenceError::InvalidDecision)?;
    let period_start = NaiveDate::from_ymd_opt(
        occurred_at.with_timezone(&tokyo_offset).year(),
        occurred_at.with_timezone(&tokyo_offset).month(),
        1,
    )
    .ok_or(CreatePurchasePersistenceError::InvalidOccurredAt)?;
    let allocation_rule_version =
        i64::try_from(decision.allocation_rule_version.value()).map_err(|_| {
            CreatePurchasePersistenceError::NumericOutOfRange("allocation_rule_version")
        })?;
    if decision.items.is_empty()
        || decision
            .items
            .iter()
            .any(|item| item.allocations.is_empty())
    {
        return Err(CreatePurchasePersistenceError::InvalidDecision);
    }

    use public::accounting_periods::dsl as periods;
    use public::household_members::dsl as members;
    use public::households::dsl as households;
    use public::idempotency_records::dsl as idem;

    let current_default_version = households::households
        .filter(households::id.eq(household_id))
        .for_share()
        .select(households::default_allocation_version)
        .first::<i64>(conn)
        .await
        .optional()?
        .ok_or(CreatePurchasePersistenceError::HouseholdNotFound)?;
    // 冪等性判定は業務判断(負担ルールバージョンチェック等)より先に行う。初回成功後に負担ルールが変わっても再送は過去の成功結果を返す。
    let member = members::household_members
        .filter(members::household_id.eq(household_id))
        .filter(members::member_id.eq(actor_id))
        .filter(members::left_at.is_null())
        .select(members::member_id)
        .first::<Uuid>(conn)
        .await
        .optional()?
        .is_some();
    if !member {
        return Err(CreatePurchasePersistenceError::MemberNotFound);
    }

    let existing = idem::idempotency_records
        .filter(idem::household_id.eq(household_id))
        .filter(idem::operation.eq(OPERATION))
        .filter(idem::key.eq(&input.idempotency_key))
        .select((idem::request_fingerprint, idem::resource_id))
        .first::<(Vec<u8>, Uuid)>(conn)
        .await
        .optional()?;
    if let Some((fingerprint, existing_id)) = existing {
        if fingerprint.as_slice() != input.request_fingerprint {
            return Err(CreatePurchasePersistenceError::IdempotencyConflict);
        }
        return Ok(PersistedCreatePurchase {
            purchase_id: existing_id.to_string(),
            created: false,
        });
    }
    if current_default_version != allocation_rule_version {
        return Err(CreatePurchasePersistenceError::AllocationRuleMismatch);
    }

    diesel::insert_into(periods::accounting_periods)
        .values((
            periods::household_id.eq(household_id),
            periods::period_start.eq(period_start),
            periods::status.eq("OPEN"),
            periods::revision.eq(1_i64),
        ))
        .on_conflict((periods::household_id, periods::period_start))
        .do_nothing()
        .execute(&mut *conn)
        .await?;
    let status = periods::accounting_periods
        .filter(periods::household_id.eq(household_id))
        .filter(periods::period_start.eq(period_start))
        .for_update()
        .select(periods::status)
        .first::<String>(&mut *conn)
        .await?;
    if status != "OPEN" {
        return Err(CreatePurchasePersistenceError::AccountingPeriodClosed);
    }

    let confirmed_purchase = resolve_source_confirmation(
        &mut *conn,
        household_id,
        decision.source_document_id.as_deref(),
        decision.source_reference.as_deref().unwrap_or(""),
    )
    .await?;
    let effective_purchase_id = confirmed_purchase.unwrap_or(purchase_id);
    if confirmed_purchase.is_none() {
        validate_catalog_references(&mut *conn, household_id, &mut decision).await?;
    }

    let inserted = diesel::insert_into(idem::idempotency_records)
        .values((
            idem::household_id.eq(household_id),
            idem::operation.eq(OPERATION),
            idem::key.eq(&input.idempotency_key),
            idem::request_fingerprint.eq(input.request_fingerprint.to_vec()),
            idem::resource_kind.eq("purchase"),
            idem::resource_id.eq(effective_purchase_id),
        ))
        .on_conflict((idem::household_id, idem::operation, idem::key))
        .do_nothing()
        .returning(idem::resource_id)
        .get_result::<Uuid>(conn)
        .await
        .optional()?;
    if inserted.is_none() {
        let (fingerprint, existing_id) = idem::idempotency_records
            .filter(idem::household_id.eq(household_id))
            .filter(idem::operation.eq(OPERATION))
            .filter(idem::key.eq(&input.idempotency_key))
            .select((idem::request_fingerprint, idem::resource_id))
            .first::<(Vec<u8>, Uuid)>(conn)
            .await?;
        if fingerprint.as_slice() != input.request_fingerprint {
            return Err(CreatePurchasePersistenceError::IdempotencyConflict);
        }
        return Ok(PersistedCreatePurchase {
            purchase_id: existing_id.to_string(),
            created: false,
        });
    }

    if confirmed_purchase.is_some() {
        return Ok(PersistedCreatePurchase {
            purchase_id: effective_purchase_id.to_string(),
            created: false,
        });
    }

    diesel::insert_into(public::purchases::table)
        .values((
            public::purchases::id.eq(purchase_id),
            public::purchases::household_id.eq(household_id),
            public::purchases::occurred_at.eq(occurred_at),
            public::purchases::merchant.eq(&decision.merchant),
            public::purchases::status.eq("DRAFT"),
            public::purchases::created_by.eq(actor_id),
            public::purchases::allocation_rule_version.eq(allocation_rule_version),
            public::purchases::date_precision.eq(decision.date_precision.as_db_value()),
            public::purchases::origin.eq(decision.origin.as_db_value()),
        ))
        .execute(&mut *conn)
        .await?;

    for item in &decision.items {
        let item_id = parse_uuid(&item.item_id, "item_id")?;
        let category_id = item
            .category_id
            .as_deref()
            .map(|value| parse_uuid(value, "category_id"))
            .transpose()?;
        let product_variant_id = item
            .product_variant_id
            .as_deref()
            .map(|value| parse_uuid(value, "product_variant_id"))
            .transpose()?;
        let (quantity_numerator, quantity_denominator, quantity_unit) = item
            .quantity
            .as_ref()
            .map(|measure| {
                (
                    Some(measure.numerator()),
                    Some(measure.denominator()),
                    Some(measure.unit().as_db_value()),
                )
            })
            .unwrap_or((None, None, None));
        let (printed_unit_price_yen, printed_num, printed_den, printed_unit) = item
            .printed_unit_price
            .as_ref()
            .map(|price| {
                (
                    Some(price.amount_yen),
                    Some(price.basis.numerator()),
                    Some(price.basis.denominator()),
                    Some(price.basis.unit().as_db_value()),
                )
            })
            .unwrap_or((None, None, None, None));
        diesel::insert_into(public::purchase_items::table)
            .values((
                public::purchase_items::id.eq(item_id),
                public::purchase_items::household_id.eq(household_id),
                public::purchase_items::purchase_id.eq(purchase_id),
                public::purchase_items::name.eq(&item.name),
                public::purchase_items::amount_yen.eq(item.amount_yen),
                public::purchase_items::category_id.eq(category_id),
                public::purchase_items::product_variant_id.eq(product_variant_id),
                public::purchase_items::allocation_override.eq(item.allocation_override),
                public::purchase_items::quantity_numerator.eq(quantity_numerator),
                public::purchase_items::quantity_denominator.eq(quantity_denominator),
                public::purchase_items::quantity_unit.eq(quantity_unit),
                public::purchase_items::printed_unit_price_yen.eq(printed_unit_price_yen),
                public::purchase_items::printed_unit_price_quantity_numerator.eq(printed_num),
                public::purchase_items::printed_unit_price_quantity_denominator.eq(printed_den),
                public::purchase_items::printed_unit_price_unit.eq(printed_unit),
            ))
            .execute(&mut *conn)
            .await?;
        for allocation in &item.allocations {
            let pool_id = parse_uuid(&allocation.pool_id, "pool_id")?;
            let rule_id = allocation
                .rule_id
                .as_deref()
                .map(|value| parse_uuid(value, "rule_id"))
                .transpose()?;
            let rule_version = i64::try_from(allocation.rule_version)
                .map_err(|_| CreatePurchasePersistenceError::NumericOutOfRange("rule_version"))?;
            diesel::insert_into(public::purchase_allocations::table)
                .values((
                    public::purchase_allocations::household_id.eq(household_id),
                    public::purchase_allocations::purchase_id.eq(purchase_id),
                    public::purchase_allocations::item_id.eq(item_id),
                    public::purchase_allocations::pool_id.eq(pool_id),
                    public::purchase_allocations::amount_yen.eq(allocation.amount_yen),
                    public::purchase_allocations::weight.eq(allocation.weight),
                    public::purchase_allocations::rule_version.eq(rule_version),
                    public::purchase_allocations::rule_id.eq(rule_id),
                ))
                .execute(&mut *conn)
                .await?;
        }
    }

    for (index, payment) in decision.payments.iter().enumerate() {
        let line_no = i32::try_from(index + 1)
            .map_err(|_| CreatePurchasePersistenceError::NumericOutOfRange("payment line_no"))?;
        let account_id = parse_uuid(&payment.account_id, "account_id")?;
        diesel::insert_into(public::purchase_payments::table)
            .values((
                public::purchase_payments::household_id.eq(household_id),
                public::purchase_payments::purchase_id.eq(purchase_id),
                public::purchase_payments::line_no.eq(line_no),
                public::purchase_payments::account_id.eq(account_id),
                public::purchase_payments::amount_yen.eq(payment.amount_yen),
            ))
            .execute(&mut *conn)
            .await?;
    }

    if let Some(source_document_id) = decision.source_document_id.as_deref() {
        let source_document_id = parse_uuid(source_document_id, "source_document_id")?;
        let source_reference = decision.source_reference.as_deref().unwrap_or("");
        diesel::insert_into(public::source_confirmations::table)
            .values((
                public::source_confirmations::id.eq(Uuid::new_v4()),
                public::source_confirmations::household_id.eq(household_id),
                public::source_confirmations::source_document_id.eq(source_document_id),
                public::source_confirmations::observation_id.eq(None::<Uuid>),
                public::source_confirmations::source_reference.eq(source_reference),
                public::source_confirmations::entity_kind.eq("purchase"),
                public::source_confirmations::entity_id.eq(purchase_id),
                public::source_confirmations::status.eq("confirmed"),
                public::source_confirmations::idempotency_key.eq(&input.idempotency_key),
                public::source_confirmations::changed_by.eq(actor_id),
                public::source_confirmations::changed_at.eq(now),
            ))
            .execute(&mut *conn)
            .await?;
        persist_source_evidence(
            &mut *conn,
            household_id,
            source_document_id,
            source_reference,
            purchase_id,
            actor_id,
            &decision,
        )
        .await?;
    }

    diesel::insert_into(public::household_events::table)
        .values((
            public::household_events::household_id.eq(household_id),
            public::household_events::event_type.eq("purchase.created"),
            public::household_events::entity_kind.eq("purchase"),
            public::household_events::entity_id.eq(purchase_id),
            public::household_events::revision.eq(1_i64),
            public::household_events::changed_fields.eq(Vec::<Option<String>>::new()),
            public::household_events::actor_id.eq(actor_id),
            public::household_events::created_at.eq(now),
        ))
        .execute(&mut *conn)
        .await?;
    Ok(PersistedCreatePurchase {
        purchase_id: purchase_id.to_string(),
        created: true,
    })
}

async fn validate_catalog_references(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    decision: &mut CreatePurchaseDecision,
) -> Result<(), CreatePurchasePersistenceError> {
    use public::accounts::dsl as accounts;
    use public::allocation_defaults::dsl as defaults;
    use public::allocation_rule_weights::dsl as rule_weights;
    use public::allocation_rules::dsl as rules;
    use public::categories::dsl as categories;
    use public::households::dsl as households;
    use public::pools::dsl as pools;
    use public::product_concepts::dsl as concepts;
    use public::product_variants::dsl as variants;

    let default_version =
        i64::try_from(decision.allocation_rule_version.value()).map_err(|_| {
            CreatePurchasePersistenceError::NumericOutOfRange("allocation_rule_version")
        })?;
    let current_default_version = households::households
        .filter(households::id.eq(household_id))
        .select(households::default_allocation_version)
        .first::<i64>(conn)
        .await
        .optional()?
        .ok_or(CreatePurchasePersistenceError::HouseholdNotFound)?;
    if current_default_version != default_version {
        return Err(CreatePurchasePersistenceError::AllocationRuleMismatch);
    }
    let configured_defaults = defaults::allocation_defaults
        .filter(defaults::household_id.eq(household_id))
        .filter(defaults::version.eq(default_version))
        .select(defaults::pool_id)
        .load::<Uuid>(conn)
        .await?;
    if configured_defaults.is_empty() {
        return Err(CreatePurchasePersistenceError::MissingDefaultAllocation);
    }

    for payment in &decision.payments {
        let account_id = parse_uuid(&payment.account_id, "account_id")?;
        let found = accounts::accounts
            .filter(accounts::household_id.eq(household_id))
            .filter(accounts::id.eq(account_id))
            .select(accounts::id)
            .first::<Uuid>(conn)
            .await
            .optional()?
            .is_some();
        if !found {
            return Err(CreatePurchasePersistenceError::ReferenceNotFound("account"));
        }
    }

    for item in &mut decision.items {
        if let Some(category_id) = item.category_id.as_deref() {
            let category_id = parse_uuid(category_id, "category_id")?;
            let found = categories::categories
                .filter(categories::household_id.eq(household_id))
                .filter(categories::id.eq(category_id))
                .select(categories::id)
                .first::<Uuid>(conn)
                .await
                .optional()?
                .is_some();
            if !found {
                return Err(CreatePurchasePersistenceError::ReferenceNotFound(
                    "category",
                ));
            }
        }
        let mut concept_id = None;
        if let Some(variant_id) = item.product_variant_id.as_deref() {
            let variant_id = parse_uuid(variant_id, "product_variant_id")?;
            let (found_concept_id, concept_category_id) = variants::product_variants
                .filter(variants::household_id.eq(household_id))
                .filter(variants::id.eq(variant_id))
                .filter(variants::archived.eq(false))
                .inner_join(concepts::product_concepts.on(concepts::id.eq(variants::concept_id)))
                .filter(concepts::household_id.eq(household_id))
                .filter(concepts::archived.eq(false))
                .select((variants::concept_id, concepts::category_id))
                .first::<(Uuid, Option<Uuid>)>(conn)
                .await
                .optional()?
                .ok_or(CreatePurchasePersistenceError::ReferenceNotFound(
                    "product_variant",
                ))?;
            concept_id = Some(found_concept_id);
            if item.category_id.is_none() {
                item.category_id = concept_category_id.map(|id| id.to_string());
            }
        }

        let first = item
            .allocations
            .get(0)
            .ok_or(CreatePurchasePersistenceError::InvalidDecision)?;
        if item.allocations.iter().any(|allocation| {
            allocation.rule_version != first.rule_version || allocation.rule_id != first.rule_id
        }) {
            return Err(CreatePurchasePersistenceError::AllocationRuleMismatch);
        }
        for allocation in &item.allocations {
            let pool_id = parse_uuid(&allocation.pool_id, "pool_id")?;
            let found = pools::pools
                .filter(pools::household_id.eq(household_id))
                .filter(pools::id.eq(pool_id))
                .select(pools::id)
                .first::<Uuid>(conn)
                .await
                .optional()?
                .is_some();
            if !found {
                return Err(CreatePurchasePersistenceError::ReferenceNotFound("pool"));
            }
        }
        if item.allocation_override {
            if first.rule_id.is_some()
                || first.rule_version != decision.allocation_rule_version.value()
            {
                return Err(CreatePurchasePersistenceError::AllocationRuleMismatch);
            }
            continue;
        }

        let selected_rule = applicable_allocation_rule(
            conn,
            household_id,
            item.category_id.as_deref(),
            concept_id,
            item.product_variant_id.as_deref(),
            &decision.merchant,
        )
        .await?;
        if selected_rule.map(|id| id.to_string()) != first.rule_id {
            return Err(CreatePurchasePersistenceError::AllocationRuleMismatch);
        }
        if first.rule_id.is_none() && first.rule_version != decision.allocation_rule_version.value()
        {
            return Err(CreatePurchasePersistenceError::AllocationRuleMismatch);
        }

        let expected = if let Some(rule_id) = first.rule_id.as_deref() {
            let rule_id = parse_uuid(rule_id, "allocation_rule_id")?;
            let revision = rules::allocation_rules
                .filter(rules::household_id.eq(household_id))
                .filter(rules::id.eq(rule_id))
                .select(rules::revision)
                .first::<i64>(conn)
                .await
                .optional()?
                .ok_or(CreatePurchasePersistenceError::ReferenceNotFound(
                    "allocation_rule",
                ))?;
            if u64::try_from(revision).ok() != Some(first.rule_version) {
                return Err(CreatePurchasePersistenceError::AllocationRuleMismatch);
            }
            rule_weights::allocation_rule_weights
                .filter(rule_weights::household_id.eq(household_id))
                .filter(rule_weights::rule_id.eq(rule_id))
                .select((rule_weights::pool_id, rule_weights::weight))
                .load::<(Uuid, i64)>(conn)
                .await?
        } else {
            let version = i64::try_from(first.rule_version)
                .map_err(|_| CreatePurchasePersistenceError::NumericOutOfRange("rule_version"))?;
            defaults::allocation_defaults
                .filter(defaults::household_id.eq(household_id))
                .filter(defaults::version.eq(version))
                .select((defaults::pool_id, defaults::weight))
                .load::<(Uuid, i64)>(conn)
                .await?
        };
        let mut expected = expected;
        let mut actual = item
            .allocations
            .iter()
            .map(|allocation| {
                Ok((
                    parse_uuid(&allocation.pool_id, "pool_id")?,
                    allocation.weight,
                ))
            })
            .collect::<Result<Vec<_>, CreatePurchasePersistenceError>>()?;
        expected.sort_unstable();
        actual.sort_unstable();
        if expected != actual {
            return Err(CreatePurchasePersistenceError::AllocationRuleMismatch);
        }
    }
    Ok(())
}

async fn applicable_allocation_rule(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    category_id: Option<&str>,
    concept_id: Option<Uuid>,
    variant_id: Option<&str>,
    merchant: &str,
) -> Result<Option<Uuid>, CreatePurchasePersistenceError> {
    use public::allocation_rules::dsl as rules;

    if let Some(variant_id) = variant_id {
        let variant_id = parse_uuid(variant_id, "product_variant_id")?;
        if let Some(id) = rules::allocation_rules
            .filter(rules::household_id.eq(household_id))
            .filter(rules::product_variant_id.eq(Some(variant_id)))
            .select(rules::id)
            .first::<Uuid>(conn)
            .await
            .optional()?
        {
            return Ok(Some(id));
        }
    }
    if let Some(concept_id) = concept_id {
        if let Some(id) = rules::allocation_rules
            .filter(rules::household_id.eq(household_id))
            .filter(rules::product_concept_id.eq(Some(concept_id)))
            .select(rules::id)
            .first::<Uuid>(conn)
            .await
            .optional()?
        {
            return Ok(Some(id));
        }
    }
    if let Some(category_id) = category_id {
        let category_id = parse_uuid(category_id, "category_id")?;
        if let Some(id) = rules::allocation_rules
            .filter(rules::household_id.eq(household_id))
            .filter(rules::category_id.eq(Some(category_id)))
            .select(rules::id)
            .first::<Uuid>(conn)
            .await
            .optional()?
        {
            return Ok(Some(id));
        }
    }
    let merchant_key = merchant
        .split_whitespace()
        .collect::<Vec<_>>()
        .join(" ")
        .to_lowercase();
    if !merchant_key.is_empty() {
        if let Some(id) = rules::allocation_rules
            .filter(rules::household_id.eq(household_id))
            .filter(rules::merchant_key.eq(Some(merchant_key)))
            .select(rules::id)
            .first::<Uuid>(conn)
            .await
            .optional()?
        {
            return Ok(Some(id));
        }
    }
    Ok(None)
}

async fn resolve_source_confirmation(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    source_document_id: Option<&str>,
    source_reference: &str,
) -> Result<Option<Uuid>, CreatePurchasePersistenceError> {
    let Some(source_document_id) = source_document_id else {
        return Ok(None);
    };
    use public::source_confirmations::dsl as confirmations;
    use public::source_documents::dsl as documents;

    let source_document_id = parse_uuid(source_document_id, "source_document_id")?;
    let found = documents::source_documents
        .filter(documents::household_id.eq(household_id))
        .filter(documents::id.eq(source_document_id))
        .for_update()
        .select(documents::id)
        .first::<Uuid>(conn)
        .await
        .optional()?
        .is_some();
    if !found {
        return Err(CreatePurchasePersistenceError::ReferenceNotFound(
            "source_document",
        ));
    }
    let latest = confirmations::source_confirmations
        .filter(confirmations::household_id.eq(household_id))
        .filter(confirmations::source_document_id.eq(source_document_id))
        .filter(confirmations::source_reference.eq(source_reference))
        .order_by(confirmations::changed_at.desc())
        .select((
            confirmations::entity_kind,
            confirmations::entity_id,
            confirmations::status,
        ))
        .first::<(String, Uuid, String)>(conn)
        .await
        .optional()?;
    match latest {
        Some((kind, entity_id, status)) if status == "confirmed" && kind == "purchase" => {
            Ok(Some(entity_id))
        }
        Some((_, _, status)) if status == "confirmed" => {
            Err(CreatePurchasePersistenceError::SourceAlreadyConfirmedAsCorrection)
        }
        _ => Ok(None),
    }
}

async fn persist_source_evidence(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    source_document_id: Uuid,
    source_reference: &str,
    purchase_id: Uuid,
    actor_id: Uuid,
    decision: &CreatePurchaseDecision,
) -> Result<(), CreatePurchasePersistenceError> {
    use public::extraction_jobs::dsl as jobs;

    let mut observation = serde_json::json!({});
    let mut receipt_items = Vec::new();
    let mut region_transform = serde_json::Value::Null;
    let mut transform_page = 0_i64;
    let mut receipt_total = None;
    let mut confirmed_total = None;
    let mut extraction_job_id = None;

    if source_reference.is_empty() {
        let latest = jobs::extraction_jobs
            .filter(jobs::household_id.eq(household_id))
            .filter(jobs::source_document_id.eq(source_document_id))
            .filter(jobs::status.eq("succeeded"))
            .order_by(jobs::version.desc())
            .select((jobs::id, jobs::result))
            .first::<(Uuid, Option<serde_json::Value>)>(conn)
            .await
            .optional()?;
        if let Some((job_id, Some(result))) = latest {
            let receipt = result
                .get("receipt")
                .cloned()
                .unwrap_or(serde_json::Value::Null);
            receipt_items = receipt
                .get("items")
                .and_then(serde_json::Value::as_array)
                .cloned()
                .unwrap_or_default();
            let candidate_id = decision.receipt_candidate_id.as_deref().unwrap_or("");
            let override_reason = decision
                .manual_amount_override_reason
                .as_deref()
                .unwrap_or_default()
                .trim();
            if receipt_items.is_empty() && (!candidate_id.is_empty() || !override_reason.is_empty())
            {
                return Err(CreatePurchasePersistenceError::ReceiptConfirmationInvalid);
            }
            let amounts = decision
                .items
                .iter()
                .map(|item| item.amount_yen)
                .collect::<Vec<_>>();
            if !candidate_id.is_empty() {
                let candidates = result
                    .get("candidates")
                    .and_then(serde_json::Value::as_array)
                    .cloned()
                    .unwrap_or_default();
                let candidate = candidates
                    .iter()
                    .find(|candidate| {
                        candidate.get("id").and_then(serde_json::Value::as_str)
                            == Some(candidate_id)
                    })
                    .ok_or(CreatePurchasePersistenceError::ReceiptConfirmationInvalid)?;
                let candidate_amounts = candidate
                    .get("amounts")
                    .and_then(serde_json::Value::as_array)
                    .ok_or(CreatePurchasePersistenceError::ReceiptConfirmationInvalid)?
                    .iter()
                    .map(|amount| {
                        amount
                            .as_i64()
                            .ok_or(CreatePurchasePersistenceError::ReceiptConfirmationInvalid)
                    })
                    .collect::<Result<Vec<_>, _>>()?;
                if override_reason.is_empty() && candidate_amounts != amounts {
                    return Err(CreatePurchasePersistenceError::ReceiptConfirmationInvalid);
                }
            } else if receipt_items.len() == amounts.len() && override_reason.is_empty() {
                let extracted_amounts = receipt_items
                    .iter()
                    .map(|item| {
                        item.get("grossAmount")
                            .and_then(serde_json::Value::as_i64)
                            .ok_or(CreatePurchasePersistenceError::ReceiptConfirmationInvalid)
                    })
                    .collect::<Result<Vec<_>, _>>()?;
                if extracted_amounts != amounts {
                    return Err(CreatePurchasePersistenceError::ReceiptConfirmationInvalid);
                }
            }
            let total = receipt
                .get("total")
                .and_then(serde_json::Value::as_i64)
                .ok_or(CreatePurchasePersistenceError::ReceiptConfirmationInvalid)?;
            let calculated_total = amounts.iter().try_fold(0_i64, |sum, amount| {
                if *amount < 0 || *amount > 1_000_000_000_000_i64.saturating_sub(sum) {
                    return Err(CreatePurchasePersistenceError::ReceiptConfirmationInvalid);
                }
                sum.checked_add(*amount)
                    .ok_or(CreatePurchasePersistenceError::ReceiptConfirmationInvalid)
            })?;
            if override_reason.is_empty() && total != calculated_total {
                return Err(CreatePurchasePersistenceError::ReceiptConfirmationInvalid);
            }
            confirmed_total = Some(calculated_total);
            extraction_job_id = Some(job_id.to_string());
            transform_page = result
                .get("transformPage")
                .and_then(serde_json::Value::as_i64)
                .unwrap_or(0);
            region_transform = result
                .get("transform")
                .cloned()
                .unwrap_or(serde_json::Value::Null);
            receipt_total = Some(total);
            let mut values = serde_json::json!({
                "jobId": job_id.to_string(),
                "receipt": receipt,
                "transform": region_transform,
                "transformPage": transform_page,
                    "confirmedTotal": calculated_total,
                "confirmedItems": decision.items.iter().map(confirmed_item_json).collect::<Vec<_>>(),
            });
            if !candidate_id.is_empty() {
                values["receiptCandidateId"] = serde_json::Value::String(candidate_id.to_owned());
            }
            if !override_reason.is_empty() {
                values["manualAmountOverrideReason"] =
                    serde_json::Value::String(override_reason.to_owned());
            }
            observation = values;
        } else if decision.receipt_candidate_id.is_some()
            || decision
                .manual_amount_override_reason
                .as_deref()
                .is_some_and(|reason| !reason.trim().is_empty())
        {
            return Err(CreatePurchasePersistenceError::ReceiptConfirmationInvalid);
        }
    }

    let item_evidence_matches = source_reference.is_empty()
        && !receipt_items.is_empty()
        && receipt_items.len() == decision.items.len()
        && receipt_items
            .iter()
            .zip(&decision.items)
            .all(|(receipt_item, item)| {
                let extracted_name = receipt_item
                    .get("name")
                    .and_then(serde_json::Value::as_str)
                    .unwrap_or("")
                    .trim();
                let printed_name = receipt_item
                    .get("printedName")
                    .and_then(serde_json::Value::as_str)
                    .unwrap_or("")
                    .trim();
                let name = item.name.trim();
                name == extracted_name || name == printed_name
            });

    if item_evidence_matches {
        for (index, (receipt_item, item)) in receipt_items.iter().zip(&decision.items).enumerate() {
            let mut value = serde_json::json!({
                "jobId": extraction_job_id,
                "itemIndex": index,
                "item": receipt_item,
                "confirmedItem": confirmed_item_json(item),
                "transform": region_transform,
                "transformPage": transform_page,
                "confirmedTotal": confirmed_total,
            });
            if let Some(total) = receipt_total {
                value["receiptTotal"] = serde_json::json!(total);
            }
            if let Some(candidate_id) = decision.receipt_candidate_id.as_deref() {
                value["receiptCandidateId"] = serde_json::Value::String(candidate_id.to_owned());
            }
            if let Some(reason) = decision
                .manual_amount_override_reason
                .as_deref()
                .filter(|reason| !reason.trim().is_empty())
            {
                value["manualAmountOverrideReason"] =
                    serde_json::Value::String(reason.trim().to_owned());
            }
            let item_id = parse_uuid(&item.item_id, "item_id")?;
            insert_evidence_link(
                conn,
                household_id,
                source_document_id,
                purchase_id,
                Some(item_id),
                receipt_item.get("region").cloned(),
                value,
                actor_id,
                source_reference,
            )
            .await?;
        }
    } else {
        if let Some(candidate_id) = decision.receipt_candidate_id.as_deref() {
            observation["receiptCandidateId"] = serde_json::Value::String(candidate_id.to_owned());
        }
        if let Some(reason) = decision
            .manual_amount_override_reason
            .as_deref()
            .filter(|reason| !reason.trim().is_empty())
        {
            observation["manualAmountOverrideReason"] =
                serde_json::Value::String(reason.trim().to_owned());
        }
        insert_evidence_link(
            conn,
            household_id,
            source_document_id,
            purchase_id,
            None,
            None,
            observation,
            actor_id,
            source_reference,
        )
        .await?;
    }
    Ok(())
}

fn confirmed_item_json(
    item: &bookkeeping_application::create_purchase::DecidedPurchaseItem,
) -> serde_json::Value {
    serde_json::json!({
        "purchaseItemId": item.item_id,
        "name": item.name,
        "amountYen": item.amount_yen,
        "categoryId": item.category_id,
        "productVariantId": item.product_variant_id,
        "allocations": item.allocations.iter().map(|allocation| serde_json::json!({
            "poolId": allocation.pool_id,
            "weight": allocation.weight,
        })).collect::<Vec<_>>(),
        "quantity": item.quantity.as_ref().map(|measure| serde_json::json!({
            "numerator": measure.numerator(),
            "denominator": measure.denominator(),
            "unit": measure.unit().as_db_value(),
        })),
        "printedUnitPrice": item.printed_unit_price.as_ref().map(|price| serde_json::json!({
            "amountYen": price.amount_yen,
            "basis": {
                "numerator": price.basis.numerator(),
                "denominator": price.basis.denominator(),
                "unit": price.basis.unit().as_db_value(),
            },
        })),
    })
}

async fn insert_evidence_link(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    source_document_id: Uuid,
    purchase_id: Uuid,
    item_id: Option<Uuid>,
    evidence_region: Option<serde_json::Value>,
    observation: serde_json::Value,
    actor_id: Uuid,
    source_reference: &str,
) -> Result<(), CreatePurchasePersistenceError> {
    diesel::insert_into(public::evidence_links::table)
        .values((
            public::evidence_links::id.eq(Uuid::new_v4()),
            public::evidence_links::household_id.eq(household_id),
            public::evidence_links::source_document_id.eq(source_document_id),
            public::evidence_links::observation_id.eq(None::<Uuid>),
            public::evidence_links::source_reference.eq(source_reference),
            public::evidence_links::entity_kind.eq("purchase"),
            public::evidence_links::entity_id.eq(purchase_id),
            public::evidence_links::item_id.eq(item_id),
            public::evidence_links::evidence_region.eq(evidence_region),
            public::evidence_links::observation.eq(observation),
            public::evidence_links::created_by.eq(actor_id),
            public::evidence_links::created_at.eq(now),
        ))
        .on_conflict_do_nothing()
        .execute(conn)
        .await?;
    Ok(())
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, CreatePurchasePersistenceError> {
    Uuid::parse_str(value).map_err(|_| CreatePurchasePersistenceError::InvalidUuid(field))
}
