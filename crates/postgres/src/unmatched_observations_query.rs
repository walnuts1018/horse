use chrono::{DateTime, Datelike, NaiveDate, TimeZone, Utc};
use diesel::{
    BoolExpressionMethods, ExpressionMethods, OptionalExtension, QueryDsl,
    result::Error as DieselError,
};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Clone, Debug)]
pub struct ObservationRow {
    pub document_id: Uuid,
    pub source_reference: String,
    pub source_kind: String,
    pub merchant: String,
    pub occurred_at: Option<DateTime<Utc>>,
    pub amount_yen: Option<i64>,
    pub order_id: String,
    pub transaction_id: String,
    pub document_created_at: DateTime<Utc>,
    pub confirmed_purchase_id: Option<Uuid>,
}

#[derive(Clone, Debug)]
pub struct ObservationPage {
    pub rows: Vec<ObservationRow>,
    pub total_count: i64,
    pub has_previous_page: bool,
    pub has_next_page: bool,
}

#[derive(Debug)]
pub enum UnmatchedObservationsError {
    Pool(String),
    Database(DieselError),
    HouseholdNotFound,
    InvalidPagination,
    InvalidPeriod,
    InvalidRange,
    MissingCursor,
}

impl From<DieselError> for UnmatchedObservationsError {
    fn from(value: DieselError) -> Self {
        Self::Database(value)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ObservationCursor {
    pub document_id: Uuid,
    pub source_reference: String,
}

pub async fn unmatched_observations(
    pool: &PgPool,
    actor_id: Uuid,
    household_id: Uuid,
    from: Option<&str>,
    to: Option<&str>,
    source_kind: Option<&str>,
    state: Option<&str>,
    after: Option<ObservationCursor>,
    before: Option<ObservationCursor>,
    first: Option<i32>,
    last: Option<i32>,
) -> Result<ObservationPage, UnmatchedObservationsError> {
    if !crate::household_access::is_active_member(pool, household_id, actor_id)
        .await
        .map_err(|e| match e {
            crate::household_access::HouseholdAccessError::Pool(message) => {
                UnmatchedObservationsError::Pool(message)
            }
            crate::household_access::HouseholdAccessError::Database(error) => {
                UnmatchedObservationsError::Database(error)
            }
        })?
    {
        return Err(UnmatchedObservationsError::HouseholdNotFound);
    }
    let (size, backward) = page_size(first, last)?;
    let from = period_start(from)?;
    let to = period_end(to)?;
    if from.zip(to).is_some_and(|(from, to)| to < from) {
        return Err(UnmatchedObservationsError::InvalidRange);
    }
    let mut conn = pool
        .get()
        .await
        .map_err(|e| UnmatchedObservationsError::Pool(e.to_string()))?;
    use public::{
        source_confirmations as confirmations, source_documents as documents,
        source_observations as observations,
    };

    let mut query = observations::table
        .inner_join(documents::table.on(documents::id.eq(observations::source_document_id)))
        .filter(observations::household_id.eq(household_id))
        .filter(documents::household_id.eq(household_id))
        .into_boxed();
    if let Some(kind) = source_kind {
        query = query.filter(observations::source_kind.eq(kind.to_ascii_lowercase()));
    }
    if let Some(from) = from {
        query = query.filter(observations::occurred_at.ge(from));
    }
    if let Some(to) = to {
        query = query.filter(observations::occurred_at.lt(to));
    }
    let is_confirmed = diesel::dsl::exists(
        confirmations::table
            .filter(confirmations::household_id.eq(household_id))
            .filter(confirmations::source_document_id.eq(observations::source_document_id))
            .filter(confirmations::source_reference.eq(observations::source_reference))
            .filter(
                confirmations::purchase_id
                    .is_not_null()
                    .or(confirmations::correction_id.is_not_null()),
            ),
    );
    match state {
        Some("CONFIRMED") => query = query.filter(is_confirmed),
        Some("UNMATCHED") => query = query.filter(diesel::dsl::not(is_confirmed)),
        Some(_) => return Err(UnmatchedObservationsError::InvalidRange),
        None => (),
    }
    let mut all = query
        .select((
            observations::source_document_id,
            observations::source_reference,
            observations::source_kind,
            observations::merchant,
            observations::occurred_at,
            observations::amount_yen,
            observations::external_order_id,
            observations::external_transaction_id,
            documents::created_at,
        ))
        .load::<(
            Uuid,
            String,
            String,
            String,
            Option<DateTime<Utc>>,
            Option<i64>,
            String,
            String,
            DateTime<Utc>,
        )>(&mut *conn)
        .await?;
    let total_count = i64::try_from(all.len()).unwrap_or(i64::MAX);
    all.sort_by(|a, b| {
        b.8.cmp(&a.8)
            .then_with(|| b.0.cmp(&a.0))
            .then_with(|| ordinal_cmp(row_ordinal(&a.1), row_ordinal(&b.1)))
            .then_with(|| a.1.cmp(&b.1))
    });
    let cursor_position = |cursor: &ObservationCursor,
                           rows: &[(
        Uuid,
        String,
        String,
        String,
        Option<DateTime<Utc>>,
        Option<i64>,
        String,
        String,
        DateTime<Utc>,
    )]| {
        rows.iter()
            .position(|row| row.0 == cursor.document_id && row.1 == cursor.source_reference)
    };
    let start = match after.as_ref() {
        Some(c) => cursor_position(c, &all).ok_or(UnmatchedObservationsError::MissingCursor)? + 1,
        None => 0,
    };
    let end = match before.as_ref() {
        Some(c) => cursor_position(c, &all).ok_or(UnmatchedObservationsError::MissingCursor)?,
        None => all.len(),
    };
    if start > end {
        return Err(UnmatchedObservationsError::InvalidRange);
    }
    let (page_start, page_end) = if backward {
        (start.max(end.saturating_sub(size)), end)
    } else {
        (start, end.min(start.saturating_add(size)))
    };
    let selected = &all[page_start..page_end];
    let mut rows = Vec::with_capacity(selected.len());
    for item in selected {
        let confirmation = confirmations::table
            .filter(confirmations::household_id.eq(household_id))
            .filter(confirmations::source_document_id.eq(item.0))
            .filter(confirmations::source_reference.eq(&item.1))
            .select((confirmations::purchase_id, confirmations::correction_id))
            .first::<(Option<Uuid>, Option<Uuid>)>(&mut *conn)
            .await
            .optional()?;
        let purchase_id = if let Some((Some(purchase_id), _)) = confirmation.as_ref() {
            Some(*purchase_id)
        } else if let Some((_, Some(correction_id))) = confirmation.as_ref() {
            public::purchase_corrections::table
                .filter(public::purchase_corrections::id.eq(*correction_id))
                .filter(public::purchase_corrections::household_id.eq(household_id))
                .select(public::purchase_corrections::purchase_id)
                .first::<Uuid>(&mut *conn)
                .await
                .optional()?
        } else {
            None
        };
        rows.push(ObservationRow {
            document_id: item.0,
            source_reference: item.1.clone(),
            source_kind: item.2.clone(),
            merchant: item.3.clone(),
            occurred_at: item.4,
            amount_yen: item.5,
            order_id: item.6.clone(),
            transaction_id: item.7.clone(),
            document_created_at: item.8,
            confirmed_purchase_id: purchase_id,
        });
    }
    Ok(ObservationPage {
        rows,
        total_count,
        has_previous_page: page_start > 0,
        has_next_page: page_end < all.len(),
    })
}

fn page_size(
    first: Option<i32>,
    last: Option<i32>,
) -> Result<(usize, bool), UnmatchedObservationsError> {
    if first.is_some() && last.is_some() {
        return Err(UnmatchedObservationsError::InvalidPagination);
    }
    let size = first.or(last).unwrap_or(50);
    if !(1..=100).contains(&size) {
        return Err(UnmatchedObservationsError::InvalidPagination);
    }
    Ok((size as usize, last.is_some()))
}
fn period_start(value: Option<&str>) -> Result<Option<DateTime<Utc>>, UnmatchedObservationsError> {
    value
        .map(|v| {
            parse_period(v).map(|date| {
                Utc.from_utc_datetime(&date.and_hms_opt(0, 0, 0).expect("midnight exists"))
            })
        })
        .transpose()
}
fn period_end(value: Option<&str>) -> Result<Option<DateTime<Utc>>, UnmatchedObservationsError> {
    value
        .map(|v| {
            let date = parse_period(v)?;
            let (year, month) = if date.month() == 12 {
                (date.year() + 1, 1)
            } else {
                (date.year(), date.month() + 1)
            };
            let next = NaiveDate::from_ymd_opt(year, month, 1)
                .ok_or(UnmatchedObservationsError::InvalidPeriod)?;
            Ok(Utc.from_utc_datetime(&next.and_hms_opt(0, 0, 0).expect("midnight exists")))
        })
        .transpose()
}
fn parse_period(value: &str) -> Result<NaiveDate, UnmatchedObservationsError> {
    if value.len() != 7 || value.as_bytes()[4] != b'-' {
        return Err(UnmatchedObservationsError::InvalidPeriod);
    }
    NaiveDate::parse_from_str(&format!("{value}-01"), "%Y-%m-%d")
        .map_err(|_| UnmatchedObservationsError::InvalidPeriod)
}
fn row_ordinal(value: &str) -> Option<u64> {
    value.strip_prefix("row:")?.parse().ok()
}
fn ordinal_cmp(left: Option<u64>, right: Option<u64>) -> std::cmp::Ordering {
    match (left, right) {
        (Some(left), Some(right)) => left.cmp(&right),
        (Some(_), None) => std::cmp::Ordering::Less,
        (None, Some(_)) => std::cmp::Ordering::Greater,
        (None, None) => std::cmp::Ordering::Equal,
    }
}
