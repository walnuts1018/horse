use bookkeeping_domain::{AccountingPeriod, AccountingPeriodStatus};
use chrono::{DateTime, Utc};

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
    pub accounting_period: AccountingPeriod,
    pub source_account: Option<FundingAccount>,
    pub destination_account: Option<FundingAccount>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FundingCommandContext {
    pub accounting_period: AccountingPeriod,
    pub pool_kind: FundingPoolKind,
    pub source_account: Option<FundingAccount>,
    pub destination_account: Option<FundingAccount>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RecordFundingWorkflowCommand {
    pub record: RecordFundingCommand,
    pub household_id: String,
    pub occurred_at: DateTime<Utc>,
    pub idempotency_key: String,
    pub request_fingerprint: [u8; 32],
    pub source_account_id: Option<String>,
    pub destination_account_id: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RecordedFunding {
    pub journal_id: String,
    pub ledger_entry_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum RecordFundingWorkflowError<E> {
    Decision(RecordFundingError),
    Operations(E),
}

pub trait RecordFundingOperations {
    type Error;

    async fn replay(
        &mut self,
        command: &RecordFundingWorkflowCommand,
    ) -> Result<Option<RecordedFunding>, Self::Error>;

    async fn load(
        &mut self,
        command: &RecordFundingWorkflowCommand,
    ) -> Result<FundingCommandContext, Self::Error>;

    async fn persist(
        &mut self,
        command: &RecordFundingWorkflowCommand,
        decision: RecordFundingDecision,
    ) -> Result<RecordedFunding, Self::Error>;
}

pub async fn record_funding<O: RecordFundingOperations>(
    operations: &mut O,
    command: RecordFundingWorkflowCommand,
) -> Result<RecordedFunding, RecordFundingWorkflowError<O::Error>> {
    if let Some(result) = operations
        .replay(&command)
        .await
        .map_err(RecordFundingWorkflowError::Operations)?
    {
        return Ok(result);
    }
    let context = operations
        .load(&command)
        .await
        .map_err(RecordFundingWorkflowError::Operations)?;
    let mut record = command.record.clone();
    record.accounting_period = context.accounting_period;
    record.source_account = context.source_account;
    record.destination_account = context.destination_account;
    let decision = decide_record_funding(record).map_err(RecordFundingWorkflowError::Decision)?;
    operations
        .persist(&command, decision)
        .await
        .map_err(RecordFundingWorkflowError::Operations)
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
    pub accounting_period: AccountingPeriod,
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
    if command.accounting_period.status() == AccountingPeriodStatus::Closed {
        return Err(RecordFundingError::AccountingPeriodClosed);
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
    use super::{
        FundingAccount, FundingAccountKind, FundingPoolKind, PostingSide, RecordFundingCommand,
        decide_record_funding,
    };
    use bookkeeping_domain::{AccountingPeriod, YearMonth};

    fn command() -> RecordFundingCommand {
        RecordFundingCommand {
            actor_id: "actor-1".into(),
            member_id: "member-1".into(),
            pool_id: "pool-1".into(),
            amount_yen: 12_345,
            accounting_period: AccountingPeriod::open(YearMonth::new(2026, 10).unwrap(), 1)
                .unwrap(),
            source_account: None,
            destination_account: None,
        }
    }

    fn assert_balanced_and_preserves_amount(decision: &super::RecordFundingDecision) {
        let debits = decision
            .journal_lines
            .iter()
            .filter(|line| line.side == PostingSide::Debit)
            .map(|line| line.amount_yen)
            .sum::<i64>();
        let credits = decision
            .journal_lines
            .iter()
            .filter(|line| line.side == PostingSide::Credit)
            .map(|line| line.amount_yen)
            .sum::<i64>();
        let journal_pairs = i64::try_from(decision.journal_lines.len() / 2).unwrap();

        assert_eq!(decision.amount_yen, 12_345);
        assert_eq!(debits, credits);
        assert_eq!(debits, decision.amount_yen * journal_pairs);
        assert!(
            decision
                .journal_lines
                .iter()
                .all(|line| line.amount_yen == decision.amount_yen)
        );
    }

    #[test]
    fn funding_without_account_movement_balances_and_preserves_amount() {
        let decision = decide_record_funding(command()).unwrap();

        assert_eq!(decision.journal_lines.len(), 2);
        assert_balanced_and_preserves_amount(&decision);
    }

    #[test]
    fn funding_with_account_movement_balances_and_preserves_amount() {
        let mut command = command();
        command.source_account = Some(FundingAccount {
            id: "member-account".into(),
            pool_id: None,
            kind: FundingAccountKind::Bank,
            pool_kind: Some(FundingPoolKind::Personal),
            personal_owner_id: Some("member-1".into()),
        });
        command.destination_account = Some(FundingAccount {
            id: "shared-account".into(),
            pool_id: Some("pool-1".into()),
            kind: FundingAccountKind::Bank,
            pool_kind: Some(FundingPoolKind::Shared),
            personal_owner_id: None,
        });

        let decision = decide_record_funding(command).unwrap();

        assert_eq!(decision.journal_lines.len(), 4);
        assert_balanced_and_preserves_amount(&decision);
    }
}
