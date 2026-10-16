use std::collections::HashMap;

use async_graphql::Result;
use chrono::{DateTime, Datelike, Duration, NaiveDate, Utc};
use cross_context_application::node_financial_batch::{
    NodeFinancialBatchInput, NodeFinancialBatchProjection, NodeFinancialBatchQuery,
};
use uuid::Uuid;

use crate::{
    ledger_entry::{LoanBalance, Node},
    policy_mutations::{BudgetObject, Date},
    pool_mutations::{FundingScheduleCadence, FundingScheduleObject},
};

pub(crate) async fn load_node_financial_batch(
    query: &dyn NodeFinancialBatchQuery,
    household_ids: &[Uuid],
    funding_schedule_ids: &[Uuid],
    budget_ids: &[Uuid],
    loan_balance_ids: &[Uuid],
) -> Result<HashMap<(String, Uuid), Node>> {
    let now = Utc::now();
    let next_month = if now.month() == 12 {
        NaiveDate::from_ymd_opt(now.year() + 1, 1, 1)
    } else {
        NaiveDate::from_ymd_opt(now.year(), now.month() + 1, 1)
    }
    .ok_or_else(|| crate::internal_error("invalid next month"))?;
    let end_date = next_month
        .checked_add_signed(Duration::days(i64::from(now.day() - 1)))
        .ok_or_else(|| crate::internal_error("invalid next month date"))?;
    let period_end = DateTime::<Utc>::from_naive_utc_and_offset(end_date.and_time(now.time()), Utc);
    let projection = query
        .load(NodeFinancialBatchInput {
            household_ids: household_ids.to_vec(),
            funding_schedule_ids: funding_schedule_ids.to_vec(),
            budget_ids: budget_ids.to_vec(),
            loan_balance_ids: loan_balance_ids.to_vec(),
            ledger_entry_ids: Vec::new(),
            transfer_ids: Vec::new(),
            period_start: now,
            period_end,
        })
        .await
        .map_err(crate::internal_error)?;
    map_projection(projection)
}

fn map_projection(
    projection: NodeFinancialBatchProjection,
) -> Result<HashMap<(String, Uuid), Node>> {
    let mut result = HashMap::with_capacity(
        projection.funding_schedules.len()
            + projection.budgets.len()
            + projection.loan_balances.len(),
    );
    for value in projection.funding_schedules {
        let cadence = match value.cadence.as_str() {
            "MONTHLY" => FundingScheduleCadence::Monthly,
            "WEEKLY" => FundingScheduleCadence::Weekly,
            "CUSTOM" => FundingScheduleCadence::Custom,
            invalid => {
                return Err(crate::internal_error(format!(
                    "invalid funding schedule cadence {invalid}"
                )));
            }
        };
        result.insert(
            ("FundingSchedule".to_owned(), value.id),
            Node::FundingSchedule(FundingScheduleObject {
                id: crate::node_query::encode_global_node_id("FundingSchedule", value.id),
                household_id: crate::node_query::encode_global_node_id(
                    "Household",
                    value.household_id,
                ),
                member_id: crate::node_query::encode_global_node_id("Member", value.member_id),
                pool_id: crate::node_query::encode_global_node_id("Pool", value.pool_id),
                amount_yen: crate::Long(value.amount_yen),
                cadence,
                starts_on: Date(value.starts_on),
                ends_on: value.ends_on.map(Date),
                active: value.active,
                created_at: value.created_at.into(),
            }),
        );
    }
    for value in projection.budgets {
        let remaining_yen = value
            .amount_yen
            .checked_sub(value.actual_yen)
            .ok_or_else(|| {
                crate::internal_error(format!(
                    "budget remaining overflow for budget {}: amount {}, actual {}",
                    value.id, value.amount_yen, value.actual_yen
                ))
            })?;
        let revision = i32::try_from(value.revision).map_err(crate::internal_error)?;
        result.insert(
            ("Budget".to_owned(), value.id),
            Node::Budget(BudgetObject {
                id: crate::node_query::encode_global_node_id("Budget", value.id),
                household_id: crate::node_query::encode_global_node_id(
                    "Household",
                    value.household_id,
                ),
                category_id: value.category_id.map(|category_id| {
                    crate::node_query::encode_global_node_id("Category", category_id)
                }),
                name: value.name,
                amount_yen: crate::Long(value.amount_yen),
                actual_yen: crate::Long(value.actual_yen),
                remaining_yen: crate::Long(remaining_yen),
                starts_on: Date(value.starts_on),
                ends_on: value.ends_on.map(Date),
                archived: value.archived,
                revision,
            }),
        );
    }
    for value in projection.loan_balances {
        let balance_yen = value
            .principal_yen
            .checked_sub(value.repaid_yen)
            .ok_or_else(|| {
                crate::internal_error(format!(
                    "loan balance overflow for loan {}: principal {}, repaid {}",
                    value.id, value.principal_yen, value.repaid_yen
                ))
            })?;
        result.insert(
            ("LoanBalance".to_owned(), value.id),
            Node::LoanBalance(LoanBalance {
                id: crate::node_query::encode_global_node_id("LoanBalance", value.id),
                lender_member_id: crate::node_query::encode_global_node_id(
                    "Member",
                    value.from_member_id,
                ),
                borrower_member_id: crate::node_query::encode_global_node_id(
                    "Member",
                    value.to_member_id,
                ),
                principal_yen: crate::Long(value.principal_yen),
                repaid_yen: crate::Long(value.repaid_yen),
                balance_yen: crate::Long(balance_yen),
                created_at: value.created_at.into(),
            }),
        );
    }
    Ok(result)
}
