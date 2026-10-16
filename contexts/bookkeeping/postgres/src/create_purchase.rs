use bookkeeping_application::create_purchase::CreatePurchaseDecision;
use chrono::{Datelike, FixedOffset, NaiveDate};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use crate::{persistence_values, schema::public};

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

pub struct PersistPurchaseSourceEvidence<'a> {
    pub household_id: Uuid,
    pub source_document_id: Uuid,
    pub source_reference: &'a str,
    pub purchase_id: Uuid,
    pub actor_id: Uuid,
    pub decision: &'a CreatePurchaseDecision,
}

pub struct PersistPurchaseSourceConfirmation<'a> {
    pub household_id: Uuid,
    pub actor_id: Uuid,
    pub idempotency_key: &'a str,
    pub source_document_id: Uuid,
    pub source_reference: &'a str,
    pub purchase_id: Uuid,
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

#[allow(async_fn_in_trait)]
pub trait PurchaseCreationCrossContext {
    async fn require_active_member(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_id: Uuid,
        actor_id: Uuid,
    ) -> Result<bool, CreatePurchasePersistenceError>;
    async fn current_default_allocation_version(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_id: Uuid,
    ) -> Result<i64, CreatePurchasePersistenceError>;
    async fn validate_catalog_references(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_id: Uuid,
        decision: &CreatePurchaseDecision,
    ) -> Result<(), CreatePurchasePersistenceError>;
    async fn resolve_source_confirmation(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_id: Uuid,
        source_document_id: Option<&str>,
        source_reference: &str,
    ) -> Result<Option<Uuid>, CreatePurchasePersistenceError>;
    async fn persist_source_evidence(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        input: PersistPurchaseSourceEvidence<'_>,
    ) -> Result<(), CreatePurchasePersistenceError>;
    async fn persist_source_confirmation(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        input: PersistPurchaseSourceConfirmation<'_>,
    ) -> Result<(), CreatePurchasePersistenceError>;
    async fn publish_purchase_created(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_id: Uuid,
        actor_id: Uuid,
        purchase_id: Uuid,
        change: horse_household_change::UiChangeDescriptor,
    ) -> Result<(), CreatePurchasePersistenceError>;
}

pub async fn persist_create_purchase_in_transaction<C: PurchaseCreationCrossContext>(
    conn: &mut diesel_async::AsyncPgConnection,
    input: PersistCreatePurchase,
    decision: CreatePurchaseDecision,
    cross_context: &mut C,
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
    use public::idempotency_records::dsl as idem;

    // 冪等性判定は業務判断より先に行う。初回成功後に負担ルールが変わっても再送は過去の成功結果を返す。
    if !cross_context
        .require_active_member(conn, household_id, actor_id)
        .await?
    {
        return Err(CreatePurchasePersistenceError::MemberNotFound);
    }
    let current_default_version = cross_context
        .current_default_allocation_version(conn, household_id)
        .await?;
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

    let confirmed_purchase = cross_context
        .resolve_source_confirmation(
            conn,
            household_id,
            decision.source_document_id.as_deref(),
            decision.source_reference.as_deref().unwrap_or(""),
        )
        .await?;
    let effective_purchase_id = confirmed_purchase.unwrap_or(purchase_id);
    if confirmed_purchase.is_none() {
        cross_context
            .validate_catalog_references(conn, household_id, &decision)
            .await?;
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
            public::purchases::date_precision
                .eq(persistence_values::date_precision(decision.date_precision)),
            public::purchases::origin.eq(persistence_values::purchase_origin(decision.origin)),
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
                    Some(persistence_values::quantity_unit(measure.unit())),
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
                    Some(persistence_values::quantity_unit(price.basis.unit())),
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
        cross_context
            .persist_source_confirmation(
                conn,
                PersistPurchaseSourceConfirmation {
                    household_id,
                    actor_id,
                    idempotency_key: &input.idempotency_key,
                    source_document_id,
                    source_reference,
                    purchase_id,
                },
            )
            .await?;
        cross_context
            .persist_source_evidence(
                conn,
                PersistPurchaseSourceEvidence {
                    household_id,
                    source_document_id,
                    source_reference,
                    purchase_id,
                    actor_id,
                    decision: &decision,
                },
            )
            .await?;
    }

    cross_context
        .publish_purchase_created(
            conn,
            household_id,
            actor_id,
            purchase_id,
            decision.ui_change(),
        )
        .await?;
    Ok(PersistedCreatePurchase {
        purchase_id: purchase_id.to_string(),
        created: true,
    })
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, CreatePurchasePersistenceError> {
    Uuid::parse_str(value).map_err(|_| CreatePurchasePersistenceError::InvalidUuid(field))
}
