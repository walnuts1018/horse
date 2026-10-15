use chrono::{DateTime, Datelike, FixedOffset, NaiveDate, Utc};
use diesel::{
    BoolExpressionMethods, ExpressionMethods, JoinOnDsl, OptionalExtension, QueryDsl,
    result::Error as DieselError,
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

pub async fn load_purchase_command_results(
    pool: &PgPool,
    household_ids: &[Uuid],
    purchase_ids: &[Uuid],
) -> Result<Vec<PurchaseCommandResult>, PurchaseCommandResultError> {
    use public::{
        accounting_periods::dsl as periods, accounts::dsl as accounts,
        evidence_links::dsl as evidence, pools::dsl as pools,
        purchase_allocations::dsl as allocations, purchase_corrections::dsl as corrections,
        purchase_item_tags::dsl as item_tags, purchase_items::dsl as items,
        purchase_payments::dsl as payments, purchases::dsl as purchases,
        source_confirmations::dsl as confirmations, source_documents::dsl as documents,
        tags::dsl as tags,
    };

    if household_ids.is_empty() || purchase_ids.is_empty() {
        return Ok(Vec::new());
    }
    let mut conn = pool
        .get()
        .await
        .map_err(|error| PurchaseCommandResultError::Pool(error.to_string()))?;
    let bases = purchases::purchases
        .filter(purchases::household_id.eq_any(household_ids))
        .filter(purchases::id.eq_any(purchase_ids))
        .select((
            purchases::id,
            purchases::household_id,
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
        .load::<(
            Uuid,
            Uuid,
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
        .await?;
    if bases.is_empty() {
        return Ok(Vec::new());
    }
    let mut period_starts = Vec::with_capacity(bases.len());
    for base in &bases {
        period_starts.push(accounting_period_start(base.2)?);
    }
    let period_rows = periods::accounting_periods
        .filter(periods::household_id.eq_any(household_ids))
        .filter(periods::period_start.eq_any(&period_starts))
        .select((
            periods::household_id,
            periods::period_start,
            periods::status,
        ))
        .load::<(Uuid, NaiveDate, String)>(&mut *conn)
        .await?;
    let period_statuses = period_rows
        .into_iter()
        .map(|(household_id, period_start, status)| ((household_id, period_start), status))
        .collect::<std::collections::HashMap<_, _>>();

    let item_rows = items::purchase_items
        .filter(items::household_id.eq_any(household_ids))
        .filter(items::purchase_id.eq_any(purchase_ids))
        .order_by((items::created_at, items::id))
        .select((
            items::id,
            items::household_id,
            items::purchase_id,
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
            Uuid,
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
    let item_ids = item_rows.iter().map(|row| row.0).collect::<Vec<_>>();
    let allocation_rows = if item_ids.is_empty() {
        Vec::new()
    } else {
        allocations::purchase_allocations
            .inner_join(pools::pools.on(pools::id.eq(allocations::pool_id)))
            .filter(allocations::household_id.eq_any(household_ids))
            .filter(allocations::purchase_id.eq_any(purchase_ids))
            .filter(allocations::item_id.eq_any(&item_ids))
            .select((
                allocations::item_id,
                allocations::pool_id,
                pools::name,
                allocations::weight,
                allocations::amount_yen,
            ))
            .order_by((allocations::item_id, allocations::pool_id))
            .load::<(Uuid, Uuid, String, i64, i64)>(&mut *conn)
            .await?
    };
    let mut allocations_by_item = std::collections::HashMap::new();
    for (item_id, pool_id, pool_name, weight, amount_yen) in allocation_rows {
        allocations_by_item
            .entry(item_id)
            .or_insert_with(Vec::new)
            .push(PurchaseItemAllocationCommandResult {
                pool_id,
                pool_name,
                weight,
                amount_yen,
            });
    }
    let tag_rows = if item_ids.is_empty() {
        Vec::new()
    } else {
        item_tags::purchase_item_tags
            .inner_join(tags::tags.on(tags::id.eq(item_tags::tag_id)))
            .filter(item_tags::household_id.eq_any(household_ids))
            .filter(item_tags::purchase_id.eq_any(purchase_ids))
            .filter(item_tags::item_id.eq_any(&item_ids))
            .select((
                item_tags::item_id,
                tags::id,
                tags::name,
                tags::archived,
                tags::revision,
            ))
            .order_by((item_tags::item_id, tags::id))
            .load::<(Uuid, Uuid, String, bool, i64)>(&mut *conn)
            .await?
    };
    let mut tags_by_item = std::collections::HashMap::new();
    for (item_id, tag_id, name, archived, revision) in tag_rows {
        tags_by_item
            .entry(item_id)
            .or_insert_with(Vec::new)
            .push((tag_id, name, archived, revision));
    }
    let correction_rows = corrections::purchase_corrections
        .filter(corrections::household_id.eq_any(household_ids))
        .filter(corrections::purchase_id.eq_any(purchase_ids))
        .select((
            corrections::household_id,
            corrections::purchase_id,
            corrections::item_id,
            corrections::amount_delta_yen,
        ))
        .load::<(Uuid, Uuid, Option<Uuid>, i64)>(&mut *conn)
        .await?;
    let mut correction_totals = std::collections::HashMap::<(Uuid, Uuid), (i64, i64)>::new();
    let mut item_correction_totals = std::collections::HashMap::<Uuid, i64>::new();
    for (household_id, purchase_id, item_id, amount_yen) in &correction_rows {
        let totals = correction_totals
            .entry((*household_id, *purchase_id))
            .or_default();
        totals.0 = totals
            .0
            .checked_add(*amount_yen)
            .ok_or(PurchaseCommandResultError::NumericOutOfRange)?;
        totals.1 = totals
            .1
            .checked_add(1)
            .ok_or(PurchaseCommandResultError::NumericOutOfRange)?;
        if let Some(item_id) = item_id {
            let total = item_correction_totals.entry(*item_id).or_default();
            *total = total
                .checked_add(*amount_yen)
                .ok_or(PurchaseCommandResultError::NumericOutOfRange)?;
        }
    }
    let payment_rows = payments::purchase_payments
        .inner_join(
            accounts::accounts.on(accounts::id
                .eq(payments::account_id)
                .and(accounts::household_id.eq(payments::household_id))),
        )
        .filter(payments::household_id.eq_any(household_ids))
        .filter(payments::purchase_id.eq_any(purchase_ids))
        .order_by((payments::purchase_id, payments::line_no))
        .select((
            payments::household_id,
            payments::purchase_id,
            accounts::id,
            accounts::name,
            accounts::kind,
            accounts::card_last4,
            accounts::pool_id,
            payments::amount_yen,
        ))
        .load::<(
            Uuid,
            Uuid,
            Uuid,
            String,
            String,
            Option<String>,
            Option<Uuid>,
            i64,
        )>(&mut *conn)
        .await?;
    let mut payments_by_purchase = std::collections::HashMap::new();
    for (
        household_id,
        purchase_id,
        account_id,
        account_name,
        account_kind,
        card_last4,
        pool_id,
        amount_yen,
    ) in payment_rows
    {
        payments_by_purchase
            .entry((household_id, purchase_id))
            .or_insert_with(Vec::new)
            .push(PurchasePaymentCommandResult {
                account_id,
                account_name,
                account_kind,
                account_card_last4: card_last4,
                account_pool_id: pool_id,
                amount_yen,
            });
    }
    let mut fallback_account_ids = bases
        .iter()
        .filter_map(|base| {
            (payments_by_purchase
                .get(&(base.1, base.0))
                .is_none_or(Vec::is_empty))
            .then_some((base.1, base.0, base.10))
        })
        .filter_map(|(household_id, purchase_id, account_id)| {
            account_id.map(|account_id| (household_id, purchase_id, account_id))
        })
        .collect::<Vec<_>>();
    let fallback_ids = fallback_account_ids
        .iter()
        .map(|(_, _, account_id)| *account_id)
        .collect::<Vec<_>>();
    let fallback_accounts = if fallback_ids.is_empty() {
        Vec::new()
    } else {
        accounts::accounts
            .filter(accounts::household_id.eq_any(household_ids))
            .filter(accounts::id.eq_any(&fallback_ids))
            .select((
                accounts::household_id,
                accounts::id,
                accounts::name,
                accounts::kind,
                accounts::card_last4,
                accounts::pool_id,
            ))
            .load::<(Uuid, Uuid, String, String, Option<String>, Option<Uuid>)>(&mut *conn)
            .await?
    };
    let fallback_by_account = fallback_accounts
        .into_iter()
        .map(|row| ((row.0, row.1), (row.2, row.3, row.4, row.5)))
        .collect::<std::collections::HashMap<_, _>>();
    let totals_by_purchase = item_rows.iter().try_fold(
        std::collections::HashMap::<(Uuid, Uuid), i64>::new(),
        |mut totals, row| {
            let total = totals.entry((row.1, row.2)).or_default();
            *total = total
                .checked_add(row.4)
                .ok_or(PurchaseCommandResultError::NumericOutOfRange)?;
            Ok::<_, PurchaseCommandResultError>(totals)
        },
    )?;
    for (household_id, purchase_id, account_id) in fallback_account_ids.drain(..) {
        if let Some((name, kind, card_last4, pool_id)) =
            fallback_by_account.get(&(household_id, account_id))
        {
            payments_by_purchase
                .entry((household_id, purchase_id))
                .or_insert_with(Vec::new)
                .push(PurchasePaymentCommandResult {
                    account_id,
                    account_name: name.clone(),
                    account_kind: kind.clone(),
                    account_card_last4: card_last4.clone(),
                    account_pool_id: *pool_id,
                    amount_yen: totals_by_purchase
                        .get(&(household_id, purchase_id))
                        .copied()
                        .unwrap_or_default(),
                });
        }
    }
    let optional_purchase_ids = purchase_ids.iter().copied().map(Some).collect::<Vec<_>>();
    let evidence_rows = evidence::evidence_links
        .inner_join(documents::source_documents.on(documents::id.eq(evidence::source_document_id)))
        .filter(evidence::household_id.eq_any(household_ids))
        .filter(evidence::purchase_id.eq_any(&optional_purchase_ids))
        .select((
            evidence::household_id,
            evidence::purchase_id,
            documents::source_kind,
        ))
        .distinct()
        .load::<(Uuid, Option<Uuid>, String)>(&mut *conn)
        .await?;
    let confirmation_rows = confirmations::source_confirmations
        .inner_join(
            documents::source_documents.on(documents::id.eq(confirmations::source_document_id)),
        )
        .filter(confirmations::household_id.eq_any(household_ids))
        .filter(confirmations::purchase_id.eq_any(&optional_purchase_ids))
        .select((
            confirmations::household_id,
            confirmations::purchase_id,
            documents::source_kind,
        ))
        .distinct()
        .load::<(Uuid, Option<Uuid>, String)>(&mut *conn)
        .await?;
    let mut evidence_keys = std::collections::HashSet::new();
    let mut source_kinds_by_purchase =
        std::collections::HashMap::<(Uuid, Uuid), Vec<String>>::new();
    for (household_id, purchase_id, source_kind) in evidence_rows {
        if let Some(purchase_id) = purchase_id {
            evidence_keys.insert((household_id, purchase_id));
            source_kinds_by_purchase
                .entry((household_id, purchase_id))
                .or_default()
                .push(source_kind);
        }
    }
    for (household_id, purchase_id, source_kind) in confirmation_rows {
        if let Some(purchase_id) = purchase_id {
            evidence_keys.insert((household_id, purchase_id));
            source_kinds_by_purchase
                .entry((household_id, purchase_id))
                .or_default()
                .push(source_kind);
        }
    }
    let mut items_by_purchase = std::collections::HashMap::new();
    for row in item_rows {
        let item_correction_total = item_correction_totals
            .get(&row.0)
            .copied()
            .unwrap_or_default();
        let remaining_refundable_amount_yen = row
            .4
            .checked_add(item_correction_total)
            .ok_or(PurchaseCommandResultError::NumericOutOfRange)?;
        items_by_purchase
            .entry((row.1, row.2))
            .or_insert_with(Vec::new)
            .push(PurchaseItemCommandResult {
                id: row.0,
                name: row.3,
                amount_yen: row.4,
                category_id: row.5,
                product_variant_id: row.6,
                revision: row.7,
                quantity_numerator: row.8,
                quantity_denominator: row.9,
                quantity_unit: row.10,
                printed_unit_price_yen: row.11,
                printed_unit_price_quantity_numerator: row.12,
                printed_unit_price_quantity_denominator: row.13,
                printed_unit_price_unit: row.14,
                allocations: allocations_by_item.remove(&row.0).unwrap_or_default(),
                tags: tags_by_item.remove(&row.0).unwrap_or_default(),
                remaining_refundable_amount_yen,
            });
    }
    let mut results = Vec::with_capacity(bases.len());
    for base in bases {
        let period_start = accounting_period_start(base.2)?;
        let status = match base.6.as_str() {
            "DRAFT" => "DRAFT",
            "POSTED" => {
                if period_statuses
                    .get(&(base.1, period_start))
                    .map(String::as_str)
                    == Some("CLOSED")
                {
                    "CLOSED"
                } else {
                    "POSTED"
                }
            }
            _ => return Err(PurchaseCommandResultError::InvalidPurchaseStatus),
        }
        .to_owned();
        let (correction_amount_yen, correction_count) = correction_totals
            .get(&(base.1, base.0))
            .copied()
            .unwrap_or_default();
        let items = items_by_purchase
            .remove(&(base.1, base.0))
            .unwrap_or_default();
        let total_yen = items.iter().try_fold(0_i64, |total, item| {
            total
                .checked_add(item.amount_yen)
                .ok_or(PurchaseCommandResultError::NumericOutOfRange)
        })?;
        let remaining_refundable_amount_yen = total_yen
            .checked_add(correction_amount_yen)
            .ok_or(PurchaseCommandResultError::NumericOutOfRange)?;
        let mut source_kinds = source_kinds_by_purchase
            .remove(&(base.1, base.0))
            .unwrap_or_default();
        source_kinds.sort();
        source_kinds.dedup();
        results.push(PurchaseCommandResult {
            id: base.0,
            household_id: base.1,
            occurred_at: base.2,
            date_precision: base.3,
            merchant: base.4,
            origin: base.5,
            status,
            revision: base.7,
            created_at: base.8,
            updated_at: base.9,
            has_evidence: evidence_keys.contains(&(base.1, base.0)),
            source_kinds,
            total_yen,
            correction_amount_yen,
            correction_count,
            remaining_refundable_amount_yen,
            items,
            payments: payments_by_purchase
                .remove(&(base.1, base.0))
                .unwrap_or_default(),
        });
    }
    Ok(results)
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
