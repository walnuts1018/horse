use bookkeeping_domain::PositiveYen;

use crate::{AccountId, MemberId, Side};

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum IncomeAccountKind {
    Bank,
    Cash,
    Card,
    Other,
}

impl IncomeAccountKind {
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
pub struct RecordAccountIncomeCommand {
    pub actor_id: MemberId,
    pub account_id: AccountId,
    pub account_kind: IncomeAccountKind,
    pub amount: PositiveYen,
    pub description: String,
    pub accounting_period_open: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountIncomeJournalLine {
    pub line_no: u32,
    pub side: Side,
    pub account_id: Option<AccountId>,
    pub amount: PositiveYen,
    pub memo: &'static str,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RecordAccountIncomeDecision {
    pub actor_id: MemberId,
    pub account_id: AccountId,
    pub account_kind: IncomeAccountKind,
    pub amount: PositiveYen,
    pub description: String,
    pub journal_lines: [AccountIncomeJournalLine; 2],
    pub changed_fields: [&'static str; 3],
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AccountIncomeError {
    AccountingPeriodClosed,
    CardAccountNotAllowed,
    InvalidDescription,
    AmountOutOfRange,
}

pub fn decide_record_account_income(
    command: RecordAccountIncomeCommand,
) -> Result<RecordAccountIncomeDecision, AccountIncomeError> {
    if !command.accounting_period_open {
        return Err(AccountIncomeError::AccountingPeriodClosed);
    }
    if command.amount.value() > 1_000_000_000_000 {
        return Err(AccountIncomeError::AmountOutOfRange);
    }
    let trimmed_description = command.description.trim();
    if trimmed_description.len() > 240 {
        return Err(AccountIncomeError::InvalidDescription);
    }
    if command.account_kind == IncomeAccountKind::Card {
        return Err(AccountIncomeError::CardAccountNotAllowed);
    }
    let description = if trimmed_description.is_empty() {
        "External income".to_owned()
    } else {
        trimmed_description.to_owned()
    };
    Ok(RecordAccountIncomeDecision {
        actor_id: command.actor_id,
        account_id: command.account_id.clone(),
        account_kind: command.account_kind,
        amount: command.amount,
        description,
        journal_lines: [
            AccountIncomeJournalLine {
                line_no: 1,
                side: Side::Debit,
                account_id: Some(command.account_id),
                amount: command.amount,
                memo: "External income",
            },
            AccountIncomeJournalLine {
                line_no: 2,
                side: Side::Credit,
                account_id: None,
                amount: command.amount,
                memo: "External income offset",
            },
        ],
        changed_fields: ["amountYen", "accountId", "occurredAt"],
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use bookkeeping_domain::PositiveYen;

    fn command(account_kind: IncomeAccountKind) -> RecordAccountIncomeCommand {
        RecordAccountIncomeCommand {
            actor_id: MemberId::new("actor").unwrap(),
            account_id: AccountId::new("account").unwrap(),
            account_kind,
            amount: PositiveYen::new(500).unwrap(),
            description: String::new(),
            accounting_period_open: true,
        }
    }

    #[test]
    fn income_debits_account_and_credits_offset() {
        let decision = decide_record_account_income(command(IncomeAccountKind::Bank)).unwrap();
        assert_eq!(decision.journal_lines[0].side, Side::Debit);
        assert_eq!(decision.journal_lines[1].side, Side::Credit);
        assert_eq!(decision.description, "External income");
    }

    #[test]
    fn income_rejects_card_accounts() {
        assert_eq!(
            decide_record_account_income(command(IncomeAccountKind::Card)),
            Err(AccountIncomeError::CardAccountNotAllowed)
        );
    }
}
