use std::collections::HashMap;

use async_trait::async_trait;
use cross_context_application::node_financial_batch::{
    BudgetProjection, FundingScheduleProjection, LoanBalanceProjection, NodeFinancialBatchInput,
    NodeFinancialBatchProjection, NodeFinancialBatchQuery,
};
use diesel::{ExpressionMethods, QueryDsl};
use diesel_async::RunQueryDsl;
use policy_application::settlement_data::SettlementDataRead;
use uuid::Uuid;

use crate::{PgPool, schema::public};

pub struct PostgresNodeFinancialBatchQuery {
    pool: PgPool,
}

impl PostgresNodeFinancialBatchQuery {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait]
impl NodeFinancialBatchQuery for PostgresNodeFinancialBatchQuery {
    async fn load(
        &self,
        input: NodeFinancialBatchInput,
    ) -> Result<NodeFinancialBatchProjection, String> {
        let mut result = NodeFinancialBatchProjection::default();
        if input.household_ids.is_empty() {
            return Ok(result);
        }
        let mut conn = self
            .pool
            .get()
            .await
            .map_err(|error| format!("{error:?}"))?;

        if !input.funding_schedule_ids.is_empty() {
            let schedules = policy_postgres::settlement_data::PostgresSettlementDataRead::new()
                .schedules_by_ids(
                    &mut *conn,
                    &input.household_ids,
                    &input.funding_schedule_ids,
                )
                .await?;
            result.funding_schedules = schedules
                .into_iter()
                .map(|schedule| FundingScheduleProjection {
                    id: schedule.id,
                    household_id: schedule.household_id,
                    member_id: schedule.member_id,
                    pool_id: schedule.pool_id,
                    amount_yen: schedule.amount_yen,
                    cadence: schedule.cadence,
                    starts_on: schedule.starts_on,
                    ends_on: schedule.ends_on,
                    active: schedule.active,
                    created_at: schedule.created_at,
                })
                .collect();
        }

        if !input.budget_ids.is_empty() {
            let budgets = public::budgets::table
                .filter(public::budgets::household_id.eq_any(&input.household_ids))
                .filter(public::budgets::id.eq_any(&input.budget_ids))
                .select((
                    public::budgets::id,
                    public::budgets::household_id,
                    public::budgets::category_id,
                    public::budgets::name,
                    public::budgets::amount_yen,
                    public::budgets::starts_on,
                    public::budgets::ends_on,
                    public::budgets::archived,
                    public::budgets::revision,
                ))
                .load::<(
                    Uuid,
                    Uuid,
                    Option<Uuid>,
                    String,
                    i64,
                    chrono::NaiveDate,
                    Option<chrono::NaiveDate>,
                    bool,
                    i64,
                )>(&mut conn)
                .await
                .map_err(|error| format!("{error:?}"))?;

            let mut actual_by_scope: HashMap<(Uuid, Option<Uuid>), i64> = HashMap::new();
            let purchase_rows = public::purchases::table
                .filter(public::purchases::household_id.eq_any(&input.household_ids))
                .filter(public::purchases::occurred_at.ge(input.period_start))
                .filter(public::purchases::occurred_at.lt(input.period_end))
                .filter(public::purchases::status.eq_any(["POSTED", "CLOSED"]))
                .select((public::purchases::id, public::purchases::household_id))
                .load::<(Uuid, Uuid)>(&mut conn)
                .await
                .map_err(|error| format!("{error:?}"))?;

            if !purchase_rows.is_empty() {
                let purchase_ids = purchase_rows.iter().map(|(id, _)| *id).collect::<Vec<_>>();
                let household_by_purchase = purchase_rows.into_iter().collect::<HashMap<_, _>>();
                let item_rows = public::purchase_items::table
                    .filter(public::purchase_items::purchase_id.eq_any(&purchase_ids))
                    .select((
                        public::purchase_items::id,
                        public::purchase_items::purchase_id,
                        public::purchase_items::category_id,
                        public::purchase_items::amount_yen,
                    ))
                    .load::<(Uuid, Uuid, Option<Uuid>, i64)>(&mut conn)
                    .await
                    .map_err(|error| format!("{error:?}"))?;
                let mut category_by_item = HashMap::with_capacity(item_rows.len());
                for (item_id, purchase_id, category_id, amount_yen) in item_rows {
                    category_by_item.insert(item_id, category_id);
                    let household_id =
                        household_by_purchase.get(&purchase_id).ok_or_else(|| {
                            format!("purchase {purchase_id} disappeared during query")
                        })?;
                    add_budget_actual(
                        &mut actual_by_scope,
                        *household_id,
                        category_id,
                        amount_yen,
                    )?;
                }

                let correction_rows = public::purchase_corrections::table
                    .filter(public::purchase_corrections::purchase_id.eq_any(&purchase_ids))
                    .select((
                        public::purchase_corrections::purchase_id,
                        public::purchase_corrections::item_id,
                        public::purchase_corrections::amount_delta_yen,
                    ))
                    .load::<(Uuid, Option<Uuid>, i64)>(&mut conn)
                    .await
                    .map_err(|error| format!("{error:?}"))?;
                for (purchase_id, item_id, amount_delta_yen) in correction_rows {
                    let category_id = item_id
                        .and_then(|item_id| category_by_item.get(&item_id).copied().flatten());
                    let household_id =
                        household_by_purchase.get(&purchase_id).ok_or_else(|| {
                            format!("purchase {purchase_id} disappeared during query")
                        })?;
                    add_budget_actual(
                        &mut actual_by_scope,
                        *household_id,
                        category_id,
                        amount_delta_yen,
                    )?;
                }
            }

            result.budgets = budgets
                .into_iter()
                .map(
                    |(
                        id,
                        household_id,
                        category_id,
                        name,
                        amount_yen,
                        starts_on,
                        ends_on,
                        archived,
                        revision,
                    )| {
                        let actual_yen = actual_by_scope
                            .get(&(household_id, category_id))
                            .copied()
                            .unwrap_or_default();
                        BudgetProjection {
                            id,
                            household_id,
                            category_id,
                            name,
                            amount_yen,
                            actual_yen,
                            starts_on,
                            ends_on,
                            archived,
                            revision,
                        }
                    },
                )
                .collect();
        }

        if !input.loan_balance_ids.is_empty() {
            let loans = public::transfers::table
                .filter(public::transfers::household_id.eq_any(&input.household_ids))
                .filter(public::transfers::id.eq_any(&input.loan_balance_ids))
                .filter(public::transfers::kind.eq("LOAN"))
                .select((
                    public::transfers::id,
                    public::transfers::household_id,
                    public::transfers::journal_id,
                    public::transfers::from_member_id,
                    public::transfers::to_member_id,
                    public::transfers::amount_yen,
                ))
                .load::<(Uuid, Uuid, Uuid, Uuid, Uuid, i64)>(&mut conn)
                .await
                .map_err(|error| format!("{error:?}"))?;
            if !loans.is_empty() {
                let journal_ids = loans
                    .iter()
                    .map(|(_, _, journal_id, ..)| *journal_id)
                    .collect::<Vec<_>>();
                let ledger_rows = public::ledger_entries::table
                    .filter(public::ledger_entries::household_id.eq_any(&input.household_ids))
                    .filter(public::ledger_entries::journal_id.eq_any(&journal_ids))
                    .select((
                        public::ledger_entries::journal_id,
                        public::ledger_entries::created_at,
                    ))
                    .load::<(Uuid, chrono::DateTime<chrono::Utc>)>(&mut conn)
                    .await
                    .map_err(|error| format!("{error:?}"))?;
                let created_at_by_journal = ledger_rows.into_iter().collect::<HashMap<_, _>>();
                let loan_ids = loans.iter().map(|(id, ..)| Some(*id)).collect::<Vec<_>>();
                let repayment_rows = public::transfers::table
                    .filter(public::transfers::household_id.eq_any(&input.household_ids))
                    .filter(public::transfers::loan_id.eq_any(&loan_ids))
                    .filter(public::transfers::kind.eq("REPAYMENT"))
                    .select((public::transfers::loan_id, public::transfers::amount_yen))
                    .load::<(Option<Uuid>, i64)>(&mut conn)
                    .await
                    .map_err(|error| format!("{error:?}"))?;
                let mut repaid_by_id: HashMap<Uuid, i64> = HashMap::with_capacity(loans.len());
                for (loan_id, amount_yen) in repayment_rows {
                    if let Some(loan_id) = loan_id {
                        let total = repaid_by_id
                            .get(&loan_id)
                            .copied()
                            .unwrap_or_default()
                            .checked_add(amount_yen)
                            .ok_or_else(|| format!("loan {loan_id} repayment total overflow"))?;
                        repaid_by_id.insert(loan_id, total);
                    }
                }
                for (id, household_id, journal_id, from_member_id, to_member_id, principal_yen) in
                    loans
                {
                    let created_at = created_at_by_journal
                        .get(&journal_id)
                        .ok_or_else(|| format!("loan {id} has no ledger entry"))?;
                    result.loan_balances.push(LoanBalanceProjection {
                        id,
                        household_id,
                        from_member_id,
                        to_member_id,
                        principal_yen,
                        repaid_yen: repaid_by_id.get(&id).copied().unwrap_or_default(),
                        created_at: *created_at,
                    });
                }
            }
        }

        if !input.ledger_entry_ids.is_empty() {
            result.funding_ledger_entries =
                crate::funding_command_result::load_funding_ledger_entry_results(
                    &self.pool,
                    &input.household_ids,
                    &input.ledger_entry_ids,
                )
                .await
                .map_err(|error| format!("{error:?}"))?
                .into_iter()
                .map(|(id, row)| (id, funding_projection(row)))
                .collect();
            result.transfer_ledger_entries =
                crate::financial_command_result::load_transfer_ledger_entry_results(
                    &self.pool,
                    &input.household_ids,
                    &input.ledger_entry_ids,
                )
                .await
                .map_err(|error| format!("{error:?}"))?
                .into_iter()
                .map(|(id, row)| (id, transfer_projection(row)))
                .collect();
        }

        if !input.transfer_ids.is_empty() {
            let mut transfers = HashMap::new();
            for household_id in &input.household_ids {
                for transfer_id in &input.transfer_ids {
                    match crate::node_query::load_transfer_ledger_entry_result_by_transfer_id(
                        &self.pool,
                        *household_id,
                        *transfer_id,
                    )
                    .await
                    {
                        Ok(row) => {
                            transfers.insert(*transfer_id, transfer_projection(row));
                        }
                        Err(crate::node_query::NodeQueryError::MissingLedgerEntry) => {}
                        Err(error) => return Err(format!("{error:?}")),
                    }
                }
            }
            result.transfers_by_id = transfers;
        }
        Ok(result)
    }
}

fn funding_projection(
    value: crate::funding_command_result::FundingLedgerEntryResult,
) -> cross_context_application::ledger_query::FundingLedgerEntryProjection {
    cross_context_application::ledger_query::FundingLedgerEntryProjection {
        id: value.id,
        household_id: value.household_id,
        occurred_at: value.occurred_at,
        description: value.description,
        amount_yen: value.amount_yen,
        currency: value.currency,
        created_at: value.created_at,
        member: cross_context_application::ledger_query::FundingMemberProjection {
            id: value.member.id,
            display_name: value.member.display_name,
            role: value.member.role,
        },
        pool: cross_context_application::ledger_query::FundingPoolProjection {
            id: value.pool.id,
            name: value.pool.name,
            kind: value.pool.kind,
            default_share_weight: value.pool.default_share_weight,
            funding_shares: value
                .pool
                .funding_shares
                .into_iter()
                .map(
                    |share| cross_context_application::ledger_query::FundingPoolShareProjection {
                        member_id: share.member_id,
                        member_name: share.member_name,
                        weight: share.weight,
                    },
                )
                .collect(),
            funding_rule_version: value.pool.funding_rule_version,
            owner_member_id: value.pool.owner_member_id,
            settlement_policy: value.pool.settlement_policy,
        },
    }
}

fn transfer_projection(
    value: crate::financial_command_result::TransferLedgerEntryResult,
) -> cross_context_application::statement_link_mutations::TransferLedgerEntryProjection {
    cross_context_application::statement_link_mutations::TransferLedgerEntryProjection {
        id: value.id,
        kind: value.kind,
        occurred_at: value.occurred_at,
        description: value.description,
        amount_yen: value.amount_yen,
        currency: value.currency,
        created_at: value.created_at,
        from_member:
            cross_context_application::statement_link_mutations::TransferMemberProjection {
                id: value.from_member.id,
                display_name: value.from_member.display_name,
                role: value.from_member.role,
            },
        to_member: cross_context_application::statement_link_mutations::TransferMemberProjection {
            id: value.to_member.id,
            display_name: value.to_member.display_name,
            role: value.to_member.role,
        },
        loan: value.loan.map(|loan| {
            cross_context_application::statement_link_mutations::LoanBalanceProjection {
                id: loan.id,
                lender_member_id: loan.lender_member_id,
                borrower_member_id: loan.borrower_member_id,
                principal_yen: loan.principal_yen,
                repaid_yen: loan.repaid_yen,
                balance_yen: loan.balance_yen,
                created_at: loan.created_at,
            }
        }),
    }
}

fn add_budget_actual(
    actual_by_scope: &mut HashMap<(Uuid, Option<Uuid>), i64>,
    household_id: Uuid,
    category_id: Option<Uuid>,
    amount_yen: i64,
) -> Result<(), String> {
    let global_total = actual_by_scope
        .get(&(household_id, None))
        .copied()
        .unwrap_or_default()
        .checked_add(amount_yen)
        .ok_or_else(|| "budget actual exceeds int64".to_owned())?;
    actual_by_scope.insert((household_id, None), global_total);
    if let Some(category_id) = category_id {
        let category_total = actual_by_scope
            .get(&(household_id, Some(category_id)))
            .copied()
            .unwrap_or_default()
            .checked_add(amount_yen)
            .ok_or_else(|| "budget actual exceeds int64".to_owned())?;
        actual_by_scope.insert((household_id, Some(category_id)), category_total);
    }
    Ok(())
}
