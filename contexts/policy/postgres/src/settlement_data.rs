use chrono::NaiveDate;
use diesel::{BoolExpressionMethods, ExpressionMethods, OptionalExtension, QueryDsl, dsl::min};
use diesel_async::RunQueryDsl;
use policy_application::settlement_data::{
    FundingRule, FundingRulesAndShares, FundingSchedule, FundingShare, SettlementDataRead,
};
use uuid::Uuid;

use crate::{PgPool, schema::public};

type FundingScheduleRow = (
    Uuid,
    Uuid,
    Uuid,
    Uuid,
    i64,
    String,
    NaiveDate,
    Option<NaiveDate>,
    bool,
    chrono::DateTime<chrono::Utc>,
);
type FundingRuleRow = (Uuid, i32, Option<Uuid>, Option<String>);
type FundingShareRow = (Uuid, i32, Uuid, i64);

pub struct PostgresSettlementDataRead {
    pool: PgPool,
}

impl PostgresSettlementDataRead {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl SettlementDataRead for PostgresSettlementDataRead {
    async fn schedules_for_period(
        &self,
        household_id: Uuid,
        start: NaiveDate,
        end: NaiveDate,
    ) -> Result<Vec<FundingSchedule>, String> {
        use public::funding_schedules as schedules;
        let mut conn = self.pool.get().await.map_err(|error| error.to_string())?;
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
                schedules::household_id,
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
            .load::<FundingScheduleRow>(&mut *conn)
            .await
            .map(map_schedules)
            .map_err(|error| error.to_string())
    }

    async fn schedules_overlapping(
        &self,
        household_ids: &[Uuid],
        start: NaiveDate,
        end: NaiveDate,
    ) -> Result<Vec<FundingSchedule>, String> {
        use public::funding_schedules as schedules;
        let mut conn = self.pool.get().await.map_err(|error| error.to_string())?;
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
                schedules::created_at,
            ))
            .load::<FundingScheduleRow>(&mut *conn)
            .await
            .map(map_schedules)
            .map_err(|error| error.to_string())
    }

    async fn earliest_schedule_start(
        &self,
        household_id: Uuid,
    ) -> Result<Option<NaiveDate>, String> {
        use public::funding_schedules as schedules;
        let mut conn = self.pool.get().await.map_err(|error| error.to_string())?;
        schedules::table
            .filter(schedules::household_id.eq(household_id))
            .select(min(schedules::starts_on))
            .first(&mut *conn)
            .await
            .map_err(|error| error.to_string())
    }

    async fn rules_and_shares(&self, household_id: Uuid) -> Result<FundingRulesAndShares, String> {
        use public::{pool_funding_rules as rules, pool_funding_shares as shares};
        let mut conn = self.pool.get().await.map_err(|error| error.to_string())?;
        let rule_rows = rules::table
            .filter(rules::household_id.eq(household_id))
            .select((
                rules::pool_id,
                rules::version,
                rules::owner_member_id,
                rules::settlement_policy,
            ))
            .load::<FundingRuleRow>(&mut *conn)
            .await
            .map(|rows| rows.into_iter().map(map_rule).collect::<Vec<_>>())
            .map_err(|error| error.to_string())?;
        let share_rows = shares::table
            .filter(shares::household_id.eq(household_id))
            .order_by((
                shares::pool_id.asc(),
                shares::version.asc(),
                shares::member_id.asc(),
            ))
            .select((
                shares::pool_id,
                shares::version,
                shares::member_id,
                shares::weight,
            ))
            .load::<FundingShareRow>(&mut *conn)
            .await
            .map(|rows| rows.into_iter().map(map_share).collect::<Vec<_>>())
            .map_err(|error| error.to_string())?;
        Ok(FundingRulesAndShares {
            rules: rule_rows,
            shares: share_rows,
        })
    }

    async fn historical_rules(
        &self,
        household_id: Uuid,
        pool_ids: &[Uuid],
        versions: &[i32],
    ) -> Result<Vec<FundingRule>, String> {
        use public::pool_funding_rules as rules;
        let mut conn = self.pool.get().await.map_err(|error| error.to_string())?;
        rules::table
            .filter(rules::household_id.eq(household_id))
            .filter(rules::pool_id.eq_any(pool_ids))
            .filter(rules::version.eq_any(versions))
            .select((
                rules::pool_id,
                rules::version,
                rules::owner_member_id,
                rules::settlement_policy,
            ))
            .load::<FundingRuleRow>(&mut *conn)
            .await
            .map(|rows| rows.into_iter().map(map_rule).collect())
            .map_err(|error| error.to_string())
    }

    async fn pool_funding_owner(
        &self,
        household_id: Uuid,
        pool_id: Uuid,
        version: i32,
    ) -> Result<Option<Uuid>, String> {
        use public::pool_funding_rules as rules;
        let mut conn = self.pool.get().await.map_err(|error| error.to_string())?;
        rules::table
            .filter(rules::household_id.eq(household_id))
            .filter(rules::pool_id.eq(pool_id))
            .filter(rules::version.eq(version))
            .select(rules::owner_member_id)
            .first::<Option<Uuid>>(&mut *conn)
            .await
            .optional()
            .map(Option::flatten)
            .map_err(|error| error.to_string())
    }

    async fn active_funding_schedule(
        &self,
        household_id: Uuid,
        member_id: Uuid,
        pool_id: Uuid,
    ) -> Result<Option<Uuid>, String> {
        use public::funding_schedules as schedules;
        let mut conn = self.pool.get().await.map_err(|error| error.to_string())?;
        schedules::table
            .filter(schedules::household_id.eq(household_id))
            .filter(schedules::member_id.eq(member_id))
            .filter(schedules::pool_id.eq(pool_id))
            .filter(schedules::active.eq(true))
            .order(schedules::created_at.desc())
            .select(schedules::id)
            .first::<Uuid>(&mut *conn)
            .await
            .optional()
            .map_err(|error| error.to_string())
    }

    async fn rule_owner(
        &self,
        household_id: Uuid,
        pool_id: Uuid,
        version: i32,
    ) -> Result<Option<Uuid>, String> {
        use public::pool_funding_rules as rules;
        let mut conn = self.pool.get().await.map_err(|error| error.to_string())?;
        rules::table
            .filter(rules::household_id.eq(household_id))
            .filter(rules::pool_id.eq(pool_id))
            .filter(rules::version.eq(version))
            .select(rules::owner_member_id)
            .first::<Option<Uuid>>(&mut *conn)
            .await
            .optional()
            .map(Option::flatten)
            .map_err(|error| error.to_string())
    }
}

fn map_schedules(rows: Vec<FundingScheduleRow>) -> Vec<FundingSchedule> {
    rows.into_iter()
        .map(
            |(
                household_id,
                id,
                member_id,
                pool_id,
                amount_yen,
                cadence,
                starts_on,
                ends_on,
                active,
                created_at,
            )| FundingSchedule {
                household_id,
                id,
                member_id,
                pool_id,
                amount_yen,
                cadence,
                starts_on,
                ends_on,
                active,
                created_at,
            },
        )
        .collect()
}

fn map_rule((pool_id, version, owner_member_id, settlement_policy): FundingRuleRow) -> FundingRule {
    FundingRule {
        pool_id,
        version,
        owner_member_id,
        settlement_policy,
    }
}

fn map_share((pool_id, version, member_id, weight): FundingShareRow) -> FundingShare {
    FundingShare {
        pool_id,
        version,
        member_id,
        weight,
    }
}
