use chrono::{DateTime, NaiveDate, Utc};
use diesel::{
    BoolExpressionMethods, ExpressionMethods, JoinOnDsl, OptionalExtension, QueryDsl,
    result::Error as DieselError,
};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug, Clone)]
pub struct PurchaseCorrectionProjection {
    pub id: Uuid,
    pub item_id: Option<Uuid>,
    pub amount_delta_yen: i64,
    pub reason: String,
    pub occurred_at: DateTime<Utc>,
    pub settlement_month: NaiveDate,
    pub created_at: DateTime<Utc>,
    pub payment_allocations: Vec<PurchaseCorrectionPaymentProjection>,
}

#[derive(Debug, Clone)]
pub struct PurchaseCorrectionPaymentProjection {
    pub account_id: Uuid,
    pub account_name: String,
    pub account_kind: String,
    pub account_card_last4: Option<String>,
    pub account_pool_id: Option<Uuid>,
    pub amount_yen: i64,
}

#[derive(Debug, Clone)]
pub struct PurchaseCorrectionConnectionProjection {
    pub edges: Vec<PurchaseCorrectionProjection>,
    pub total_count: i64,
    pub has_next_page: bool,
    pub has_previous_page: bool,
}

#[derive(Debug)]
pub enum PurchaseCorrectionError {
    Pool(String),
    Database(DieselError),
    PurchaseNotFound,
    InvalidPageSize,
    InvalidCursor,
    CursorNotFound,
    NumericOutOfRange,
}

impl From<DieselError> for PurchaseCorrectionError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn load_purchase_corrections(
    pool: &PgPool,
    household_id: Uuid,
    purchase_id: Uuid,
    first: Option<i32>,
    after: Option<&str>,
) -> Result<PurchaseCorrectionConnectionProjection, PurchaseCorrectionError> {
    use public::{
        accounts::dsl as accounts, journals::dsl as journals,
        purchase_correction_payments::dsl as correction_payments,
        purchase_corrections::dsl as corrections, purchases::dsl as purchases,
    };

    let page_size = first.unwrap_or(50);
    if !(1..=100).contains(&page_size) {
        return Err(PurchaseCorrectionError::InvalidPageSize);
    }
    let mut conn = pool
        .get()
        .await
        .map_err(|error| PurchaseCorrectionError::Pool(error.to_string()))?;
    let exists = purchases::purchases
        .filter(purchases::household_id.eq(household_id))
        .filter(purchases::id.eq(purchase_id))
        .select(purchases::id)
        .first::<Uuid>(&mut *conn)
        .await
        .optional()?
        .is_some();
    if !exists {
        return Err(PurchaseCorrectionError::PurchaseNotFound);
    }

    let cursor = after
        .filter(|cursor| !cursor.is_empty())
        .map(parse_cursor)
        .transpose()?;
    if let Some(cursor) = cursor.as_ref() {
        let row = corrections::purchase_corrections
            .filter(corrections::household_id.eq(household_id))
            .filter(corrections::purchase_id.eq(purchase_id))
            .filter(corrections::id.eq(cursor.id))
            .select((corrections::created_at, corrections::id))
            .first::<(DateTime<Utc>, Uuid)>(&mut *conn)
            .await
            .optional()?;
        match row {
            Some((created_at, _))
                if cursor.legacy || created_at == cursor.created_at.unwrap_or(created_at) => {}
            _ => return Err(PurchaseCorrectionError::CursorNotFound),
        }
    }
    let total_count = corrections::purchase_corrections
        .filter(corrections::household_id.eq(household_id))
        .filter(corrections::purchase_id.eq(purchase_id))
        .count()
        .get_result::<i64>(&mut *conn)
        .await?;
    let mut query = corrections::purchase_corrections
        .filter(corrections::household_id.eq(household_id))
        .filter(corrections::purchase_id.eq(purchase_id))
        .into_boxed();
    if let Some(cursor) = cursor.as_ref() {
        if cursor.legacy {
            let created_at = corrections::purchase_corrections
                .filter(corrections::household_id.eq(household_id))
                .filter(corrections::purchase_id.eq(purchase_id))
                .filter(corrections::id.eq(cursor.id))
                .select(corrections::created_at)
                .first::<DateTime<Utc>>(&mut *conn)
                .await?;
            query = query.filter(
                corrections::created_at
                    .gt(created_at)
                    .or(corrections::created_at
                        .eq(created_at)
                        .and(corrections::id.gt(cursor.id))),
            );
        } else {
            let created_at = cursor
                .created_at
                .ok_or(PurchaseCorrectionError::InvalidCursor)?;
            query = query.filter(
                corrections::created_at
                    .gt(created_at)
                    .or(corrections::created_at
                        .eq(created_at)
                        .and(corrections::id.gt(cursor.id))),
            );
        }
    }
    let mut rows = query
        .order_by((corrections::created_at, corrections::id))
        .limit(i64::from(page_size) + 1)
        .select((
            corrections::id,
            corrections::item_id,
            corrections::amount_delta_yen,
            corrections::reason,
            corrections::effective_period_start,
            corrections::journal_id,
            corrections::created_at,
        ))
        .load::<(
            Uuid,
            Option<Uuid>,
            i64,
            String,
            NaiveDate,
            Uuid,
            DateTime<Utc>,
        )>(&mut *conn)
        .await?;
    let has_next_page = rows.len() > page_size as usize;
    rows.truncate(page_size as usize);
    let mut edges = Vec::with_capacity(rows.len());
    for row in rows {
        let occurred_at = journals::journals
            .filter(journals::household_id.eq(household_id))
            .filter(journals::id.eq(row.5))
            .select(journals::occurred_at)
            .first::<DateTime<Utc>>(&mut *conn)
            .await?;
        let payments = correction_payments::purchase_correction_payments
            .inner_join(accounts::accounts.on(accounts::id.eq(correction_payments::account_id)))
            .filter(correction_payments::household_id.eq(household_id))
            .filter(correction_payments::correction_id.eq(row.0))
            .order_by(correction_payments::line_no)
            .select((
                accounts::id,
                accounts::name,
                accounts::kind,
                accounts::card_last4,
                accounts::pool_id,
                correction_payments::amount_yen,
            ))
            .load::<(Uuid, String, String, Option<String>, Option<Uuid>, i64)>(&mut *conn)
            .await?;
        edges.push(PurchaseCorrectionProjection {
            id: row.0,
            item_id: row.1,
            amount_delta_yen: row.2,
            reason: row.3,
            settlement_month: row.4,
            occurred_at,
            created_at: row.6,
            payment_allocations: payments
                .into_iter()
                .map(|payment| PurchaseCorrectionPaymentProjection {
                    account_id: payment.0,
                    account_name: payment.1,
                    account_kind: payment.2,
                    account_card_last4: payment.3,
                    account_pool_id: payment.4,
                    amount_yen: payment.5,
                })
                .collect(),
        });
    }
    let has_previous_page = after.is_some_and(|value| !value.is_empty());
    Ok(PurchaseCorrectionConnectionProjection {
        edges,
        total_count,
        has_next_page,
        has_previous_page,
    })
}

struct Cursor {
    created_at: Option<DateTime<Utc>>,
    id: Uuid,
    legacy: bool,
}

fn parse_cursor(value: &str) -> Result<Cursor, PurchaseCorrectionError> {
    use base64::Engine;
    let decoded = base64::engine::general_purpose::URL_SAFE_NO_PAD
        .decode(value)
        .map_err(|_| PurchaseCorrectionError::InvalidCursor)?;
    let text = std::str::from_utf8(&decoded).map_err(|_| PurchaseCorrectionError::InvalidCursor)?;
    let value = text
        .strip_prefix("PurchaseCorrection:")
        .ok_or(PurchaseCorrectionError::InvalidCursor)?;
    if let Some((created_at, id)) = value.split_once('|') {
        let created_at = DateTime::parse_from_rfc3339(created_at)
            .map_err(|_| PurchaseCorrectionError::InvalidCursor)?
            .with_timezone(&Utc);
        let id = Uuid::parse_str(id).map_err(|_| PurchaseCorrectionError::InvalidCursor)?;
        Ok(Cursor {
            created_at: Some(created_at),
            id,
            legacy: false,
        })
    } else {
        let id = Uuid::parse_str(value).map_err(|_| PurchaseCorrectionError::InvalidCursor)?;
        Ok(Cursor {
            created_at: None,
            id,
            legacy: true,
        })
    }
}

pub fn encode_cursor(value: &PurchaseCorrectionProjection) -> String {
    use base64::Engine;
    let value = format!(
        "PurchaseCorrection:{}|{}",
        value.created_at.to_rfc3339(),
        value.id
    );
    base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(value)
}
