use async_trait::async_trait;
use bookkeeping_application::{
    account_history_query::{
        AccountIncomeProjection, AccountMovementProjection, AccountProjection,
    },
    purchase_search_query::{
        PurchaseItemAllocationProjection, PurchaseItemProjection, PurchasePaymentProjection,
        PurchaseProjection,
    },
    workflow_failure::{WorkflowFailure, WorkflowRejection},
};
use cross_context_application::statement_link_mutations::{
    LoanBalanceProjection, StatementLinkCommand, StatementLinkMutationFailure,
    StatementLinkMutationsHandler, StatementRefundCorrectionLinkCommand,
    StatementTransferLinkCommand, TransferLedgerEntryProjection, TransferMemberProjection,
};
use horse_postgres_common::PgPool;
use uuid::Uuid;

pub struct PostgresStatementLinkMutations {
    pool: PgPool,
}

impl PostgresStatementLinkMutations {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait]
impl StatementLinkMutationsHandler for PostgresStatementLinkMutations {
    async fn link_movement(
        &self,
        command: StatementLinkCommand,
    ) -> Result<AccountMovementProjection, StatementLinkMutationFailure> {
        let source_account_id = command.source_account_id.ok_or_else(invalid_input)?;
        let decision = bookkeeping_domain::account_movement::LinkStatementMovementDecision {
            source_document_id: command.source_document_id.to_string(),
            source_reference: command.source_reference,
            statement_account_id: bookkeeping_application::AccountId::new(
                source_account_id.to_string(),
            )
            .map_err(|_| invalid_input())?,
            movement_id: command.target_id.to_string(),
        };
        crate::evidence_workflows::persist_link_statement_movement(
            &self.pool,
            bookkeeping_postgres::account_movement::PersistLinkStatementMovement {
                household_id: command.household_id.to_string(),
                actor_id: command.actor_id.to_string(),
                account_movement_id: command.target_id.to_string(),
                statement_account_id: source_account_id.to_string(),
            },
            decision,
        )
        .await
        .map_err(map_workflow)?;
        let result =
            bookkeeping_postgres::account_command_result::load_account_movement_command_result(
                &self.pool,
                command.household_id,
                command.target_id,
            )
            .await
            .map_err(|error| WorkflowFailure::Infrastructure(format!("{error:?}")))?;
        Ok(AccountMovementProjection {
            id: result.id,
            household_id: result.household_id,
            kind: result.kind,
            occurred_at: result.occurred_at,
            from_account_id: result.from_account_id,
            from_account: map_account(result.from_account),
            to_account_id: result.to_account_id,
            to_account: map_account(result.to_account),
            amount_yen: result.amount_yen,
            description: result.description,
            created_at: result.created_at,
        })
    }

    async fn link_income(
        &self,
        command: StatementLinkCommand,
    ) -> Result<AccountIncomeProjection, StatementLinkMutationFailure> {
        let source_account_id = command.source_account_id.ok_or_else(invalid_input)?;
        let account_id = bookkeeping_application::AccountId::new(source_account_id.to_string())
            .map_err(|_| invalid_input())?;
        let decision = bookkeeping_domain::account_income::LinkStatementIncomeDecision {
            source_document_id: command.source_document_id.to_string(),
            source_reference: command.source_reference,
            statement_account_id: account_id.clone(),
            income_account_id: account_id,
        };
        crate::evidence_workflows::persist_link_statement_income(
            &self.pool,
            bookkeeping_postgres::account_income::PersistLinkStatementIncome {
                household_id: command.household_id.to_string(),
                actor_id: command.actor_id.to_string(),
                account_income_id: command.target_id.to_string(),
                statement_account_id: source_account_id.to_string(),
            },
            decision,
        )
        .await
        .map_err(map_workflow)?;
        let result =
            bookkeeping_postgres::account_command_result::load_account_income_command_result(
                &self.pool,
                command.household_id,
                command.target_id,
            )
            .await
            .map_err(|error| WorkflowFailure::Infrastructure(format!("{error:?}")))?;
        Ok(AccountIncomeProjection {
            id: result.id,
            household_id: result.household_id,
            account_id: result.account_id,
            amount_yen: result.amount_yen,
            occurred_at: result.occurred_at,
            description: result.description,
            created_at: result.created_at,
        })
    }

    async fn link_transfer(
        &self,
        command: StatementTransferLinkCommand,
    ) -> Result<TransferLedgerEntryProjection, StatementLinkMutationFailure> {
        crate::evidence_workflows::persist_link_statement_transfer(
            &self.pool,
            bookkeeping_postgres::statement_transfer::PersistLinkStatementTransfer {
                household_id: command.household_id.to_string(),
                actor_id: command.actor_id.to_string(),
                ledger_entry_id: command.ledger_entry_id.to_string(),
                source_document_id: command.source_document_id.to_string(),
                source_reference: command.source_reference,
                source_account_id: command.source_account_id.to_string(),
            },
        )
        .await
        .map_err(map_workflow)?;
        let result = crate::financial_command_result::load_transfer_ledger_entry_result(
            &self.pool,
            command.household_id,
            command.ledger_entry_id,
        )
        .await
        .map_err(|error| WorkflowFailure::Infrastructure(format!("{error:?}")))?;
        Ok(TransferLedgerEntryProjection {
            id: result.id,
            kind: result.kind,
            occurred_at: result.occurred_at,
            description: result.description,
            amount_yen: result.amount_yen,
            currency: result.currency,
            created_at: result.created_at,
            from_member: TransferMemberProjection {
                id: result.from_member.id,
                display_name: result.from_member.display_name,
                role: result.from_member.role,
            },
            to_member: TransferMemberProjection {
                id: result.to_member.id,
                display_name: result.to_member.display_name,
                role: result.to_member.role,
            },
            loan: result.loan.map(|loan| LoanBalanceProjection {
                id: loan.id,
                lender_member_id: loan.lender_member_id,
                borrower_member_id: loan.borrower_member_id,
                principal_yen: loan.principal_yen,
                repaid_yen: loan.repaid_yen,
                balance_yen: loan.balance_yen,
                created_at: loan.created_at,
            }),
        })
    }

    async fn link_refund_correction(
        &self,
        command: StatementRefundCorrectionLinkCommand,
    ) -> Result<PurchaseProjection, StatementLinkMutationFailure> {
        let purchase_id = crate::evidence_workflows::link_statement_refund_correction(
            &self.pool,
            bookkeeping_postgres::correct_purchase::LinkStatementRefundCorrection {
                household_id: command.household_id.to_string(),
                actor_id: command.actor_id.to_string(),
                correction_id: command.correction_id.to_string(),
                source_document_id: command.source_document_id.to_string(),
                source_reference: command.source_reference,
            },
        )
        .await
        .map_err(map_workflow)?;
        let purchase_id = Uuid::parse_str(&purchase_id)
            .map_err(|error| WorkflowFailure::Infrastructure(error.to_string()))?;
        let result = crate::purchase_command_result::load_purchase_command_result(
            &self.pool,
            command.household_id,
            purchase_id,
        )
        .await
        .map_err(|error| WorkflowFailure::Infrastructure(format!("{error:?}")))?;
        Ok(PurchaseProjection {
            id: result.id,
            household_id: result.household_id,
            occurred_at: result.occurred_at,
            date_precision: result.date_precision,
            merchant: result.merchant,
            origin: result.origin,
            status: result.status,
            revision: result.revision,
            created_at: result.created_at,
            updated_at: result.updated_at,
            has_evidence: result.has_evidence,
            source_kinds: result.source_kinds,
            total_yen: result.total_yen,
            correction_amount_yen: result.correction_amount_yen,
            correction_count: result.correction_count,
            remaining_refundable_amount_yen: result.remaining_refundable_amount_yen,
            items: result
                .items
                .into_iter()
                .map(|item| PurchaseItemProjection {
                    id: item.id,
                    name: item.name,
                    amount_yen: item.amount_yen,
                    category_id: item.category_id,
                    product_variant_id: item.product_variant_id,
                    revision: item.revision,
                    quantity_numerator: item.quantity_numerator,
                    quantity_denominator: item.quantity_denominator,
                    quantity_unit: item.quantity_unit,
                    printed_unit_price_yen: item.printed_unit_price_yen,
                    printed_unit_price_quantity_numerator: item
                        .printed_unit_price_quantity_numerator,
                    printed_unit_price_quantity_denominator: item
                        .printed_unit_price_quantity_denominator,
                    printed_unit_price_unit: item.printed_unit_price_unit,
                    allocations: item
                        .allocations
                        .into_iter()
                        .map(|allocation| PurchaseItemAllocationProjection {
                            pool_id: allocation.pool_id,
                            pool_name: allocation.pool_name,
                            weight: allocation.weight,
                            amount_yen: allocation.amount_yen,
                        })
                        .collect(),
                    tags: item.tags,
                    remaining_refundable_amount_yen: item.remaining_refundable_amount_yen,
                })
                .collect(),
            payments: result
                .payments
                .into_iter()
                .map(|payment| PurchasePaymentProjection {
                    account_id: payment.account_id,
                    account_name: payment.account_name,
                    account_kind: payment.account_kind,
                    account_card_last4: payment.account_card_last4,
                    account_pool_id: payment.account_pool_id,
                    amount_yen: payment.amount_yen,
                })
                .collect(),
        })
    }
}

fn map_account(
    value: bookkeeping_postgres::account_command_result::AccountCommandAccount,
) -> AccountProjection {
    AccountProjection {
        id: value.id,
        name: value.name,
        kind: value.kind,
        card_last4: value.card_last4,
        pool_id: value.pool_id,
    }
}

fn invalid_input() -> StatementLinkMutationFailure {
    WorkflowFailure::Decision(WorkflowRejection::InvalidInput)
}

fn map_workflow<E: std::fmt::Debug>(
    error: WorkflowFailure<WorkflowRejection, E>,
) -> StatementLinkMutationFailure {
    match error {
        WorkflowFailure::Decision(rejection) => WorkflowFailure::Decision(rejection),
        WorkflowFailure::NotFound => WorkflowFailure::NotFound,
        WorkflowFailure::Conflict => WorkflowFailure::Conflict,
        WorkflowFailure::Infrastructure(error) => {
            WorkflowFailure::Infrastructure(format!("{error:?}"))
        }
    }
}
