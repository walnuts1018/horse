use crate::{Long, policy_mutations::YearMonth};
use async_graphql::{Context, Error, ID, Result, SimpleObject};
use bookkeeping_application::monthly_spending_query::{
    MonthlySpendingQuery, MonthlySpendingQueryError,
};

#[derive(Clone, Debug, SimpleObject)]
pub struct MonthlyCategorySpending {
    pub period: YearMonth,
    pub category_id: Option<ID>,
    pub category_name: Option<String>,
    pub amount_yen: Long,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct MonthlyMemberSpending {
    pub period: YearMonth,
    pub member_id: ID,
    pub member_name: String,
    pub amount_yen: Long,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct MonthlyMemberSpendingReport {
    pub items: Vec<MonthlyMemberSpending>,
    pub has_unattributed_purchases: bool,
}

pub async fn graphql_monthly_category_spending(
    ctx: &Context<'_>,
    household_id: ID,
    from: YearMonth,
    to: YearMonth,
) -> Result<Vec<MonthlyCategorySpending>> {
    let actor = crate::actor(ctx)?;
    let household_id = crate::decode_household_id(&household_id)?;
    let rows = ctx
        .data::<std::sync::Arc<dyn MonthlySpendingQuery>>()?
        .monthly_category_spending(actor.0, household_id, from.as_str(), to.as_str())
        .await
        .map_err(map_query_error)?;
    let mut rows = rows;
    rows.sort_by(|left, right| {
        right
            .period
            .cmp(&left.period)
            .then_with(|| left.category_id.is_none().cmp(&right.category_id.is_none()))
            .then_with(|| left.category_name.cmp(&right.category_name))
            .then_with(|| left.category_id.cmp(&right.category_id))
    });
    rows.into_iter()
        .map(|row| {
            Ok(MonthlyCategorySpending {
                period: YearMonth::from_naive_date(
                    chrono::NaiveDate::parse_from_str(&format!("{}-01", row.period), "%Y-%m-%d")
                        .map_err(crate::internal_error)?,
                ),
                category_id: row
                    .category_id
                    .map(|id| crate::catalog_mutations::encode_node_id("Category", &id.to_string()))
                    .transpose()
                    .map_err(|_| crate::internal_error("invalid category node ID"))?,
                category_name: row.category_name,
                amount_yen: Long(row.amount_yen),
            })
        })
        .collect()
}

pub async fn graphql_monthly_member_spending(
    ctx: &Context<'_>,
    household_id: ID,
    from: YearMonth,
    to: YearMonth,
) -> Result<MonthlyMemberSpendingReport> {
    let actor = crate::actor(ctx)?;
    let household_id = crate::decode_household_id(&household_id)?;
    let report = ctx
        .data::<std::sync::Arc<dyn MonthlySpendingQuery>>()?
        .monthly_member_spending(actor.0, household_id, from.as_str(), to.as_str())
        .await
        .map_err(map_query_error)?;
    let items = report
        .items
        .into_iter()
        .map(|row| {
            Ok(MonthlyMemberSpending {
                period: YearMonth::from_naive_date(
                    chrono::NaiveDate::parse_from_str(&format!("{}-01", row.period), "%Y-%m-%d")
                        .map_err(crate::internal_error)?,
                ),
                member_id: crate::catalog_mutations::encode_node_id(
                    "Member",
                    &row.member_id.to_string(),
                )
                .map_err(|_| crate::internal_error("invalid member node ID"))?,
                member_name: row.member_name,
                amount_yen: Long(row.amount_yen),
            })
        })
        .collect::<Result<Vec<_>>>()?;
    Ok(MonthlyMemberSpendingReport {
        items,
        has_unattributed_purchases: report.has_unattributed_purchases,
    })
}

fn map_query_error(error: MonthlySpendingQueryError) -> Error {
    use MonthlySpendingQueryError as E;
    match error {
        E::HouseholdNotFound => Error::new("household access denied"),
        E::InvalidPeriod => Error::new("invalid monthly spending period"),
        E::AmountOutOfRange => Error::new("monthly spending total exceeds supported range"),
        E::Infrastructure(_) => crate::internal_error(error),
    }
}
