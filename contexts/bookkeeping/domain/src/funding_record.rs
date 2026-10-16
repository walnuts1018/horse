use crate::OpenAccountingPeriod;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FundingAccountKind {
    Bank,
    Cash,
    Card,
    Other,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FundingPoolKind {
    Personal,
    Shared,
    Other,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FundingAccount {
    pub id: String,
    pub pool_id: Option<String>,
    pub kind: FundingAccountKind,
    pub pool_kind: Option<FundingPoolKind>,
    pub personal_owner_id: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RecordFundingCommand {
    pub actor_id: String,
    pub member_id: String,
    pub pool_id: String,
    pub amount_yen: i64,
    pub accounting_period: OpenAccountingPeriod,
    pub source_account: Option<FundingAccount>,
    pub destination_account: Option<FundingAccount>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FundingCommandContext {
    pub accounting_period: OpenAccountingPeriod,
    pub pool_kind: FundingPoolKind,
    pub source_account: Option<FundingAccount>,
    pub destination_account: Option<FundingAccount>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PostingSide {
    Debit,
    Credit,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FundingJournalLine {
    pub line_no: u32,
    pub side: PostingSide,
    pub account_id: Option<String>,
    pub pool_id: Option<String>,
    pub amount_yen: i64,
    pub memo: &'static str,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FundingRecordedEvent {
    pub actor_id: String,
    pub revision: u64,
    pub changed_fields: &'static [&'static str],
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RecordFundingDecision {
    pub actor_id: String,
    pub member_id: String,
    pub pool_id: String,
    pub amount_yen: i64,
    pub accounting_period: OpenAccountingPeriod,
    pub source_account_id: Option<String>,
    pub destination_account_id: Option<String>,
    pub journal_lines: Vec<FundingJournalLine>,
    pub event: FundingRecordedEvent,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RecordFundingError {
    InvalidAmount,
    InvalidIdentifier,
    AccountingPeriodClosed,
    CardAccount,
    DestinationPoolMismatch,
    PersonalFundingOwnerMismatch,
    NonPersonalFundingSource,
    FundingSourceAndDestinationPoolSame,
    SameAccounts,
    AmountOverflow,
}

pub fn decide_record_funding(
    command: RecordFundingCommand,
) -> Result<RecordFundingDecision, RecordFundingError> {
    if command.amount_yen <= 0 || command.amount_yen > 1_000_000_000_000 {
        return Err(RecordFundingError::InvalidAmount);
    }
    if command.actor_id.is_empty() || command.member_id.is_empty() || command.pool_id.is_empty() {
        return Err(RecordFundingError::InvalidIdentifier);
    }
    for account in [
        command.source_account.as_ref(),
        command.destination_account.as_ref(),
    ]
    .into_iter()
    .flatten()
    {
        if account.id.is_empty() {
            return Err(RecordFundingError::InvalidIdentifier);
        }
        if account.kind == FundingAccountKind::Card {
            return Err(RecordFundingError::CardAccount);
        }
    }
    if let Some(destination) = &command.destination_account
        && destination.pool_id.as_deref() != Some(command.pool_id.as_str())
    {
        return Err(RecordFundingError::DestinationPoolMismatch);
    }
    if let Some(source) = &command.source_account
        && source.pool_kind == Some(FundingPoolKind::Personal)
        && source.personal_owner_id.as_deref() != Some(command.member_id.as_str())
    {
        return Err(RecordFundingError::PersonalFundingOwnerMismatch);
    }
    if let Some(source) = &command.source_account {
        if source.pool_id.as_deref() == Some(command.pool_id.as_str()) {
            return Err(RecordFundingError::FundingSourceAndDestinationPoolSame);
        }
        if source.pool_id.is_some() && source.pool_kind != Some(FundingPoolKind::Personal) {
            return Err(RecordFundingError::NonPersonalFundingSource);
        }
    }
    if command
        .source_account
        .as_ref()
        .zip(command.destination_account.as_ref())
        .is_some_and(|(source, destination)| source.id == destination.id)
    {
        return Err(RecordFundingError::SameAccounts);
    }

    let amount = command.amount_yen;
    let pool_id = Some(command.pool_id.clone());
    let mut journal_lines = vec![
        FundingJournalLine {
            line_no: 1,
            side: PostingSide::Debit,
            account_id: None,
            pool_id: pool_id.clone(),
            amount_yen: amount,
            memo: "Pool funding",
        },
        FundingJournalLine {
            line_no: 2,
            side: PostingSide::Credit,
            account_id: None,
            pool_id,
            amount_yen: amount,
            memo: "Member contribution",
        },
    ];
    if command.source_account.is_some() || command.destination_account.is_some() {
        let next = u32::try_from(journal_lines.len() + 1)
            .map_err(|_| RecordFundingError::AmountOverflow)?;
        journal_lines.push(FundingJournalLine {
            line_no: next,
            side: PostingSide::Debit,
            account_id: command
                .destination_account
                .as_ref()
                .map(|account| account.id.clone()),
            pool_id: None,
            amount_yen: amount,
            memo: "Funding destination",
        });
        journal_lines.push(FundingJournalLine {
            line_no: next
                .checked_add(1)
                .ok_or(RecordFundingError::AmountOverflow)?,
            side: PostingSide::Credit,
            account_id: command
                .source_account
                .as_ref()
                .map(|account| account.id.clone()),
            pool_id: None,
            amount_yen: amount,
            memo: "Funding source",
        });
    }

    Ok(RecordFundingDecision {
        actor_id: command.actor_id.clone(),
        member_id: command.member_id,
        pool_id: command.pool_id,
        amount_yen: amount,
        accounting_period: command.accounting_period,
        source_account_id: command.source_account.map(|account| account.id),
        destination_account_id: command.destination_account.map(|account| account.id),
        journal_lines,
        event: FundingRecordedEvent {
            actor_id: command.actor_id,
            revision: 1,
            changed_fields: &["amountYen"],
        },
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{OpenAccountingPeriod, YearMonth};

    fn command(source_account: Option<FundingAccount>) -> RecordFundingCommand {
        RecordFundingCommand {
            actor_id: "actor".into(),
            member_id: "member-a".into(),
            pool_id: "shared-pool".into(),
            amount_yen: 1_000,
            accounting_period: OpenAccountingPeriod::new(YearMonth::new(2026, 10).unwrap(), 1)
                .unwrap(),
            source_account,
            destination_account: None,
        }
    }

    fn account(pool_id: Option<&str>, pool_kind: Option<FundingPoolKind>) -> FundingAccount {
        FundingAccount {
            id: "source-account".into(),
            pool_id: pool_id.map(str::to_owned),
            kind: FundingAccountKind::Bank,
            pool_kind,
            personal_owner_id: (pool_kind == Some(FundingPoolKind::Personal))
                .then(|| "member-a".into()),
        }
    }

    #[test]
    fn funding_rejects_shared_and_same_pool_sources() {
        assert_eq!(
            decide_record_funding(command(Some(account(
                Some("another-shared-pool"),
                Some(FundingPoolKind::Shared),
            )))),
            Err(RecordFundingError::NonPersonalFundingSource)
        );
        assert_eq!(
            decide_record_funding(command(Some(account(
                Some("shared-pool"),
                Some(FundingPoolKind::Personal),
            )))),
            Err(RecordFundingError::FundingSourceAndDestinationPoolSame)
        );
    }

    #[test]
    fn funding_accepts_member_owned_personal_and_external_sources() {
        assert!(
            decide_record_funding(command(Some(account(
                Some("personal-pool"),
                Some(FundingPoolKind::Personal),
            ))))
            .is_ok()
        );
        assert!(decide_record_funding(command(Some(account(None, None)))).is_ok());
    }
}
