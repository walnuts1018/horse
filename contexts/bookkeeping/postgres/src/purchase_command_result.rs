use chrono::{DateTime, Datelike, FixedOffset, NaiveDate, Utc};
use diesel::{
    ExpressionMethods, JoinOnDsl, OptionalExtension, QueryDsl, result::Error as DieselError,
};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug, Clone)]
pub struct PurchaseCommandResult {
    pub id: Uuid,
    pub household_id: Uuid,
    pub occurred_at: DateTime<Utc>,
    pub date_precision: String,
    pub merchant: String,
    pub origin: String,
    pub status: String,
    pub revision: i64,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
    pub has_evidence: bool,
    pub source_kinds: Vec<String>,
    pub total_yen: i64,
    pub correction_amount_yen: i64,
    pub correction_count: i64,
    pub remaining_refundable_amount_yen: i64,
    pub items: Vec<PurchaseItemCommandResult>,
    pub payments: Vec<PurchasePaymentCommandResult>,
}

#[derive(Debug, Clone)]
pub struct PurchaseEditContext {
    pub revision: i64,
    pub status: String,
    pub occurred_at: DateTime<Utc>,
    pub date_precision: String,
    pub merchant: String,
    pub field_revisions: serde_json::Value,
}

#[derive(Debug, Clone)]
pub struct PurchaseItemCommandResult {
    pub id: Uuid,
    pub name: String,
    pub amount_yen: i64,
    pub category_id: Option<Uuid>,
    pub product_variant_id: Option<Uuid>,
    pub revision: i64,
    pub quantity_numerator: Option<i64>,
    pub quantity_denominator: Option<i64>,
    pub quantity_unit: Option<String>,
    pub printed_unit_price_yen: Option<i64>,
    pub printed_unit_price_quantity_numerator: Option<i64>,
    pub printed_unit_price_quantity_denominator: Option<i64>,
    pub printed_unit_price_unit: Option<String>,
    pub allocations: Vec<PurchaseItemAllocationCommandResult>,
    pub tags: Vec<(Uuid, String, bool, i64)>,
    pub remaining_refundable_amount_yen: i64,
}

#[derive(Debug, Clone)]
pub struct PurchaseItemAllocationCommandResult {
    pub pool_id: Uuid,
    pub pool_name: String,
    pub weight: i64,
    pub amount_yen: i64,
}

#[derive(Debug, Clone)]
pub struct PurchasePaymentCommandResult {
    pub account_id: Uuid,
    pub account_name: String,
    pub account_kind: String,
    pub account_card_last4: Option<String>,
    pub account_pool_id: Option<Uuid>,
    pub amount_yen: i64,
}

#[derive(Debug)]
pub enum PurchaseCommandResultError {
    Pool(String),
    Database(DieselError),
    PurchaseNotFound,
    NumericOutOfRange,
    InvalidPurchaseStatus,
}

impl From<DieselError> for PurchaseCommandResultError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn load_purchase_command_result(
    pool: &PgPool,
    household_id: Uuid,
    purchase_id: Uuid,
) -> Result<PurchaseCommandResult, PurchaseCommandResultError> {
    use public::{
        accounting_periods::dsl as periods, accounts::dsl as accounts,
        evidence_links::dsl as evidence, pools::dsl as pools,
        purchase_allocations::dsl as allocations, purchase_corrections::dsl as corrections,
        purchase_item_tags::dsl as item_tags, purchase_items::dsl as items,
        purchase_payments::dsl as payments, purchases::dsl as purchases,
        source_confirmations::dsl as confirmations, source_documents::dsl as documents,
        tags::dsl as tags,
    };

    let mut conn = pool
        .get()
        .await
        .map_err(|error| PurchaseCommandResultError::Pool(error.to_string()))?;
    let base = purchases::purchases
        .filter(purchases::household_id.eq(household_id))
        .filter(purchases::id.eq(purchase_id))
        .select((
            purchases::occurred_at,
            purchases::date_precision,
            purchases::merchant,
            purchases::origin,
            purchases::status,
            purchases::revision,
            purchases::created_at,
            purchases::updated_at,
            purchases::payment_account_id,
        ))
        .first::<(
            DateTime<Utc>,
            String,
            String,
            String,
            String,
            i64,
            DateTime<Utc>,
            DateTime<Utc>,
            Option<Uuid>,
        )>(&mut *conn)
        .await
        .optional()?
        .ok_or(PurchaseCommandResultError::PurchaseNotFound)?;

    let period_start = accounting_period_start(base.0)?;
    let period_status = periods::accounting_periods
        .filter(periods::household_id.eq(household_id))
        .filter(periods::period_start.eq(period_start))
        .select(periods::status)
        .first::<String>(&mut *conn)
        .await
        .optional()?;
    let status = match base.4.as_str() {
        "DRAFT" => "DRAFT",
        "POSTED" => {
            if period_status.as_deref() == Some("CLOSED") {
                "CLOSED"
            } else {
                "POSTED"
            }
        }
        _ => return Err(PurchaseCommandResultError::InvalidPurchaseStatus),
    }
    .to_owned();

    let item_rows = items::purchase_items
        .filter(items::household_id.eq(household_id))
        .filter(items::purchase_id.eq(purchase_id))
        .order_by((items::created_at, items::id))
        .select((
            items::id,
            items::name,
            items::amount_yen,
            items::category_id,
            items::product_variant_id,
            items::revision,
            items::quantity_numerator,
            items::quantity_denominator,
            items::quantity_unit,
            items::printed_unit_price_yen,
            items::printed_unit_price_quantity_numerator,
            items::printed_unit_price_quantity_denominator,
            items::printed_unit_price_unit,
        ))
        .load::<(
            Uuid,
            String,
            i64,
            Option<Uuid>,
            Option<Uuid>,
            i64,
            Option<i64>,
            Option<i64>,
            Option<String>,
            Option<i64>,
            Option<i64>,
            Option<i64>,
            Option<String>,
        )>(&mut *conn)
        .await?;
    let mut item_results = Vec::with_capacity(item_rows.len());
    let mut total_yen = 0_i64;
    for row in item_rows {
        total_yen = total_yen
            .checked_add(row.2)
            .ok_or(PurchaseCommandResultError::NumericOutOfRange)?;
        let allocation_rows = allocations::purchase_allocations
            .inner_join(pools::pools.on(pools::id.eq(allocations::pool_id)))
            .filter(allocations::household_id.eq(household_id))
            .filter(allocations::purchase_id.eq(purchase_id))
            .filter(allocations::item_id.eq(row.0))
            .select((
                allocations::pool_id,
                pools::name,
                allocations::weight,
                allocations::amount_yen,
            ))
            .order_by(allocations::pool_id)
            .load::<(Uuid, String, i64, i64)>(&mut *conn)
            .await?;
        let tag_rows = item_tags::purchase_item_tags
            .inner_join(tags::tags.on(tags::id.eq(item_tags::tag_id)))
            .filter(item_tags::household_id.eq(household_id))
            .filter(item_tags::purchase_id.eq(purchase_id))
            .filter(item_tags::item_id.eq(row.0))
            .select((tags::id, tags::name, tags::archived, tags::revision))
            .order_by(tags::id)
            .load::<(Uuid, String, bool, i64)>(&mut *conn)
            .await?;
        let item_correction_amount = corrections::purchase_corrections
            .filter(corrections::household_id.eq(household_id))
            .filter(corrections::purchase_id.eq(purchase_id))
            .filter(corrections::item_id.eq(Some(row.0)))
            .select(corrections::amount_delta_yen)
            .load::<i64>(&mut *conn)
            .await?
            .into_iter()
            .try_fold(0_i64, |total, value| total.checked_add(value))
            .ok_or(PurchaseCommandResultError::NumericOutOfRange)?;
        let remaining_refundable_amount_yen = row
            .2
            .checked_add(item_correction_amount)
            .ok_or(PurchaseCommandResultError::NumericOutOfRange)?;
        item_results.push(PurchaseItemCommandResult {
            id: row.0,
            name: row.1,
            amount_yen: row.2,
            category_id: row.3,
            product_variant_id: row.4,
            revision: row.5,
            quantity_numerator: row.6,
            quantity_denominator: row.7,
            quantity_unit: row.8,
            printed_unit_price_yen: row.9,
            printed_unit_price_quantity_numerator: row.10,
            printed_unit_price_quantity_denominator: row.11,
            printed_unit_price_unit: row.12,
            allocations: allocation_rows
                .into_iter()
                .map(|allocation| PurchaseItemAllocationCommandResult {
                    pool_id: allocation.0,
                    pool_name: allocation.1,
                    weight: allocation.2,
                    amount_yen: allocation.3,
                })
                .collect(),
            tags: tag_rows,
            remaining_refundable_amount_yen,
        });
    }

    let mut payment_rows = payments::purchase_payments
        .inner_join(accounts::accounts.on(accounts::id.eq(payments::account_id)))
        .filter(payments::household_id.eq(household_id))
        .filter(payments::purchase_id.eq(purchase_id))
        .order_by(payments::line_no)
        .select((
            accounts::id,
            accounts::name,
            accounts::kind,
            accounts::card_last4,
            accounts::pool_id,
            payments::amount_yen,
        ))
        .load::<(Uuid, String, String, Option<String>, Option<Uuid>, i64)>(&mut *conn)
        .await?;
    if payment_rows.is_empty()
        && let Some(account_id) = base.8
    {
        if let Some(account) = accounts::accounts
            .filter(accounts::household_id.eq(household_id))
            .filter(accounts::id.eq(account_id))
            .select((
                accounts::id,
                accounts::name,
                accounts::kind,
                accounts::card_last4,
                accounts::pool_id,
            ))
            .first::<(Uuid, String, String, Option<String>, Option<Uuid>)>(&mut *conn)
            .await
            .optional()?
        {
            payment_rows.push((
                account.0, account.1, account.2, account.3, account.4, total_yen,
            ));
        }
    }
    let payment_results = payment_rows
        .into_iter()
        .map(|payment| PurchasePaymentCommandResult {
            account_id: payment.0,
            account_name: payment.1,
            account_kind: payment.2,
            account_card_last4: payment.3,
            account_pool_id: payment.4,
            amount_yen: payment.5,
        })
        .collect();

    let evidence_rows = evidence::evidence_links
        .inner_join(documents::source_documents.on(documents::id.eq(evidence::source_document_id)))
        .filter(evidence::household_id.eq(household_id))
        .filter(evidence::purchase_id.eq(Some(purchase_id)))
        .select(documents::source_kind)
        .distinct()
        .load::<String>(&mut *conn)
        .await?;
    let confirmed_source_kinds = confirmations::source_confirmations
        .inner_join(
            documents::source_documents.on(documents::id.eq(confirmations::source_document_id)),
        )
        .filter(confirmations::household_id.eq(household_id))
        .filter(confirmations::purchase_id.eq(Some(purchase_id)))
        .select(documents::source_kind)
        .distinct()
        .load::<String>(&mut *conn)
        .await?;
    let mut source_kinds = evidence_rows;
    source_kinds.extend(confirmed_source_kinds);
    source_kinds.sort();
    source_kinds.dedup();
    let has_evidence = evidence::evidence_links
        .filter(evidence::household_id.eq(household_id))
        .filter(evidence::purchase_id.eq(Some(purchase_id)))
        .select(evidence::id)
        .first::<Uuid>(&mut *conn)
        .await
        .optional()?
        .is_some()
        || confirmations::source_confirmations
            .filter(confirmations::household_id.eq(household_id))
            .filter(confirmations::purchase_id.eq(Some(purchase_id)))
            .select(confirmations::id)
            .first::<Uuid>(&mut *conn)
            .await
            .optional()?
            .is_some();
    let correction_rows = corrections::purchase_corrections
        .filter(corrections::household_id.eq(household_id))
        .filter(corrections::purchase_id.eq(purchase_id))
        .select(corrections::amount_delta_yen)
        .load::<i64>(&mut *conn)
        .await?;
    let correction_amount_yen = correction_rows.iter().try_fold(0_i64, |sum, amount| {
        sum.checked_add(*amount)
            .ok_or(PurchaseCommandResultError::NumericOutOfRange)
    })?;
    let remaining_refundable_amount_yen = total_yen
        .checked_add(correction_amount_yen)
        .ok_or(PurchaseCommandResultError::NumericOutOfRange)?;

    Ok(PurchaseCommandResult {
        id: purchase_id,
        household_id,
        occurred_at: base.0,
        date_precision: base.1,
        merchant: base.2,
        origin: base.3,
        status,
        revision: base.5,
        created_at: base.6,
        updated_at: base.7,
        has_evidence,
        source_kinds,
        total_yen,
        correction_amount_yen,
        correction_count: i64::try_from(correction_rows.len())
            .map_err(|_| PurchaseCommandResultError::NumericOutOfRange)?,
        remaining_refundable_amount_yen,
        items: item_results,
        payments: payment_results,
    })
}

pub async fn load_purchase_household_id(
    pool: &PgPool,
    purchase_id: Uuid,
) -> Result<Option<Uuid>, PurchaseCommandResultError> {
    use public::purchases::dsl as purchases;

    let mut conn = pool
        .get()
        .await
        .map_err(|error| PurchaseCommandResultError::Pool(error.to_string()))?;
    purchases::purchases
        .filter(purchases::id.eq(purchase_id))
        .select(purchases::household_id)
        .first::<Uuid>(&mut *conn)
        .await
        .optional()
        .map_err(PurchaseCommandResultError::Database)
}

pub async fn load_purchase_edit_context(
    pool: &PgPool,
    household_id: Uuid,
    purchase_id: Uuid,
) -> Result<Option<PurchaseEditContext>, PurchaseCommandResultError> {
    use public::{accounting_periods::dsl as periods, purchases::dsl as purchases};

    let mut conn = pool
        .get()
        .await
        .map_err(|error| PurchaseCommandResultError::Pool(error.to_string()))?;
    let row = purchases::purchases
        .filter(purchases::household_id.eq(household_id))
        .filter(purchases::id.eq(purchase_id))
        .select((
            purchases::revision,
            purchases::status,
            purchases::occurred_at,
            purchases::date_precision,
            purchases::merchant,
            purchases::field_revisions,
        ))
        .first::<(
            i64,
            String,
            DateTime<Utc>,
            String,
            String,
            serde_json::Value,
        )>(&mut *conn)
        .await
        .optional()?;
    let Some((revision, persisted_status, occurred_at, date_precision, merchant, field_revisions)) =
        row
    else {
        return Ok(None);
    };
    let period_status = periods::accounting_periods
        .filter(periods::household_id.eq(household_id))
        .filter(periods::period_start.eq(accounting_period_start(occurred_at)?))
        .select(periods::status)
        .first::<String>(&mut *conn)
        .await
        .optional()?;
    let status = match persisted_status.as_str() {
        "DRAFT" => "DRAFT",
        "POSTED" => {
            if period_status.as_deref() == Some("CLOSED") {
                "CLOSED"
            } else {
                "POSTED"
            }
        }
        _ => return Err(PurchaseCommandResultError::InvalidPurchaseStatus),
    };
    Ok(Some(PurchaseEditContext {
        revision,
        status: status.to_owned(),
        occurred_at,
        date_precision,
        merchant,
        field_revisions,
    }))
}

fn accounting_period_start(
    occurred_at: DateTime<Utc>,
) -> Result<NaiveDate, PurchaseCommandResultError> {
    let tokyo = FixedOffset::east_opt(9 * 60 * 60)
        .ok_or(PurchaseCommandResultError::InvalidPurchaseStatus)?;
    occurred_at
        .with_timezone(&tokyo)
        .date_naive()
        .with_day(1)
        .ok_or(PurchaseCommandResultError::InvalidPurchaseStatus)
}

pub async fn load_purchase_item_household_ids(
    pool: &PgPool,
    item_ids: &[Uuid],
) -> Result<Vec<(Uuid, Uuid)>, PurchaseCommandResultError> {
    use public::purchase_items::dsl as items;

    let mut conn = pool
        .get()
        .await
        .map_err(|error| PurchaseCommandResultError::Pool(error.to_string()))?;
    items::purchase_items
        .filter(items::id.eq_any(item_ids))
        .select((items::id, items::household_id))
        .load::<(Uuid, Uuid)>(&mut *conn)
        .await
        .map_err(PurchaseCommandResultError::Database)
}

pub async fn load_purchase_item_household_id(
    pool: &PgPool,
    item_id: Uuid,
) -> Result<Option<Uuid>, PurchaseCommandResultError> {
    use public::purchase_items::dsl as items;

    let mut conn = pool
        .get()
        .await
        .map_err(|error| PurchaseCommandResultError::Pool(error.to_string()))?;
    items::purchase_items
        .filter(items::id.eq(item_id))
        .select(items::household_id)
        .first::<Uuid>(&mut *conn)
        .await
        .optional()
        .map_err(PurchaseCommandResultError::Database)
}
