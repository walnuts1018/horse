const MAX_AMOUNT_YEN: i64 = 1_000_000_000_000;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LinkStatementTransferCommand {
    pub actor_id: String,
    pub household_id: String,
    pub ledger_entry_id: String,
    pub source_document_id: String,
    pub source_reference: String,
    pub source_account_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct StatementTransferObservation {
    pub source_kind: String,
    pub kind: String,
    pub amount_yen: i64,
    pub occurred_at_unix_micros: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LinkStatementTransferDecision {
    pub actor_id: String,
    pub household_id: String,
    pub ledger_entry_id: String,
    pub source_document_id: String,
    pub source_reference: String,
    pub source_account_id: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum StatementTransferError {
    InvalidSource,
    InvalidIdentity,
    InvalidStatement,
    StatementKindMismatch,
    AmountMismatch,
    DateMismatch,
    AccountDirectionMismatch,
}

pub fn decide_link_statement_transfer(
    command: LinkStatementTransferCommand,
    observation: StatementTransferObservation,
    transfer_kind: &str,
    transfer_amount_yen: i64,
    transfer_occurred_at_unix_micros: i64,
    payment_account_id: Option<&str>,
    destination_account_id: Option<&str>,
) -> Result<LinkStatementTransferDecision, StatementTransferError> {
    if command.actor_id.trim().is_empty()
        || command.household_id.trim().is_empty()
        || command.ledger_entry_id.trim().is_empty()
    {
        return Err(StatementTransferError::InvalidIdentity);
    }
    if command.source_document_id.trim().is_empty()
        || command.source_account_id.trim().is_empty()
        || command.source_reference.trim().is_empty()
        || command.source_reference.len() > 256
    {
        return Err(StatementTransferError::InvalidSource);
    }
    if observation.source_kind != "bank"
        || observation.amount_yen == 0
        || observation.amount_yen.unsigned_abs() > MAX_AMOUNT_YEN as u64
    {
        return Err(StatementTransferError::InvalidStatement);
    }
    let expected_kind = match transfer_kind {
        "LOAN" => "LOAN",
        "REPAYMENT" => "REPAYMENT",
        _ => return Err(StatementTransferError::StatementKindMismatch),
    };
    if observation.kind != expected_kind {
        return Err(StatementTransferError::StatementKindMismatch);
    }
    if transfer_amount_yen <= 0
        || transfer_amount_yen > MAX_AMOUNT_YEN
        || observation.amount_yen.unsigned_abs() != transfer_amount_yen as u64
    {
        return Err(StatementTransferError::AmountMismatch);
    }
    if observation.occurred_at_unix_micros != transfer_occurred_at_unix_micros {
        return Err(StatementTransferError::DateMismatch);
    }
    let expected_account = if observation.amount_yen < 0 {
        payment_account_id
    } else {
        destination_account_id
    };
    if expected_account != Some(command.source_account_id.as_str()) {
        return Err(StatementTransferError::AccountDirectionMismatch);
    }
    Ok(LinkStatementTransferDecision {
        actor_id: command.actor_id,
        household_id: command.household_id,
        ledger_entry_id: command.ledger_entry_id,
        source_document_id: command.source_document_id,
        source_reference: command.source_reference,
        source_account_id: command.source_account_id,
    })
}
