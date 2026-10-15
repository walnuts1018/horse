use async_graphql::{Context, Error, ID, Result, SimpleObject};
use horse_postgres::{PgPool, home_summary_query as postgres_query};

use crate::{Long, policy_mutations::Date};

#[derive(Clone, Debug, SimpleObject)]
pub struct HomeDailySpending {
    pub date: Date,
    pub amount_yen: Long,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct HomeSummary {
    pub daily_spending: Vec<HomeDailySpending>,
    pub monthly_item_count: Long,
}

pub async fn graphql_home_summary(ctx: &Context<'_>, household_id: ID) -> Result<HomeSummary> {
    let actor = crate::actor(ctx)?;
    let household_id = crate::decode_household_id(&household_id)?;
    let projection = postgres_query::home_summary(ctx.data::<PgPool>()?, actor.0, household_id)
        .await
        .map_err(map_query_error)?;
    Ok(HomeSummary {
        daily_spending: projection
            .daily_spending
            .into_iter()
            .map(|row| HomeDailySpending {
                date: Date(row.date),
                amount_yen: Long(row.amount_yen),
            })
            .collect(),
        monthly_item_count: Long(projection.monthly_item_count),
    })
}

fn map_query_error(error: postgres_query::HomeSummaryQueryError) -> Error {
    use postgres_query::HomeSummaryQueryError as E;
    match error {
        E::HouseholdNotFound => Error::new("household access denied"),
        E::AmountOutOfRange => Error::new("home summary total exceeds supported range"),
        E::Pool(_) | E::Database(_) => crate::internal_error(error),
    }
}
