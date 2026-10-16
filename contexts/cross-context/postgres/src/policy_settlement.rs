use chrono::{NaiveDate, Utc};
use diesel_async::AsyncPgConnection;
use policy_application::settlement_data::SettlementDataRead;
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
    chrono::DateTime<Utc>,
);
pub type FundingRuleRow = (Uuid, i32, Option<Uuid>, Option<String>);
pub type FundingShareRow = (Uuid, i32, Uuid, i64);

pub struct PolicySettlementReferences {
    reader: policy_postgres::settlement_data::PostgresSettlementDataRead,
}

impl PolicySettlementReferences {
    pub fn new() -> Self {
        Self {
            reader: policy_postgres::settlement_data::PostgresSettlementDataRead::new(),
        }
    }
}

impl Default for PolicySettlementReferences {
    fn default() -> Self {
        Self::new()
    }
}

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
        &mut PolicySettlementReferences::new(),
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
    bookkeeping_postgres::settlement_query::get_many(
        pool,
        queries,
        &mut PolicySettlementReferences::new(),
    )
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
        &mut PolicySettlementReferences::new(),
    )
    .await
}

#[async_trait::async_trait]
impl bookkeeping_postgres::settlement_preview::PolicySettlementData for PolicySettlementReferences {
    async fn household_members_for_settlement(
        &mut self,
        conn: &mut horse_postgres_common::AsyncPgConnection,
        household_id: Uuid,
    ) -> Result<Vec<(Uuid, String, Option<chrono::DateTime<Utc>>)>, diesel::result::Error> {
        crate::household_identity_query::members_for_settlement(conn, household_id).await
    }

    async fn household_members_present_after(
        &mut self,
        conn: &mut horse_postgres_common::AsyncPgConnection,
        household_id: Uuid,
        period_start_utc: chrono::DateTime<Utc>,
    ) -> Result<Vec<(Uuid, String)>, diesel::result::Error> {
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
        self.reader
            .schedules_for_period(conn, household_id, start, end)
            .await
            .map(|rows| rows.into_iter().map(schedule_row).collect())
            .map_err(policy_read_error)
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
        self.reader
            .schedules_overlapping(conn, household_ids, start, end)
            .await
            .map(|rows| {
                rows.into_iter()
                    .map(|row| {
                        (
                            row.household_id,
                            row.id,
                            row.member_id,
                            row.pool_id,
                            row.amount_yen,
                            row.cadence,
                            row.starts_on,
                            row.ends_on,
                            row.active,
                        )
                    })
                    .collect()
            })
            .map_err(policy_read_error)
    }

    async fn earliest_schedule_start(
        &mut self,
        conn: &mut AsyncPgConnection,
        household_id: Uuid,
    ) -> Result<Option<NaiveDate>, diesel::result::Error> {
        self.reader
            .earliest_schedule_start(conn, household_id)
            .await
            .map_err(policy_read_error)
    }

    async fn rules_and_shares(
        &mut self,
        conn: &mut AsyncPgConnection,
        household_id: Uuid,
    ) -> Result<(Vec<FundingRuleRow>, Vec<FundingShareRow>), diesel::result::Error> {
        self.reader
            .rules_and_shares(conn, household_id)
            .await
            .map(|value| {
                (
                    value
                        .rules
                        .into_iter()
                        .map(|row| {
                            (
                                row.pool_id,
                                row.version,
                                row.owner_member_id,
                                row.settlement_policy,
                            )
                        })
                        .collect(),
                    value
                        .shares
                        .into_iter()
                        .map(|row| (row.pool_id, row.version, row.member_id, row.weight))
                        .collect(),
                )
            })
            .map_err(policy_read_error)
    }

    async fn historical_rules(
        &mut self,
        conn: &mut AsyncPgConnection,
        household_id: Uuid,
        pool_ids: &[Uuid],
        versions: &[i32],
    ) -> Result<Vec<FundingRuleRow>, diesel::result::Error> {
        self.reader
            .historical_rules(conn, household_id, pool_ids, versions)
            .await
            .map(|rows| {
                rows.into_iter()
                    .map(|row| {
                        (
                            row.pool_id,
                            row.version,
                            row.owner_member_id,
                            row.settlement_policy,
                        )
                    })
                    .collect()
            })
            .map_err(policy_read_error)
    }

    async fn rule_owner(
        &mut self,
        conn: &mut AsyncPgConnection,
        household_id: Uuid,
        pool_id: Uuid,
        version: i32,
    ) -> Result<Option<Uuid>, diesel::result::Error> {
        self.reader
            .rule_owner(conn, household_id, pool_id, version)
            .await
            .map_err(policy_read_error)
    }
}

fn schedule_row(row: policy_application::settlement_data::FundingSchedule) -> FundingScheduleRow {
    (
        row.id,
        row.member_id,
        row.pool_id,
        row.amount_yen,
        row.cadence,
        row.starts_on,
        row.ends_on,
        row.active,
        row.created_at,
    )
}

fn policy_read_error(error: String) -> diesel::result::Error {
    diesel::result::Error::SerializationError(Box::new(std::io::Error::other(error)))
}
