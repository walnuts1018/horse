use std::collections::BTreeMap;

use async_trait::async_trait;
use chrono::{DateTime, Datelike, FixedOffset, NaiveDate, TimeZone, Utc};
use diesel::{ExpressionMethods, JoinOnDsl, QueryDsl, dsl::count};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use crate::{PgPool, schema::public};
use cross_context_application::home_summary_query::{
    HomeDailySpendingProjection, HomeSummaryProjection, HomeSummaryQuery, HomeSummaryQueryError,
};

#[derive(Debug)]
enum PgHomeSummaryQueryError {
    Pool(String),
    Database(diesel::result::Error),
    HouseholdNotFound,
    AmountOutOfRange,
}

impl From<diesel::result::Error> for PgHomeSummaryQueryError {
    fn from(error: diesel::result::Error) -> Self {
        Self::Database(error)
    }
}

pub struct PostgresHomeSummaryQuery {
    pool: PgPool,
}

impl PostgresHomeSummaryQuery {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait]
impl HomeSummaryQuery for PostgresHomeSummaryQuery {
    async fn load(
        &self,
        actor_id: Uuid,
        household_id: Uuid,
    ) -> Result<HomeSummaryProjection, HomeSummaryQueryError> {
        home_summary(&self.pool, actor_id, household_id)
            .await
            .map_err(|error| match error {
                PgHomeSummaryQueryError::HouseholdNotFound => {
                    HomeSummaryQueryError::HouseholdNotFound
                }
                PgHomeSummaryQueryError::AmountOutOfRange => {
                    HomeSummaryQueryError::AmountOutOfRange
                }
                PgHomeSummaryQueryError::Pool(message) => HomeSummaryQueryError::Failed(message),
                PgHomeSummaryQueryError::Database(error) => {
                    HomeSummaryQueryError::Failed(error.to_string())
                }
            })
    }
}

async fn home_summary(
    pool: &PgPool,
    actor_id: Uuid,
    household_id: Uuid,
) -> Result<HomeSummaryProjection, PgHomeSummaryQueryError> {
    let tokyo = FixedOffset::east_opt(9 * 60 * 60).expect("Tokyo offset is valid");
    let today = Utc::now().with_timezone(&tokyo).date_naive();
    let start_of_week = today - chrono::Duration::days(6);
    let start_of_month = today.with_day(1).expect("first day of month is valid");
    let start = tokyo
        .from_local_datetime(&start_of_week.and_hms_opt(0, 0, 0).expect("valid midnight"))
        .single()
        .expect("fixed offset is unambiguous")
        .with_timezone(&Utc);
    let end = tokyo
        .from_local_datetime(
            &(today + chrono::Duration::days(1))
                .and_hms_opt(0, 0, 0)
                .expect("valid midnight"),
        )
        .single()
        .expect("fixed offset is unambiguous")
        .with_timezone(&Utc);
    let month_start = tokyo
        .from_local_datetime(&start_of_month.and_hms_opt(0, 0, 0).expect("valid midnight"))
        .single()
        .expect("fixed offset is unambiguous")
        .with_timezone(&Utc);
    let next_month = start_of_month
        .checked_add_months(chrono::Months::new(1))
        .expect("next month is representable");
    let month_end = tokyo
        .from_local_datetime(&next_month.and_hms_opt(0, 0, 0).expect("valid midnight"))
        .single()
        .expect("fixed offset is unambiguous")
        .with_timezone(&Utc);

    let mut conn = pool
        .get()
        .await
        .map_err(|error| PgHomeSummaryQueryError::Pool(error.to_string()))?;
    require_member(&mut conn, actor_id, household_id).await?;

    use public::{purchase_corrections, purchase_items, purchases};
    let item_rows = purchase_items::table
        .inner_join(purchases::table.on(purchases::id.eq(purchase_items::purchase_id)))
        .filter(purchases::household_id.eq(household_id))
        .filter(purchases::occurred_at.ge(start))
        .filter(purchases::occurred_at.lt(end))
        .filter(purchases::status.eq("POSTED"))
        .select((purchases::occurred_at, purchase_items::amount_yen))
        .load::<(DateTime<Utc>, i64)>(&mut conn)
        .await?;
    let correction_rows = purchase_corrections::table
        .inner_join(purchases::table.on(purchases::id.eq(purchase_corrections::purchase_id)))
        .filter(purchases::household_id.eq(household_id))
        .filter(purchases::occurred_at.ge(start))
        .filter(purchases::occurred_at.lt(end))
        .filter(purchases::status.eq("POSTED"))
        .select((
            purchases::occurred_at,
            purchase_corrections::amount_delta_yen,
        ))
        .load::<(DateTime<Utc>, i64)>(&mut conn)
        .await?;
    let mut totals = BTreeMap::<NaiveDate, i64>::new();
    for (occurred_at, amount) in item_rows.into_iter().chain(correction_rows) {
        let date = occurred_at.with_timezone(&tokyo).date_naive();
        let total = totals.entry(date).or_default();
        *total = total
            .checked_add(amount)
            .ok_or(PgHomeSummaryQueryError::AmountOutOfRange)?;
    }
    let daily_spending = (0..7)
        .map(|offset| {
            let date = start_of_week + chrono::Duration::days(offset);
            HomeDailySpendingProjection {
                date,
                amount_yen: totals.get(&date).copied().unwrap_or_default(),
            }
        })
        .collect();
    let monthly_item_count = purchase_items::table
        .inner_join(purchases::table.on(purchases::id.eq(purchase_items::purchase_id)))
        .filter(purchases::household_id.eq(household_id))
        .filter(purchases::occurred_at.ge(month_start))
        .filter(purchases::occurred_at.lt(month_end))
        .filter(purchases::status.eq("POSTED"))
        .select(count(purchase_items::id))
        .first::<i64>(&mut conn)
        .await?;
    Ok(HomeSummaryProjection {
        daily_spending,
        monthly_item_count,
    })
}

async fn require_member(
    conn: &mut diesel_async::AsyncPgConnection,
    actor_id: Uuid,
    household_id: Uuid,
) -> Result<(), PgHomeSummaryQueryError> {
    use public::household_members;
    let active = diesel::select(diesel::dsl::exists(
        household_members::table
            .filter(household_members::household_id.eq(household_id))
            .filter(household_members::member_id.eq(actor_id))
            .filter(household_members::left_at.is_null()),
    ))
    .get_result::<bool>(conn)
    .await?;
    if active {
        Ok(())
    } else {
        Err(PgHomeSummaryQueryError::HouseholdNotFound)
    }
}
