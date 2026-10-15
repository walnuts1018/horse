use async_graphql::{Error, Result};
use chrono::{DateTime, Datelike, Duration, NaiveDate, Utc};
use diesel::{ExpressionMethods, QueryDsl};
use diesel_async::RunQueryDsl;
use horse_postgres_common::{PgPool, schema::public};
use std::collections::HashMap;
use uuid::Uuid;

use crate::{
    ledger_entry::{LoanBalance, Node},
    policy_mutations::{BudgetObject, Date},
    pool_mutations::{FundingScheduleCadence, FundingScheduleObject},
};

pub(crate) async fn load_funding_schedules(
    pool: &PgPool,
    household_ids: &[Uuid],
    ids: &[Uuid],
) -> Result<HashMap<Uuid, Node>> {
    let mut result = HashMap::with_capacity(ids.len());
    if household_ids.is_empty() || ids.is_empty() {
        return Ok(result);
    }

    use public::funding_schedules::dsl as schedules;
    let mut conn = pool.get().await.map_err(crate::internal_error)?;
    let rows = schedules::funding_schedules
        .filter(schedules::household_id.eq_any(household_ids))
        .filter(schedules::id.eq_any(ids))
        .select((
            schedules::id,
            schedules::household_id,
            schedules::member_id,
            schedules::pool_id,
            schedules::amount_yen,
            schedules::cadence,
            schedules::starts_on,
            schedules::ends_on,
            schedules::active,
            schedules::created_at,
        ))
        .load::<(
            Uuid,
            Uuid,
            Uuid,
            Uuid,
            i64,
            String,
            NaiveDate,
            Option<NaiveDate>,
            bool,
            DateTime<Utc>,
        )>(&mut conn)
        .await
        .map_err(crate::internal_error)?;

    for (
        id,
        household_id,
        member_id,
        pool_id,
        amount_yen,
        cadence,
        starts_on,
        ends_on,
        active,
        created_at,
    ) in rows
    {
        let cadence = match cadence.as_str() {
            "MONTHLY" => FundingScheduleCadence::Monthly,
            value => {
                return Err(crate::internal_error(format!(
                    "invalid funding schedule cadence {value}"
                )));
            }
        };
        let value = FundingScheduleObject {
            id: crate::node_query::encode_global_node_id("FundingSchedule", id),
            household_id: crate::node_query::encode_global_node_id("Household", household_id),
            member_id: crate::node_query::encode_global_node_id("Member", member_id),
            pool_id: crate::node_query::encode_global_node_id("Pool", pool_id),
            amount_yen: crate::Long(amount_yen),
            cadence,
            starts_on: Date(starts_on),
            ends_on: ends_on.map(Date),
            active,
            created_at: created_at.into(),
        };
        result.insert(id, Node::FundingSchedule(value));
    }
    Ok(result)
}

pub(crate) async fn load_budgets(
    pool: &PgPool,
    household_ids: &[Uuid],
    ids: &[Uuid],
) -> Result<HashMap<Uuid, Node>> {
    let mut result = HashMap::with_capacity(ids.len());
    if household_ids.is_empty() || ids.is_empty() {
        return Ok(result);
    }

    use public::budgets::dsl as budgets;
    let mut conn = pool.get().await.map_err(crate::internal_error)?;
    let rows = budgets::budgets
        .filter(budgets::household_id.eq_any(household_ids))
        .filter(budgets::id.eq_any(ids))
        .select((
            budgets::id,
            budgets::household_id,
            budgets::category_id,
            budgets::name,
            budgets::amount_yen,
            budgets::starts_on,
            budgets::ends_on,
            budgets::archived,
            budgets::revision,
        ))
        .load::<(
            Uuid,
            Uuid,
            Option<Uuid>,
            String,
            i64,
            NaiveDate,
            Option<NaiveDate>,
            bool,
            i64,
        )>(&mut conn)
        .await
        .map_err(crate::internal_error)?;

    let start = Utc::now();
    let now = start.date_naive();
    let next_month = if now.month() == 12 {
        NaiveDate::from_ymd_opt(now.year() + 1, 1, 1)
    } else {
        NaiveDate::from_ymd_opt(now.year(), now.month() + 1, 1)
    }
    .ok_or_else(|| crate::internal_error("invalid next month"))?;
    let end_date = next_month
        .checked_add_signed(Duration::days(i64::from(now.day() - 1)))
        .ok_or_else(|| crate::internal_error("invalid next month date"))?;
    let end = DateTime::<Utc>::from_naive_utc_and_offset(end_date.and_time(start.time()), Utc);

    use public::purchases::dsl as purchases;
    let purchase_rows = purchases::purchases
        .filter(purchases::household_id.eq_any(household_ids))
        .filter(purchases::occurred_at.ge(start))
        .filter(purchases::occurred_at.lt(end))
        .filter(purchases::status.eq_any(["POSTED", "CLOSED"]))
        .select((purchases::id, purchases::household_id))
        .load::<(Uuid, Uuid)>(&mut conn)
        .await
        .map_err(crate::internal_error)?;

    let mut actual_by_scope: HashMap<(Uuid, Option<Uuid>), i64> = HashMap::new();
    if !purchase_rows.is_empty() {
        let purchase_ids = purchase_rows.iter().map(|(id, _)| *id).collect::<Vec<_>>();
        let household_by_purchase = purchase_rows.into_iter().collect::<HashMap<_, _>>();
        use public::purchase_items::dsl as items;
        let item_rows = items::purchase_items
            .filter(items::purchase_id.eq_any(&purchase_ids))
            .select((
                items::id,
                items::purchase_id,
                items::category_id,
                items::amount_yen,
            ))
            .load::<(Uuid, Uuid, Option<Uuid>, i64)>(&mut conn)
            .await
            .map_err(crate::internal_error)?;
        let mut category_by_item = HashMap::with_capacity(item_rows.len());
        for (item_id, purchase_id, category_id, amount_yen) in item_rows {
            category_by_item.insert(item_id, category_id);
            let household_id = household_by_purchase[&purchase_id];
            add_budget_actual(&mut actual_by_scope, household_id, category_id, amount_yen)?;
        }

        use public::purchase_corrections::dsl as corrections;
        let correction_rows = corrections::purchase_corrections
            .filter(corrections::purchase_id.eq_any(&purchase_ids))
            .select((
                corrections::purchase_id,
                corrections::item_id,
                corrections::amount_delta_yen,
            ))
            .load::<(Uuid, Option<Uuid>, i64)>(&mut conn)
            .await
            .map_err(crate::internal_error)?;
        for (purchase_id, item_id, amount_delta_yen) in correction_rows {
            let category_id =
                item_id.and_then(|item_id| category_by_item.get(&item_id).copied().flatten());
            let household_id = household_by_purchase[&purchase_id];
            add_budget_actual(
                &mut actual_by_scope,
                household_id,
                category_id,
                amount_delta_yen,
            )?;
        }
    }

    for (id, household_id, category_id, name, amount_yen, starts_on, ends_on, archived, revision) in
        rows
    {
        let actual_yen = actual_by_scope
            .get(&(household_id, category_id))
            .copied()
            .unwrap_or_default();
        let revision = i32::try_from(revision).map_err(crate::internal_error)?;
        let value = BudgetObject {
            id: crate::node_query::encode_global_node_id("Budget", id),
            household_id: crate::node_query::encode_global_node_id("Household", household_id),
            category_id: category_id.map(|category_id| {
                crate::node_query::encode_global_node_id("Category", category_id)
            }),
            name,
            amount_yen: crate::Long(amount_yen),
            actual_yen: crate::Long(actual_yen),
            remaining_yen: crate::Long(amount_yen - actual_yen),
            starts_on: Date(starts_on),
            ends_on: ends_on.map(Date),
            archived,
            revision,
        };
        result.insert(id, Node::Budget(value));
    }
    Ok(result)
}

fn add_budget_actual(
    actual_by_scope: &mut HashMap<(Uuid, Option<Uuid>), i64>,
    household_id: Uuid,
    category_id: Option<Uuid>,
    amount_yen: i64,
) -> Result<()> {
    let global_total = actual_by_scope
        .get(&(household_id, None))
        .copied()
        .unwrap_or_default()
        .checked_add(amount_yen)
        .ok_or_else(|| Error::new("budget actual exceeds int64"))?;
    actual_by_scope.insert((household_id, None), global_total);
    if let Some(category_id) = category_id {
        let category_total = actual_by_scope
            .get(&(household_id, Some(category_id)))
            .copied()
            .unwrap_or_default()
            .checked_add(amount_yen)
            .ok_or_else(|| Error::new("budget actual exceeds int64"))?;
        actual_by_scope.insert((household_id, Some(category_id)), category_total);
    }
    Ok(())
}

pub(crate) async fn load_loan_balances(
    pool: &PgPool,
    household_ids: &[Uuid],
    ids: &[Uuid],
) -> Result<HashMap<Uuid, Node>> {
    let mut result = HashMap::with_capacity(ids.len());
    if household_ids.is_empty() || ids.is_empty() {
        return Ok(result);
    }

    use public::transfers::dsl as transfers;
    let mut conn = pool.get().await.map_err(crate::internal_error)?;
    let loans = transfers::transfers
        .filter(transfers::household_id.eq_any(household_ids))
        .filter(transfers::id.eq_any(ids))
        .filter(transfers::kind.eq("LOAN"))
        .select((
            transfers::id,
            transfers::household_id,
            transfers::journal_id,
            transfers::from_member_id,
            transfers::to_member_id,
            transfers::amount_yen,
        ))
        .load::<(Uuid, Uuid, Uuid, Uuid, Uuid, i64)>(&mut conn)
        .await
        .map_err(crate::internal_error)?;
    if loans.is_empty() {
        return Ok(result);
    }

    use public::ledger_entries::dsl as ledger_entries;
    let ledger_ids = loans
        .iter()
        .map(|(_, _, journal_id, _, _, _)| *journal_id)
        .collect::<Vec<_>>();
    let ledgers = ledger_entries::ledger_entries
        .filter(ledger_entries::household_id.eq_any(household_ids))
        .filter(ledger_entries::journal_id.eq_any(&ledger_ids))
        .select((ledger_entries::journal_id, ledger_entries::created_at))
        .load::<(Uuid, DateTime<Utc>)>(&mut conn)
        .await
        .map_err(crate::internal_error)?;
    let created_at_by_journal = ledgers.into_iter().collect::<HashMap<_, _>>();

    let loan_ids = loans.iter().map(|(id, ..)| Some(*id)).collect::<Vec<_>>();
    let repayments = transfers::transfers
        .filter(transfers::household_id.eq_any(household_ids))
        .filter(transfers::loan_id.eq_any(&loan_ids))
        .filter(transfers::kind.eq("REPAYMENT"))
        .select((transfers::loan_id, transfers::amount_yen))
        .load::<(Option<Uuid>, i64)>(&mut conn)
        .await
        .map_err(crate::internal_error)?;
    let mut repaid_by_id = HashMap::with_capacity(loans.len());
    for (loan_id, amount_yen) in repayments {
        if let Some(loan_id) = loan_id {
            let total = repaid_by_id
                .get(&loan_id)
                .copied()
                .unwrap_or_default()
                .checked_add(amount_yen)
                .ok_or_else(|| Error::new(format!("loan {loan_id} repayment total overflow")))?;
            repaid_by_id.insert(loan_id, total);
        }
    }

    for (id, _, journal_id, from_member_id, to_member_id, principal_yen) in loans {
        let created_at = created_at_by_journal
            .get(&journal_id)
            .ok_or_else(|| crate::internal_error(format!("loan {id} has no ledger entry")))?;
        let repaid_yen = repaid_by_id.get(&id).copied().unwrap_or_default();
        let value = LoanBalance {
            id: crate::node_query::encode_global_node_id("LoanBalance", id),
            lender_member_id: crate::node_query::encode_global_node_id("Member", from_member_id),
            borrower_member_id: crate::node_query::encode_global_node_id("Member", to_member_id),
            principal_yen: crate::Long(principal_yen),
            repaid_yen: crate::Long(repaid_yen),
            balance_yen: crate::Long(principal_yen - repaid_yen),
            created_at: (*created_at).into(),
        };
        result.insert(id, Node::LoanBalance(value));
    }
    Ok(result)
}
