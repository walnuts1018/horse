use bookkeeping_domain::PositiveYen;

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
    pub amount: PositiveYen,
    pub description: String,
    pub accounting_period_open: bool,
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
    pub amount: PositiveYen,
    pub description: String,
    pub journal_lines: [AccountMovementJournalLine; 2],
    pub changed_fields: [&'static str; 5],
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AccountMovementError {
    AccountingPeriodClosed,
    SameAccount,
    InvalidAccountKinds,
    InvalidDescription,
    AmountOutOfRange,
}

pub fn decide_record_account_movement(
    command: RecordAccountMovementCommand,
) -> Result<RecordAccountMovementDecision, AccountMovementError> {
    if !command.accounting_period_open {
        return Err(AccountMovementError::AccountingPeriodClosed);
    }
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
        amount: command.amount,
        description,
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
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use bookkeeping_domain::PositiveYen;

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
            amount: PositiveYen::new(500).unwrap(),
            description: "  transfer  ".to_owned(),
            accounting_period_open: true,
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
}
