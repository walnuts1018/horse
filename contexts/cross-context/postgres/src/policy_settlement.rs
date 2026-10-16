use chrono::{DateTime, NaiveDate, Utc};
use diesel::{BoolExpressionMethods, ExpressionMethods, QueryDsl};
use diesel_async::{AsyncPgConnection, RunQueryDsl};
use uuid::Uuid;

pub type FundingScheduleRow = (
    Uuid,
    Uuid,
    Uuid,
    i64,
    String,
    NaiveDate,
    Option<NaiveDate>,
    bool,
    DateTime<Utc>,
);
pub type FundingRuleRow = (Uuid, i32, Option<Uuid>, Option<String>);
pub type FundingShareRow = (Uuid, i32, Uuid, i64);

pub struct PolicySettlementReferences;

pub async fn close_period(
    pool: &horse_postgres_common::PgPool,
    household_id: &str,
    actor_id: &str,
    period: &str,
    expected_revision: u64,
) -> Result<
    bookkeeping_postgres::close_period::PersistedClosePeriod,
    bookkeeping_application::workflow_failure::WorkflowFailure<
        bookkeeping_domain::close_period::ClosePeriodError,
        bookkeeping_postgres::close_period::ClosePeriodPersistenceError,
    >,
> {
    bookkeeping_postgres::close_period::close_period(
        pool,
        household_id,
        actor_id,
        period,
        expected_revision,
        &mut PolicySettlementReferences,
    )
    .await
}

pub async fn get_many(
    pool: &horse_postgres_common::PgPool,
    queries: &[bookkeeping_application::settlement_query::GetSettlementQuery],
) -> Result<
    Vec<bookkeeping_application::settlement_query::SettlementQueryResult>,
    bookkeeping_postgres::settlement_query::SettlementQueryPersistenceError,
> {
    bookkeeping_postgres::settlement_query::get_many(pool, queries, &mut PolicySettlementReferences)
        .await
}

pub async fn calculate_preview(
    pool: &horse_postgres_common::PgPool,
    household_id: &str,
    period: &str,
) -> Result<
    Vec<bookkeeping_application::settlement_preview::PreviewAmount>,
    bookkeeping_postgres::settlement_preview::SettlementPreviewPersistenceError,
> {
    bookkeeping_postgres::settlement_preview::calculate_preview(
        pool,
        household_id,
        period,
        &mut PolicySettlementReferences,
    )
    .await
}

#[async_trait::async_trait]
impl bookkeeping_postgres::settlement_preview::PolicySettlementData for PolicySettlementReferences {
    async fn household_members_for_settlement(
        &mut self,
        conn: &mut horse_postgres_common::AsyncPgConnection,
        household_id: uuid::Uuid,
    ) -> Result<
        Vec<(uuid::Uuid, String, Option<chrono::DateTime<chrono::Utc>>)>,
        diesel::result::Error,
    > {
        crate::household_identity_query::members_for_settlement(conn, household_id).await
    }

    async fn household_members_present_after(
        &mut self,
        conn: &mut horse_postgres_common::AsyncPgConnection,
        household_id: uuid::Uuid,
        period_start_utc: chrono::DateTime<chrono::Utc>,
    ) -> Result<Vec<(uuid::Uuid, String)>, diesel::result::Error> {
        crate::household_identity_query::members_present_after(conn, household_id, period_start_utc)
            .await
    }

    async fn schedules_for_period(
        &mut self,
        conn: &mut AsyncPgConnection,
        household_id: Uuid,
        start: NaiveDate,
        end: NaiveDate,
    ) -> Result<Vec<FundingScheduleRow>, diesel::result::Error> {
        funding_schedules_for_period(conn, household_id, start, end).await
    }

    async fn schedules_overlapping(
        &mut self,
        conn: &mut AsyncPgConnection,
        household_ids: &[Uuid],
        start: NaiveDate,
        end: NaiveDate,
    ) -> Result<
        Vec<bookkeeping_postgres::settlement_preview::HouseholdFundingScheduleRow>,
        diesel::result::Error,
    > {
        funding_schedules_overlapping(conn, household_ids, start, end).await
    }

    async fn earliest_schedule_start(
        &mut self,
        conn: &mut AsyncPgConnection,
        household_id: Uuid,
    ) -> Result<Option<NaiveDate>, diesel::result::Error> {
        use diesel::dsl::min;
        use horse_postgres_common::schema::public::funding_schedules as schedules;
        schedules::table
            .filter(schedules::household_id.eq(household_id))
            .select(min(schedules::starts_on))
            .first(conn)
            .await
    }

    async fn rules_and_shares(
        &mut self,
        conn: &mut AsyncPgConnection,
        household_id: Uuid,
    ) -> Result<(Vec<FundingRuleRow>, Vec<FundingShareRow>), diesel::result::Error> {
        current_funding_rules_and_shares(conn, household_id).await
    }

    async fn historical_rules(
        &mut self,
        conn: &mut AsyncPgConnection,
        household_id: Uuid,
        pool_ids: &[Uuid],
        versions: &[i32],
    ) -> Result<Vec<FundingRuleRow>, diesel::result::Error> {
        historical_funding_rules(conn, household_id, pool_ids, versions).await
    }

    async fn rule_owner(
        &mut self,
        conn: &mut AsyncPgConnection,
        household_id: Uuid,
        pool_id: Uuid,
        version: i32,
    ) -> Result<Option<Uuid>, diesel::result::Error> {
        funding_rule_owner(conn, household_id, pool_id, version).await
    }
}

pub async fn funding_schedules_for_period(
    conn: &mut AsyncPgConnection,
    household_id: Uuid,
    start: NaiveDate,
    end: NaiveDate,
) -> Result<Vec<FundingScheduleRow>, diesel::result::Error> {
    use horse_postgres_common::schema::public::funding_schedules as schedules;
    schedules::table
        .filter(schedules::household_id.eq(household_id))
        .filter(schedules::starts_on.lt(end))
        .filter(
            schedules::ends_on
                .is_null()
                .or(schedules::ends_on.ge(start)),
        )
        .filter(
            schedules::active
                .eq(true)
                .or(schedules::ends_on.is_not_null()),
        )
        .order((
            schedules::member_id,
            schedules::pool_id,
            schedules::starts_on,
            schedules::created_at,
            schedules::id,
        ))
        .select((
            schedules::id,
            schedules::member_id,
            schedules::pool_id,
            schedules::amount_yen,
            schedules::cadence,
            schedules::starts_on,
            schedules::ends_on,
            schedules::active,
            schedules::created_at,
        ))
        .load(conn)
        .await
}

pub async fn funding_schedules_overlapping(
    conn: &mut AsyncPgConnection,
    household_ids: &[Uuid],
    start: NaiveDate,
    end: NaiveDate,
) -> Result<
    Vec<(
        Uuid,
        Uuid,
        Uuid,
        Uuid,
        i64,
        String,
        NaiveDate,
        Option<NaiveDate>,
        bool,
    )>,
    diesel::result::Error,
> {
    use horse_postgres_common::schema::public::funding_schedules as schedules;
    schedules::table
        .filter(schedules::household_id.eq_any(household_ids))
        .filter(schedules::starts_on.lt(end))
        .filter(
            schedules::ends_on
                .is_null()
                .or(schedules::ends_on.ge(start)),
        )
        .filter(
            schedules::active
                .eq(true)
                .or(schedules::ends_on.is_not_null()),
        )
        .select((
            schedules::household_id,
            schedules::id,
            schedules::member_id,
            schedules::pool_id,
            schedules::amount_yen,
            schedules::cadence,
            schedules::starts_on,
            schedules::ends_on,
            schedules::active,
        ))
        .load(conn)
        .await
}

pub async fn current_funding_rules_and_shares(
    conn: &mut AsyncPgConnection,
    household_id: Uuid,
) -> Result<(Vec<FundingRuleRow>, Vec<FundingShareRow>), diesel::result::Error> {
    use horse_postgres_common::schema::public::{pool_funding_rules, pool_funding_shares};
    let rules = pool_funding_rules::table
        .filter(pool_funding_rules::household_id.eq(household_id))
        .select((
            pool_funding_rules::pool_id,
            pool_funding_rules::version,
            pool_funding_rules::owner_member_id,
            pool_funding_rules::settlement_policy,
        ))
        .load(conn)
        .await?;
    let shares = pool_funding_shares::table
        .filter(pool_funding_shares::household_id.eq(household_id))
        .order_by((
            pool_funding_shares::pool_id.asc(),
            pool_funding_shares::version.asc(),
            pool_funding_shares::member_id.asc(),
        ))
        .select((
            pool_funding_shares::pool_id,
            pool_funding_shares::version,
            pool_funding_shares::member_id,
            pool_funding_shares::weight,
        ))
        .load(conn)
        .await?;
    Ok((rules, shares))
}

pub async fn historical_funding_rules(
    conn: &mut AsyncPgConnection,
    household_id: Uuid,
    pool_ids: &[Uuid],
    versions: &[i32],
) -> Result<Vec<FundingRuleRow>, diesel::result::Error> {
    use horse_postgres_common::schema::public::pool_funding_rules;
    pool_funding_rules::table
        .filter(pool_funding_rules::household_id.eq(household_id))
        .filter(pool_funding_rules::pool_id.eq_any(pool_ids))
        .filter(pool_funding_rules::version.eq_any(versions))
        .select((
            pool_funding_rules::pool_id,
            pool_funding_rules::version,
            pool_funding_rules::owner_member_id,
            pool_funding_rules::settlement_policy,
        ))
        .load(conn)
        .await
}

pub async fn funding_rule_owner(
    conn: &mut AsyncPgConnection,
    household_id: Uuid,
    pool_id: Uuid,
    version: i32,
) -> Result<Option<Uuid>, diesel::result::Error> {
    use horse_postgres_common::schema::public::pool_funding_rules as rules;
    rules::table
        .filter(rules::household_id.eq(household_id))
        .filter(rules::pool_id.eq(pool_id))
        .filter(rules::version.eq(version))
        .select(rules::owner_member_id)
        .first(conn)
        .await
}
