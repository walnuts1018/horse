use crate::{PgPool, evidence_operations::EvidencePostgresOperations};
use bookkeeping_application::workflow_failure::{WorkflowFailure, WorkflowRejection};
use bookkeeping_postgres::{
    account_income, account_movement, correct_purchase, record_repayment, record_transfer,
    statement_transfer,
};
use chrono::{DateTime, Utc};
use uuid::Uuid;

pub async fn load_transfer_command_context(
    pool: &PgPool,
    household_id: Uuid,
    from_member_id: Uuid,
    payment_account_id: Option<Uuid>,
    to_member_id: Uuid,
    destination_account_id: Option<Uuid>,
    occurred_at: DateTime<Utc>,
) -> Result<
    crate::policy_data::TransferCommandContext,
    WorkflowFailure<WorkflowRejection, crate::policy_data::FinancialCommandContextError>,
> {
    crate::policy_data::load_transfer_command_context(
        pool,
        household_id,
        from_member_id,
        payment_account_id,
        to_member_id,
        destination_account_id,
        occurred_at,
    )
    .await
    .map_err(map_financial_context_error)
}

pub async fn load_repayment_command_context(
    pool: &PgPool,
    input: crate::policy_data::RepaymentCommandContextInput,
) -> Result<
    crate::policy_data::RepaymentCommandContext,
    WorkflowFailure<WorkflowRejection, crate::policy_data::FinancialCommandContextError>,
> {
    crate::policy_data::load_repayment_command_context(pool, input)
        .await
        .map_err(map_financial_context_error)
}

fn map_financial_context_error(
    error: crate::policy_data::FinancialCommandContextError,
) -> WorkflowFailure<WorkflowRejection, crate::policy_data::FinancialCommandContextError> {
    use crate::policy_data::FinancialCommandContextError as E;
    match error {
        E::HouseholdNotFound | E::AccountNotFound | E::LoanNotFound => WorkflowFailure::NotFound,
        E::AccountingPeriodClosed => WorkflowFailure::Conflict,
        E::InvalidAccountKind(_)
        | E::InvalidPoolKind(_)
        | E::InvalidAccountingPeriod(_)
        | E::InvalidLoan => WorkflowFailure::Decision(WorkflowRejection::InvalidInput),
        E::InvalidTokyoOffset | E::Pool(_) | E::Database(_) => {
            WorkflowFailure::Infrastructure(error)
        }
    }
}

pub async fn persist_record_account_income(
    pool: &PgPool,
    input: account_income::PersistRecordAccountIncome,
    decision: bookkeeping_domain::account_income::RecordAccountIncomeDecision,
) -> Result<
    account_income::PersistedAccountIncome,
    WorkflowFailure<WorkflowRejection, account_income::AccountIncomePersistenceError>,
> {
    account_income::persist_record_account_income(
        pool,
        input,
        decision,
        &EvidencePostgresOperations,
    )
    .await
    .map_err(|error| match error {
        account_income::AccountIncomePersistenceError::IdempotencyConflict
        | account_income::AccountIncomePersistenceError::AccountingPeriodClosed => {
            WorkflowFailure::Conflict
        }
        account_income::AccountIncomePersistenceError::HouseholdNotFound
        | account_income::AccountIncomePersistenceError::MemberNotFound
        | account_income::AccountIncomePersistenceError::AccountNotFound
        | account_income::AccountIncomePersistenceError::SourceDocumentNotFound => {
            WorkflowFailure::NotFound
        }
        account_income::AccountIncomePersistenceError::InvalidIdempotencyKey
        | account_income::AccountIncomePersistenceError::InvalidDecision
        | account_income::AccountIncomePersistenceError::StatementExtractionUnavailable
        | account_income::AccountIncomePersistenceError::StatementReferenceNotFound
        | account_income::AccountIncomePersistenceError::StatementAlreadyConfirmed
        | account_income::AccountIncomePersistenceError::StatementIsNotExternalIncome
        | account_income::AccountIncomePersistenceError::StatementIncomeMismatch => {
            WorkflowFailure::Decision(WorkflowRejection::InvalidInput)
        }
        error => WorkflowFailure::Infrastructure(error),
    })
}

pub async fn persist_link_statement_income(
    pool: &PgPool,
    input: account_income::PersistLinkStatementIncome,
    decision: bookkeeping_domain::account_income::LinkStatementIncomeDecision,
) -> Result<
    account_income::PersistedAccountIncome,
    WorkflowFailure<WorkflowRejection, account_income::AccountIncomePersistenceError>,
> {
    account_income::persist_link_statement_income(
        pool,
        input,
        decision,
        &EvidencePostgresOperations,
    )
    .await
    .map_err(|error| match error {
        account_income::AccountIncomePersistenceError::AccountingPeriodClosed
        | account_income::AccountIncomePersistenceError::IdempotencyConflict
        | account_income::AccountIncomePersistenceError::StatementAlreadyConfirmed => {
            WorkflowFailure::Conflict
        }
        account_income::AccountIncomePersistenceError::HouseholdNotFound
        | account_income::AccountIncomePersistenceError::MemberNotFound
        | account_income::AccountIncomePersistenceError::AccountNotFound
        | account_income::AccountIncomePersistenceError::SourceDocumentNotFound => {
            WorkflowFailure::NotFound
        }
        account_income::AccountIncomePersistenceError::InvalidUuid(_)
        | account_income::AccountIncomePersistenceError::InvalidDecision
        | account_income::AccountIncomePersistenceError::StatementExtractionUnavailable
        | account_income::AccountIncomePersistenceError::StatementReferenceNotFound
        | account_income::AccountIncomePersistenceError::StatementIsNotExternalIncome
        | account_income::AccountIncomePersistenceError::StatementIncomeMismatch
        | account_income::AccountIncomePersistenceError::InvalidIdempotencyKey => {
            WorkflowFailure::Decision(WorkflowRejection::InvalidInput)
        }
        error => WorkflowFailure::Infrastructure(error),
    })
}

pub async fn persist_record_transfer(
    pool: &PgPool,
    input: record_transfer::PersistRecordTransfer,
    decision: bookkeeping_domain::record_transfer::RecordTransferDecision,
) -> Result<
    record_transfer::PersistedTransfer,
    WorkflowFailure<WorkflowRejection, record_transfer::RecordTransferPersistenceError>,
> {
    record_transfer::persist_record_transfer(
        pool,
        input,
        decision,
        &EvidencePostgresOperations,
        &mut crate::policy_settlement::PolicySettlementReferences::new(pool.clone()),
    )
    .await
    .map_err(map_transfer_error)
}

pub async fn find_record_transfer_replay(
    pool: &PgPool,
    input: &record_transfer::FindRecordTransferReplay,
) -> Result<
    Option<String>,
    WorkflowFailure<WorkflowRejection, record_transfer::RecordTransferPersistenceError>,
> {
    record_transfer::find_record_transfer_replay(pool, input, &EvidencePostgresOperations)
        .await
        .map_err(map_transfer_error)
}

pub async fn persist_record_repayment(
    pool: &PgPool,
    input: record_repayment::PersistRecordRepayment,
    decision: bookkeeping_domain::record_repayment::RecordRepaymentDecision,
) -> Result<
    record_repayment::PersistedRepayment,
    WorkflowFailure<WorkflowRejection, record_repayment::RecordRepaymentPersistenceError>,
> {
    record_repayment::persist_record_repayment(pool, input, decision, &EvidencePostgresOperations)
        .await
        .map_err(map_repayment_error)
}

pub async fn find_record_repayment_replay(
    pool: &PgPool,
    input: &record_repayment::FindRecordRepaymentReplay,
) -> Result<
    Option<String>,
    WorkflowFailure<WorkflowRejection, record_repayment::RecordRepaymentPersistenceError>,
> {
    record_repayment::find_record_repayment_replay(pool, input, &EvidencePostgresOperations)
        .await
        .map_err(map_repayment_error)
}

fn map_transfer_error(
    error: record_transfer::RecordTransferPersistenceError,
) -> WorkflowFailure<WorkflowRejection, record_transfer::RecordTransferPersistenceError> {
    use record_transfer::RecordTransferPersistenceError as E;
    match error {
        E::IdempotencyConflict | E::AccountingPeriodClosed | E::StatementAlreadyConfirmed => {
            WorkflowFailure::Conflict
        }
        E::HouseholdNotFound
        | E::MemberNotFound
        | E::AccountNotFound
        | E::PoolNotFound
        | E::LoanNotFound
        | E::SourceDocumentNotFound
        | E::StatementReferenceNotFound => WorkflowFailure::NotFound,
        E::InvalidUuid(_)
        | E::InvalidIdempotencyKey
        | E::NumericOutOfRange(_)
        | E::LoanDirectionMismatch
        | E::RepaymentExceedsOutstanding
        | E::InvalidAccountKind
        | E::AccountWithoutPool
        | E::AccountOwnershipMismatch
        | E::SourceIsNotBankStatement
        | E::StatementExtractionUnavailable
        | E::StatementDoesNotMatchTransfer
        | E::InvalidDecision => WorkflowFailure::Decision(WorkflowRejection::InvalidInput),
        error => WorkflowFailure::Infrastructure(error),
    }
}

fn map_repayment_error(
    error: record_repayment::RecordRepaymentPersistenceError,
) -> WorkflowFailure<WorkflowRejection, record_repayment::RecordRepaymentPersistenceError> {
    use record_repayment::RecordRepaymentPersistenceError as E;
    match error {
        E::IdempotencyConflict | E::AccountingPeriodClosed | E::StatementAlreadyConfirmed => {
            WorkflowFailure::Conflict
        }
        E::HouseholdNotFound
        | E::LoanNotFound
        | E::MemberNotFound
        | E::SourceDocumentNotFound
        | E::StatementReferenceNotFound => WorkflowFailure::NotFound,
        E::InvalidUuid(_)
        | E::InvalidPeriod
        | E::InvalidIdempotencyKey
        | E::NumericOutOfRange(_)
        | E::LoanDirectionMismatch
        | E::RepaymentExceedsOutstanding
        | E::AccountNotFound
        | E::InvalidAccountKind
        | E::SourceIsNotBankStatement
        | E::StatementExtractionUnavailable
        | E::StatementDoesNotMatchTransfer
        | E::InvalidDecision => WorkflowFailure::Decision(WorkflowRejection::InvalidInput),
        error => WorkflowFailure::Infrastructure(error),
    }
}

pub async fn persist_record_account_movement(
    pool: &PgPool,
    input: account_movement::PersistRecordAccountMovement,
    decision: bookkeeping_domain::account_movement::RecordAccountMovementDecision,
) -> Result<
    account_movement::PersistedAccountMovement,
    WorkflowFailure<WorkflowRejection, account_movement::AccountMovementPersistenceError>,
> {
    account_movement::persist_record_account_movement(
        pool,
        input,
        decision,
        &EvidencePostgresOperations,
    )
    .await
    .map_err(|error| match error {
        account_movement::AccountMovementPersistenceError::IdempotencyConflict
        | account_movement::AccountMovementPersistenceError::AccountingPeriodClosed => {
            WorkflowFailure::Conflict
        }
        account_movement::AccountMovementPersistenceError::HouseholdNotFound
        | account_movement::AccountMovementPersistenceError::MemberNotFound
        | account_movement::AccountMovementPersistenceError::AccountNotFound
        | account_movement::AccountMovementPersistenceError::SourceDocumentNotFound => {
            WorkflowFailure::NotFound
        }
        account_movement::AccountMovementPersistenceError::InvalidIdempotencyKey
        | account_movement::AccountMovementPersistenceError::InvalidDecision
        | account_movement::AccountMovementPersistenceError::StatementExtractionUnavailable
        | account_movement::AccountMovementPersistenceError::StatementReferenceNotFound
        | account_movement::AccountMovementPersistenceError::StatementAlreadyConfirmed
        | account_movement::AccountMovementPersistenceError::StatementIsNotBankMovement
        | account_movement::AccountMovementPersistenceError::StatementMovementMismatch => {
            WorkflowFailure::Decision(WorkflowRejection::InvalidInput)
        }
        error => WorkflowFailure::Infrastructure(error),
    })
}

pub async fn persist_link_statement_movement(
    pool: &PgPool,
    input: account_movement::PersistLinkStatementMovement,
    decision: bookkeeping_domain::account_movement::LinkStatementMovementDecision,
) -> Result<
    account_movement::PersistedAccountMovement,
    WorkflowFailure<WorkflowRejection, account_movement::AccountMovementPersistenceError>,
> {
    account_movement::persist_link_statement_movement(
        pool,
        input,
        decision,
        &EvidencePostgresOperations,
    )
    .await
    .map_err(|error| match error {
        account_movement::AccountMovementPersistenceError::AccountingPeriodClosed
        | account_movement::AccountMovementPersistenceError::IdempotencyConflict
        | account_movement::AccountMovementPersistenceError::StatementAlreadyConfirmed => {
            WorkflowFailure::Conflict
        }
        account_movement::AccountMovementPersistenceError::HouseholdNotFound
        | account_movement::AccountMovementPersistenceError::MemberNotFound
        | account_movement::AccountMovementPersistenceError::AccountNotFound
        | account_movement::AccountMovementPersistenceError::SourceDocumentNotFound => {
            WorkflowFailure::NotFound
        }
        account_movement::AccountMovementPersistenceError::InvalidUuid(_)
        | account_movement::AccountMovementPersistenceError::InvalidDecision
        | account_movement::AccountMovementPersistenceError::StatementExtractionUnavailable
        | account_movement::AccountMovementPersistenceError::StatementReferenceNotFound
        | account_movement::AccountMovementPersistenceError::StatementIsNotBankMovement
        | account_movement::AccountMovementPersistenceError::StatementMovementMismatch
        | account_movement::AccountMovementPersistenceError::InvalidIdempotencyKey => {
            WorkflowFailure::Decision(WorkflowRejection::InvalidInput)
        }
        error => WorkflowFailure::Infrastructure(error),
    })
}

pub async fn correct_purchase(
    pool: &PgPool,
    request: correct_purchase::CorrectPurchaseRequest,
) -> Result<
    correct_purchase::PersistedCorrectPurchase,
    WorkflowFailure<WorkflowRejection, correct_purchase::CorrectPurchasePersistenceError>,
> {
    correct_purchase::correct_purchase(
        pool,
        request,
        &EvidencePostgresOperations,
        &mut crate::policy_settlement::PolicySettlementReferences::new(pool.clone()),
    )
    .await
    .map_err(map_correct_purchase_error)
}

fn map_correct_purchase_error(
    error: correct_purchase::CorrectPurchasePersistenceError,
) -> WorkflowFailure<WorkflowRejection, correct_purchase::CorrectPurchasePersistenceError> {
    use correct_purchase::CorrectPurchasePersistenceError as E;
    match error {
        E::IdempotencyConflict
        | E::AccountingPeriodClosed
        | E::PurchaseNotPosted
        | E::RefundExceedsPurchaseBalance
        | E::RefundExceedsItemBalance
        | E::StatementAlreadyConfirmed
        | E::RefundDoesNotMatchCorrection => WorkflowFailure::Conflict,
        E::PurchaseNotFound
        | E::SourceDocumentNotFound
        | E::StatementReferenceNotFound
        | E::MemberNotFound
        | E::HouseholdMismatch => WorkflowFailure::NotFound,
        E::InvalidUuid(_)
        | E::InvalidIdempotencyKey
        | E::InvalidDecision
        | E::StatementNotRefundable
        | E::StatementExtractionUnavailable => {
            WorkflowFailure::Decision(WorkflowRejection::InvalidInput)
        }
        error => WorkflowFailure::Infrastructure(error),
    }
}

pub async fn link_statement_refund_correction(
    pool: &PgPool,
    input: correct_purchase::LinkStatementRefundCorrection,
) -> Result<
    String,
    WorkflowFailure<WorkflowRejection, correct_purchase::CorrectPurchasePersistenceError>,
> {
    correct_purchase::link_statement_refund_correction(pool, input, &EvidencePostgresOperations)
        .await
        .map_err(|error| match error {
            correct_purchase::CorrectPurchasePersistenceError::StatementAlreadyConfirmed
            | correct_purchase::CorrectPurchasePersistenceError::AccountingPeriodClosed
            | correct_purchase::CorrectPurchasePersistenceError::IdempotencyConflict => {
                WorkflowFailure::Conflict
            }
            correct_purchase::CorrectPurchasePersistenceError::HouseholdMismatch
            | correct_purchase::CorrectPurchasePersistenceError::MemberNotFound
            | correct_purchase::CorrectPurchasePersistenceError::CorrectionNotFound
            | correct_purchase::CorrectPurchasePersistenceError::PurchaseNotFound
            | correct_purchase::CorrectPurchasePersistenceError::SourceDocumentNotFound => {
                WorkflowFailure::NotFound
            }
            correct_purchase::CorrectPurchasePersistenceError::InvalidUuid(_)
            | correct_purchase::CorrectPurchasePersistenceError::InvalidDecision
            | correct_purchase::CorrectPurchasePersistenceError::StatementNotRefundable
            | correct_purchase::CorrectPurchasePersistenceError::StatementExtractionUnavailable
            | correct_purchase::CorrectPurchasePersistenceError::StatementReferenceNotFound
            | correct_purchase::CorrectPurchasePersistenceError::RefundDoesNotMatchCorrection => {
                WorkflowFailure::Decision(WorkflowRejection::InvalidInput)
            }
            error => WorkflowFailure::Infrastructure(error),
        })
}

pub async fn persist_link_statement_transfer(
    pool: &PgPool,
    input: statement_transfer::PersistLinkStatementTransfer,
) -> Result<
    statement_transfer::PersistedStatementTransfer,
    WorkflowFailure<WorkflowRejection, statement_transfer::StatementTransferPersistenceError>,
> {
    statement_transfer::persist_link_statement_transfer(pool, input, &EvidencePostgresOperations)
        .await
        .map_err(|error| match error {
            statement_transfer::StatementTransferPersistenceError::StatementAlreadyConfirmed => WorkflowFailure::Conflict,
            statement_transfer::StatementTransferPersistenceError::HouseholdNotFound
            | statement_transfer::StatementTransferPersistenceError::MemberNotFound
            | statement_transfer::StatementTransferPersistenceError::LedgerEntryNotFound
            | statement_transfer::StatementTransferPersistenceError::TransferNotFound
            | statement_transfer::StatementTransferPersistenceError::SourceDocumentNotFound
            | statement_transfer::StatementTransferPersistenceError::StatementReferenceNotFound
            | statement_transfer::StatementTransferPersistenceError::AccountNotFound => WorkflowFailure::NotFound,
            statement_transfer::StatementTransferPersistenceError::InvalidUuid(_)
            | statement_transfer::StatementTransferPersistenceError::InvalidSource
            | statement_transfer::StatementTransferPersistenceError::InvalidDecision
            | statement_transfer::StatementTransferPersistenceError::SourceIsNotBankStatement
            | statement_transfer::StatementTransferPersistenceError::StatementExtractionUnavailable
            | statement_transfer::StatementTransferPersistenceError::StatementDoesNotMatchTransfer
            | statement_transfer::StatementTransferPersistenceError::AccountIsNotBank
            | statement_transfer::StatementTransferPersistenceError::JournalEntryMismatch => WorkflowFailure::Decision(WorkflowRejection::InvalidInput),
            error => WorkflowFailure::Infrastructure(error),
        })
}

#[derive(Debug)]
pub enum AccountMutationWorkflowError {
    Context(bookkeeping_postgres::account_command_context::AccountCommandContextError),
    Movement(account_movement::AccountMovementPersistenceError),
    Income(account_income::AccountIncomePersistenceError),
    Pool(String),
    AccessDenied,
    Conflict,
    NotFound,
    Invalid,
}

pub async fn record_account_movement_workflow(
    pool: &PgPool,
    command: bookkeeping_application::account_movement::RecordAccountMovementWorkflowCommand,
) -> Result<
    bookkeeping_application::account_movement::MovementWorkflowResult,
    WorkflowFailure<WorkflowRejection, AccountMutationWorkflowError>,
> {
    use bookkeeping_application::account_movement as app;
    let household_id = Uuid::parse_str(&command.household_id)
        .map_err(|_| WorkflowFailure::Decision(WorkflowRejection::InvalidInput))?;
    let actor_id = Uuid::parse_str(command.actor_id.as_str())
        .map_err(|_| WorkflowFailure::Decision(WorkflowRejection::InvalidInput))?;
    struct Ops<'a> {
        pool: &'a PgPool,
        household_id: Uuid,
        actor_id: Uuid,
    }
    impl app::RecordAccountMovementOperations for Ops<'_> {
        type Error = AccountMutationWorkflowError;
        async fn load(
            &mut self,
            command: &app::RecordAccountMovementWorkflowCommand,
        ) -> Result<
            (
                bookkeeping_domain::OpenAccountingPeriod,
                std::collections::BTreeMap<String, app::AccountKind>,
                bool,
            ),
            Self::Error,
        > {
            if !household_postgres::household_access::is_active_member(
                self.pool,
                self.household_id,
                self.actor_id,
            )
            .await
            .map_err(|e| AccountMutationWorkflowError::Pool(format!("{e:?}")))?
            {
                return Err(AccountMutationWorkflowError::AccessDenied);
            }
            let ids = command
                .account_ids
                .iter()
                .map(|id| {
                    Uuid::parse_str(id).map_err(|_| {
                        AccountMutationWorkflowError::Pool("invalid account id".to_owned())
                    })
                })
                .collect::<Result<Vec<_>, _>>()?;
            let context =
                bookkeeping_postgres::account_command_context::load_account_command_context(
                    self.pool,
                    self.household_id,
                    &ids,
                    command.occurred_at,
                )
                .await
                .map_err(AccountMutationWorkflowError::Context)?;
            let kinds = context
                .account_kinds
                .into_iter()
                .map(|(id, kind)| (id.to_string(), kind))
                .collect();
            Ok((
                context.accounting_period,
                kinds,
                !context.missing_account_ids.is_empty(),
            ))
        }
        async fn persist(
            &mut self,
            command: &app::RecordAccountMovementWorkflowCommand,
            decision: bookkeeping_domain::account_movement::RecordAccountMovementDecision,
        ) -> Result<app::MovementWorkflowResult, Self::Error> {
            let result = persist_record_account_movement(
                self.pool,
                account_movement::PersistRecordAccountMovement {
                    household_id: command.household_id.clone(),
                    actor_id: command.actor_id.as_str().to_owned(),
                    occurred_at: command.occurred_at,
                    idempotency_key: command.idempotency_key.clone(),
                    request_fingerprint: command.request_fingerprint,
                },
                decision,
            )
            .await
            .map_err(|error| match error {
                WorkflowFailure::Conflict => AccountMutationWorkflowError::Conflict,
                WorkflowFailure::NotFound => AccountMutationWorkflowError::NotFound,
                WorkflowFailure::Decision(_) => AccountMutationWorkflowError::Invalid,
                WorkflowFailure::Infrastructure(e) => AccountMutationWorkflowError::Movement(e),
            })?;
            Ok(app::MovementWorkflowResult {
                id: result.id,
                amount_yen: result.amount_yen,
                description: result.description,
            })
        }
    }
    match app::record_account_movement(&mut Ops { pool, household_id, actor_id }, command).await {
        Ok(result) => Ok(result),
        Err(app::RecordAccountMovementWorkflowError::Decision(_)) => Err(WorkflowFailure::Decision(WorkflowRejection::InvalidInput)),
        Err(app::RecordAccountMovementWorkflowError::NotFound) => Err(WorkflowFailure::NotFound),
        Err(app::RecordAccountMovementWorkflowError::Conflict) => Err(WorkflowFailure::Conflict),
        Err(app::RecordAccountMovementWorkflowError::Operations(AccountMutationWorkflowError::Context(bookkeeping_postgres::account_command_context::AccountCommandContextError::AccountingPeriodClosed))) => Err(WorkflowFailure::Conflict),
        Err(app::RecordAccountMovementWorkflowError::Operations(AccountMutationWorkflowError::Context(e))) => Err(WorkflowFailure::Infrastructure(AccountMutationWorkflowError::Context(e))),
        Err(app::RecordAccountMovementWorkflowError::Operations(AccountMutationWorkflowError::Conflict)) => Err(WorkflowFailure::Conflict),
        Err(app::RecordAccountMovementWorkflowError::Operations(AccountMutationWorkflowError::NotFound)) => Err(WorkflowFailure::NotFound),
        Err(app::RecordAccountMovementWorkflowError::Operations(AccountMutationWorkflowError::Invalid)) => Err(WorkflowFailure::Decision(WorkflowRejection::InvalidInput)),
        Err(app::RecordAccountMovementWorkflowError::Operations(AccountMutationWorkflowError::Movement(e))) => Err(WorkflowFailure::Infrastructure(AccountMutationWorkflowError::Movement(e))),
        Err(app::RecordAccountMovementWorkflowError::Operations(AccountMutationWorkflowError::Income(e))) => Err(WorkflowFailure::Infrastructure(AccountMutationWorkflowError::Income(e))),
        Err(app::RecordAccountMovementWorkflowError::Operations(AccountMutationWorkflowError::AccessDenied)) => Err(WorkflowFailure::NotFound),
        Err(app::RecordAccountMovementWorkflowError::Operations(AccountMutationWorkflowError::Pool(e))) => Err(WorkflowFailure::Infrastructure(AccountMutationWorkflowError::Pool(e))),
    }
}

pub async fn record_account_income_workflow(
    pool: &PgPool,
    command: bookkeeping_application::account_income::RecordAccountIncomeWorkflowCommand,
) -> Result<
    bookkeeping_application::account_income::IncomeWorkflowResult,
    WorkflowFailure<WorkflowRejection, AccountMutationWorkflowError>,
> {
    use bookkeeping_application::account_income as app;
    let household_id = Uuid::parse_str(&command.household_id)
        .map_err(|_| WorkflowFailure::Decision(WorkflowRejection::InvalidInput))?;
    let actor_id = Uuid::parse_str(command.actor_id.as_str())
        .map_err(|_| WorkflowFailure::Decision(WorkflowRejection::InvalidInput))?;
    struct Ops<'a> {
        pool: &'a PgPool,
        household_id: Uuid,
        actor_id: Uuid,
    }
    impl app::RecordAccountIncomeOperations for Ops<'_> {
        type Error = AccountMutationWorkflowError;
        async fn load(
            &mut self,
            command: &app::RecordAccountIncomeWorkflowCommand,
        ) -> Result<
            (
                bookkeeping_domain::OpenAccountingPeriod,
                Option<app::IncomeAccountKind>,
            ),
            Self::Error,
        > {
            if !household_postgres::household_access::is_active_member(
                self.pool,
                self.household_id,
                self.actor_id,
            )
            .await
            .map_err(|e| AccountMutationWorkflowError::Pool(format!("{e:?}")))?
            {
                return Err(AccountMutationWorkflowError::AccessDenied);
            }
            let account_id = Uuid::parse_str(command.account_id.as_str())
                .map_err(|_| AccountMutationWorkflowError::Pool("invalid account id".to_owned()))?;
            let context =
                bookkeeping_postgres::account_command_context::load_account_command_context(
                    self.pool,
                    self.household_id,
                    &[account_id],
                    command.occurred_at,
                )
                .await
                .map_err(AccountMutationWorkflowError::Context)?;
            let account_kind = context.income_kind(account_id);
            Ok((context.accounting_period, account_kind))
        }
        async fn persist(
            &mut self,
            command: &app::RecordAccountIncomeWorkflowCommand,
            decision: bookkeeping_domain::account_income::RecordAccountIncomeDecision,
        ) -> Result<app::IncomeWorkflowResult, Self::Error> {
            let result = persist_record_account_income(
                self.pool,
                account_income::PersistRecordAccountIncome {
                    household_id: command.household_id.clone(),
                    actor_id: command.actor_id.as_str().to_owned(),
                    occurred_at: command.occurred_at,
                    idempotency_key: command.idempotency_key.clone(),
                    request_fingerprint: command.request_fingerprint,
                },
                decision,
            )
            .await
            .map_err(|error| match error {
                WorkflowFailure::Conflict => AccountMutationWorkflowError::Conflict,
                WorkflowFailure::NotFound => AccountMutationWorkflowError::NotFound,
                WorkflowFailure::Decision(_) => AccountMutationWorkflowError::Invalid,
                WorkflowFailure::Infrastructure(e) => AccountMutationWorkflowError::Income(e),
            })?;
            Ok(app::IncomeWorkflowResult {
                id: result.id,
                occurred_at: result.occurred_at,
                amount_yen: result.amount_yen,
                description: result.description,
            })
        }
    }
    match app::record_account_income(&mut Ops { pool, household_id, actor_id }, command).await {
        Ok(result) => Ok(result),
        Err(app::RecordAccountIncomeWorkflowError::Decision(_)) => Err(WorkflowFailure::Decision(WorkflowRejection::InvalidInput)),
        Err(app::RecordAccountIncomeWorkflowError::NotFound) => Err(WorkflowFailure::NotFound),
        Err(app::RecordAccountIncomeWorkflowError::Conflict) => Err(WorkflowFailure::Conflict),
        Err(app::RecordAccountIncomeWorkflowError::Operations(AccountMutationWorkflowError::Context(bookkeeping_postgres::account_command_context::AccountCommandContextError::AccountingPeriodClosed))) => Err(WorkflowFailure::Conflict),
        Err(app::RecordAccountIncomeWorkflowError::Operations(AccountMutationWorkflowError::Context(e))) => Err(WorkflowFailure::Infrastructure(AccountMutationWorkflowError::Context(e))),
        Err(app::RecordAccountIncomeWorkflowError::Operations(AccountMutationWorkflowError::Conflict)) => Err(WorkflowFailure::Conflict),
        Err(app::RecordAccountIncomeWorkflowError::Operations(AccountMutationWorkflowError::NotFound)) => Err(WorkflowFailure::NotFound),
        Err(app::RecordAccountIncomeWorkflowError::Operations(AccountMutationWorkflowError::Invalid)) => Err(WorkflowFailure::Decision(WorkflowRejection::InvalidInput)),
        Err(app::RecordAccountIncomeWorkflowError::Operations(AccountMutationWorkflowError::Income(e))) => Err(WorkflowFailure::Infrastructure(AccountMutationWorkflowError::Income(e))),
        Err(app::RecordAccountIncomeWorkflowError::Operations(AccountMutationWorkflowError::Movement(e))) => Err(WorkflowFailure::Infrastructure(AccountMutationWorkflowError::Movement(e))),
        Err(app::RecordAccountIncomeWorkflowError::Operations(AccountMutationWorkflowError::AccessDenied)) => Err(WorkflowFailure::NotFound),
        Err(app::RecordAccountIncomeWorkflowError::Operations(AccountMutationWorkflowError::Pool(e))) => Err(WorkflowFailure::Infrastructure(AccountMutationWorkflowError::Pool(e))),
    }
}
