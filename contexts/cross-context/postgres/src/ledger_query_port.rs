use std::collections::HashMap;

use crate::PgPool;
use async_trait::async_trait;
use cross_context_application::ledger_query::{
    FundingLedgerEntryProjection, FundingMemberProjection, FundingPoolProjection,
    FundingPoolShareProjection, LedgerPage, LedgerQuery as ApplicationQuery,
    LedgerQueryError as ApplicationError, LedgerQueryHandler, LedgerRow,
};
use cross_context_application::statement_link_mutations::{
    LoanBalanceProjection, TransferLedgerEntryProjection, TransferMemberProjection,
};

pub struct PostgresLedgerQueryHandler {
    pool: PgPool,
}

impl PostgresLedgerQueryHandler {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait]
impl LedgerQueryHandler for PostgresLedgerQueryHandler {
    async fn query(&self, query: ApplicationQuery) -> Result<LedgerPage, ApplicationError> {
        let page = crate::ledger_query::ledger(
            &self.pool,
            crate::ledger_query::LedgerQuery {
                actor_id: query.actor_id,
                household_id: query.household_id,
                after: query
                    .after
                    .map(|cursor| crate::ledger_query::LedgerCursor { id: cursor.id }),
                before: query
                    .before
                    .map(|cursor| crate::ledger_query::LedgerCursor { id: cursor.id }),
                first: query.first,
                last: query.last,
            },
        )
        .await
        .map_err(application_error)?;

        let funding_ids = page
            .rows
            .iter()
            .filter(|row| matches!(row.source_kind.as_str(), "FUNDING" | "funding"))
            .map(|row| row.id)
            .collect::<Vec<_>>();
        let transfer_ids = page
            .rows
            .iter()
            .filter(|row| matches!(row.source_kind.as_str(), "TRANSFER" | "transfer"))
            .map(|row| row.id)
            .collect::<Vec<_>>();

        let funding_entries = crate::funding_command_result::load_funding_ledger_entry_results(
            &self.pool,
            &[query.household_id],
            &funding_ids,
        )
        .await
        .map_err(|error| ApplicationError::Infrastructure(format!("{error:?}")))?
        .into_iter()
        .map(|(id, value)| (id, funding_projection(value)))
        .collect::<HashMap<_, _>>();

        let transfer_entries = crate::financial_command_result::load_transfer_ledger_entry_results(
            &self.pool,
            &[query.household_id],
            &transfer_ids,
        )
        .await
        .map_err(|error| ApplicationError::Infrastructure(format!("{error:?}")))?
        .into_iter()
        .map(|(id, value)| (id, transfer_projection(value)))
        .collect::<HashMap<_, _>>();

        Ok(LedgerPage {
            rows: page
                .rows
                .into_iter()
                .map(|row| LedgerRow {
                    id: row.id,
                    source_kind: row.source_kind,
                    source_id: row.source_id,
                    source_purchase_id: row.source_purchase_id,
                    occurred_at: row.occurred_at,
                    description: row.description,
                    amount_yen: row.amount_yen,
                    created_at: row.created_at,
                })
                .collect(),
            total_count: page.total_count,
            has_extra: page.has_extra,
            backward: page.backward,
            after_has_previous_page: page.after_has_previous_page,
            before_has_next_page: page.before_has_next_page,
            funding_entries,
            transfer_entries,
        })
    }
}

fn application_error(error: crate::ledger_query::LedgerQueryError) -> ApplicationError {
    match error {
        crate::ledger_query::LedgerQueryError::HouseholdNotFound => {
            ApplicationError::HouseholdNotFound
        }
        crate::ledger_query::LedgerQueryError::InvalidPagination => {
            ApplicationError::InvalidPagination
        }
        crate::ledger_query::LedgerQueryError::MissingCursor => ApplicationError::MissingCursor,
        error => ApplicationError::Infrastructure(format!("{error:?}")),
    }
}

fn funding_projection(
    value: crate::funding_command_result::FundingLedgerEntryResult,
) -> FundingLedgerEntryProjection {
    FundingLedgerEntryProjection {
        id: value.id,
        household_id: value.household_id,
        occurred_at: value.occurred_at,
        description: value.description,
        amount_yen: value.amount_yen,
        currency: value.currency,
        created_at: value.created_at,
        member: FundingMemberProjection {
            id: value.member.id,
            display_name: value.member.display_name,
            role: value.member.role,
        },
        pool: FundingPoolProjection {
            id: value.pool.id,
            name: value.pool.name,
            kind: value.pool.kind,
            default_share_weight: value.pool.default_share_weight,
            funding_shares: value
                .pool
                .funding_shares
                .into_iter()
                .map(|share| FundingPoolShareProjection {
                    member_id: share.member_id,
                    member_name: share.member_name,
                    weight: share.weight,
                })
                .collect(),
            funding_rule_version: value.pool.funding_rule_version,
            owner_member_id: value.pool.owner_member_id,
            settlement_policy: value.pool.settlement_policy,
        },
    }
}

fn transfer_projection(
    value: crate::financial_command_result::TransferLedgerEntryResult,
) -> TransferLedgerEntryProjection {
    TransferLedgerEntryProjection {
        id: value.id,
        kind: value.kind,
        occurred_at: value.occurred_at,
        description: value.description,
        amount_yen: value.amount_yen,
        currency: value.currency,
        created_at: value.created_at,
        from_member: TransferMemberProjection {
            id: value.from_member.id,
            display_name: value.from_member.display_name,
            role: value.from_member.role,
        },
        to_member: TransferMemberProjection {
            id: value.to_member.id,
            display_name: value.to_member.display_name,
            role: value.to_member.role,
        },
        loan: value.loan.map(|loan| LoanBalanceProjection {
            id: loan.id,
            lender_member_id: loan.lender_member_id,
            borrower_member_id: loan.borrower_member_id,
            principal_yen: loan.principal_yen,
            repaid_yen: loan.repaid_yen,
            balance_yen: loan.balance_yen,
            created_at: loan.created_at,
        }),
    }
}
