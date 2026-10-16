use crate::{OpenAccountingPeriod, PositiveYen};

use crate::{AccountId, MemberId, Side};

const MAX_DESCRIPTION_BYTES: usize = 240;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AccountMovementKind {
    Transfer,
    CardBillPayment,
}

impl AccountMovementKind {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Transfer => "TRANSFER",
            Self::CardBillPayment => "CARD_BILL_PAYMENT",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AccountKind {
    Bank,
    Cash,
    Card,
    Other,
}

impl AccountKind {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Bank => "BANK",
            Self::Cash => "CASH",
            Self::Card => "CARD",
            Self::Other => "OTHER",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RecordAccountMovementCommand {
    pub actor_id: MemberId,
    pub from_account_id: AccountId,
    pub from_account_kind: AccountKind,
    pub to_account_id: AccountId,
    pub to_account_kind: AccountKind,
    pub kind: AccountMovementKind,
    pub occurred_at_unix_micros: i64,
    pub amount: PositiveYen,
    pub description: String,
    pub accounting_period: OpenAccountingPeriod,
    pub statement_source: Option<StatementMovementSource>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct StatementMovementSource {
    pub source_document_id: String,
    pub source_reference: String,
    pub statement_account_id: AccountId,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct StatementMovementObservation {
    pub bank_statement: bool,
    pub has_region: bool,
    pub kind: String,
    pub amount_yen: i64,
    pub occurred_at_unix_micros: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LinkStatementMovementCommand {
    pub source_document_id: String,
    pub source_reference: String,
    pub statement_account_id: AccountId,
    pub statement_account_kind: AccountKind,
    pub movement_id: String,
    pub from_account_id: AccountId,
    pub to_account_id: AccountId,
    pub kind: AccountMovementKind,
    pub amount: PositiveYen,
    pub occurred_at_unix_micros: i64,
    pub statement: StatementMovementObservation,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LinkStatementMovementDecision {
    pub source_document_id: String,
    pub source_reference: String,
    pub statement_account_id: AccountId,
    pub movement_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountMovementJournalLine {
    pub line_no: u32,
    pub side: Side,
    pub account_id: AccountId,
    pub amount: PositiveYen,
    pub memo: &'static str,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RecordAccountMovementDecision {
    pub actor_id: MemberId,
    pub from_account_id: AccountId,
    pub from_account_kind: AccountKind,
    pub to_account_id: AccountId,
    pub to_account_kind: AccountKind,
    pub kind: AccountMovementKind,
    pub occurred_at_unix_micros: i64,
    pub amount: PositiveYen,
    pub description: String,
    pub accounting_period: OpenAccountingPeriod,
    pub journal_lines: [AccountMovementJournalLine; 2],
    pub changed_fields: [&'static str; 5],
    pub statement_source: Option<StatementMovementSource>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AccountMovementError {
    AccountingPeriodClosed,
    SameAccount,
    InvalidAccountKinds,
    InvalidDescription,
    AmountOutOfRange,
    InvalidStatementSource,
    StatementNotBankMovement,
    StatementMovementMismatch,
    StatementAccountMismatch,
}

pub fn decide_record_account_movement(
    command: RecordAccountMovementCommand,
) -> Result<RecordAccountMovementDecision, AccountMovementError> {
    if command.amount.value() > 1_000_000_000_000 {
        return Err(AccountMovementError::AmountOutOfRange);
    }
    let trimmed_description = command.description.trim();
    if trimmed_description.len() > MAX_DESCRIPTION_BYTES {
        return Err(AccountMovementError::InvalidDescription);
    }
    if command.from_account_id == command.to_account_id {
        return Err(AccountMovementError::SameAccount);
    }
    match command.kind {
        AccountMovementKind::Transfer
            if command.from_account_kind == AccountKind::Card
                || command.to_account_kind == AccountKind::Card =>
        {
            return Err(AccountMovementError::InvalidAccountKinds);
        }
        AccountMovementKind::CardBillPayment
            if command.from_account_kind != AccountKind::Bank
                || command.to_account_kind != AccountKind::Card =>
        {
            return Err(AccountMovementError::InvalidAccountKinds);
        }
        _ => {}
    }

    if let Some(source) = &command.statement_source {
        if source.source_document_id.is_empty()
            || source.source_reference.is_empty()
            || source.source_reference.len() > 256
        {
            return Err(AccountMovementError::InvalidStatementSource);
        }
        let statement_account_is_bank = (source.statement_account_id == command.from_account_id
            && command.from_account_kind == AccountKind::Bank)
            || (source.statement_account_id == command.to_account_id
                && command.to_account_kind == AccountKind::Bank);
        if !statement_account_is_bank {
            return Err(AccountMovementError::StatementAccountMismatch);
        }
    }

    let description = if trimmed_description.is_empty() {
        command.kind.as_str().to_owned()
    } else {
        trimmed_description.to_owned()
    };
    Ok(RecordAccountMovementDecision {
        actor_id: command.actor_id,
        from_account_id: command.from_account_id.clone(),
        from_account_kind: command.from_account_kind,
        to_account_id: command.to_account_id.clone(),
        to_account_kind: command.to_account_kind,
        kind: command.kind,
        occurred_at_unix_micros: command.occurred_at_unix_micros,
        amount: command.amount,
        description,
        accounting_period: command.accounting_period,
        journal_lines: [
            AccountMovementJournalLine {
                line_no: 1,
                side: Side::Debit,
                account_id: command.to_account_id,
                amount: command.amount,
                memo: "Account movement destination",
            },
            AccountMovementJournalLine {
                line_no: 2,
                side: Side::Credit,
                account_id: command.from_account_id,
                amount: command.amount,
                memo: "Account movement source",
            },
        ],
        changed_fields: [
            "kind",
            "fromAccountId",
            "toAccountId",
            "amountYen",
            "occurredAt",
        ],
        statement_source: command.statement_source,
    })
}

pub fn decide_link_statement_movement(
    command: LinkStatementMovementCommand,
) -> Result<LinkStatementMovementDecision, AccountMovementError> {
    if command.movement_id.is_empty()
        || command.source_document_id.is_empty()
        || command.source_reference.is_empty()
        || command.source_reference.len() > 256
    {
        return Err(AccountMovementError::InvalidStatementSource);
    }
    if command.statement_account_kind != AccountKind::Bank {
        return Err(AccountMovementError::StatementAccountMismatch);
    }
    let source = StatementMovementSource {
        source_document_id: command.source_document_id,
        source_reference: command.source_reference,
        statement_account_id: command.statement_account_id,
    };
    validate_statement_movement(
        &source,
        &command.statement,
        command.kind,
        command.from_account_id.as_str(),
        command.to_account_id.as_str(),
        command.amount,
        command.occurred_at_unix_micros,
    )?;
    Ok(LinkStatementMovementDecision {
        source_document_id: source.source_document_id,
        source_reference: source.source_reference,
        statement_account_id: source.statement_account_id,
        movement_id: command.movement_id,
    })
}

fn validate_statement_movement(
    source: &StatementMovementSource,
    statement: &StatementMovementObservation,
    kind: AccountMovementKind,
    from_account_id: &str,
    to_account_id: &str,
    amount: PositiveYen,
    occurred_at_unix_micros: i64,
) -> Result<(), AccountMovementError> {
    if source.source_document_id.is_empty()
        || source.source_reference.is_empty()
        || source.source_reference.len() > 256
    {
        return Err(AccountMovementError::InvalidStatementSource);
    }
    if !statement.bank_statement {
        return Err(AccountMovementError::StatementNotBankMovement);
    }
    if !statement.has_region {
        return Err(AccountMovementError::StatementMovementMismatch);
    }
    let expected_kind = match kind {
        AccountMovementKind::Transfer => "ACCOUNT_TRANSFER",
        AccountMovementKind::CardBillPayment => "CARD_BILL_PAYMENT",
    };
    if statement.kind != expected_kind
        || statement.amount_yen == 0
        || statement.amount_yen.unsigned_abs() != amount.value() as u64
        || statement.amount_yen.unsigned_abs() > 1_000_000_000_000
        || statement.occurred_at_unix_micros != occurred_at_unix_micros
    {
        return Err(AccountMovementError::StatementMovementMismatch);
    }
    let statement_account_id = source.statement_account_id.as_str();
    match kind {
        AccountMovementKind::Transfer => {
            if statement.amount_yen < 0 && from_account_id != statement_account_id
                || statement.amount_yen > 0 && to_account_id != statement_account_id
                || from_account_id != statement_account_id && to_account_id != statement_account_id
            {
                return Err(AccountMovementError::StatementAccountMismatch);
            }
        }
        AccountMovementKind::CardBillPayment => {
            if statement.amount_yen >= 0 || statement_account_id != from_account_id {
                return Err(AccountMovementError::StatementAccountMismatch);
            }
        }
    }
    if from_account_id == to_account_id {
        return Err(AccountMovementError::SameAccount);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::PositiveYen;

    fn command(kind: AccountMovementKind) -> RecordAccountMovementCommand {
        RecordAccountMovementCommand {
            actor_id: MemberId::new("actor").unwrap(),
            from_account_id: AccountId::new("from").unwrap(),
            from_account_kind: AccountKind::Bank,
            to_account_id: AccountId::new("to").unwrap(),
            to_account_kind: if kind == AccountMovementKind::CardBillPayment {
                AccountKind::Card
            } else {
                AccountKind::Cash
            },
            kind,
            occurred_at_unix_micros: 1_791_128_400_000_000,
            amount: PositiveYen::new(500).unwrap(),
            description: "  transfer  ".to_owned(),
            accounting_period: OpenAccountingPeriod::new(
                crate::YearMonth::new(2025, 1).unwrap(),
                1,
            )
            .unwrap(),
            statement_source: None,
        }
    }

    #[test]
    fn transfer_builds_balanced_journal_lines() {
        let decision =
            decide_record_account_movement(command(AccountMovementKind::Transfer)).unwrap();
        assert_eq!(decision.journal_lines[0].side, Side::Debit);
        assert_eq!(decision.journal_lines[1].side, Side::Credit);
        assert_eq!(decision.description, "transfer");
    }

    #[test]
    fn card_bill_payment_requires_bank_to_card() {
        let mut input = command(AccountMovementKind::CardBillPayment);
        input.from_account_kind = AccountKind::Cash;
        assert_eq!(
            decide_record_account_movement(input),
            Err(AccountMovementError::InvalidAccountKinds)
        );
    }

    #[test]
    fn statement_transfer_checks_signed_direction_and_amount() {
        let statement = StatementMovementObservation {
            bank_statement: true,
            has_region: true,
            kind: "ACCOUNT_TRANSFER".to_owned(),
            amount_yen: -500,
            occurred_at_unix_micros: 1_791_128_400_000_000,
        };
        let command = LinkStatementMovementCommand {
            source_document_id: "document".to_owned(),
            source_reference: "row:1".to_owned(),
            statement_account_id: AccountId::new("from").unwrap(),
            statement_account_kind: AccountKind::Bank,
            movement_id: "movement".to_owned(),
            from_account_id: AccountId::new("from").unwrap(),
            to_account_id: AccountId::new("to").unwrap(),
            kind: AccountMovementKind::Transfer,
            amount: PositiveYen::new(500).unwrap(),
            occurred_at_unix_micros: statement.occurred_at_unix_micros,
            statement,
        };
        assert!(decide_link_statement_movement(command.clone()).is_ok());

        let mut mismatched_direction = command;
        mismatched_direction.statement.amount_yen = 500;
        assert_eq!(
            decide_link_statement_movement(mismatched_direction),
            Err(AccountMovementError::StatementAccountMismatch)
        );
    }
}
