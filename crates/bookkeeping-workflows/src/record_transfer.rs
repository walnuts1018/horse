use bookkeeping_domain::{PoolId, PositiveYen, Yen};

use crate::{AccountId, MemberId};

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TransferKind {
    Advance,
    Loan,
    Repayment,
    Gift,
}

impl TransferKind {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Advance => "ADVANCE",
            Self::Loan => "LOAN",
            Self::Repayment => "REPAYMENT",
            Self::Gift => "GIFT",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AccountingPeriodStatus {
    Open,
    Closed,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PoolKind {
    Personal,
    Shared,
    Other,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TransferAccount {
    pub id: AccountId,
    pub pool_id: PoolId,
    pub pool_kind: PoolKind,
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct LoanId(String);

impl LoanId {
    pub fn new(value: impl Into<String>) -> Result<Self, RecordTransferError> {
        let value = value.into();
        if value.is_empty() {
            return Err(RecordTransferError::EmptyLoanId);
        }
        Ok(Self(value))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LoanBasis {
    pub id: LoanId,
    pub lender_id: MemberId,
    pub borrower_id: MemberId,
    pub principal_yen: PositiveYen,
    pub repaid_yen: Yen,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TransferBase {
    pub actor_id: MemberId,
    pub from_member_id: MemberId,
    pub to_member_id: MemberId,
    pub amount: PositiveYen,
    pub description: String,
    pub period_status: AccountingPeriodStatus,
    pub payment_account: Option<TransferAccount>,
    pub destination_account: Option<TransferAccount>,
}

// 操作の種類ごとに必要な入力を区別する。返済にはLoanBasisが必須で、それ以外の操作にはLoanBasisを持たせられない。
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum RecordTransferCommand {
    Advance(TransferBase),
    Loan(TransferBase),
    Gift(TransferBase),
    Repayment { base: TransferBase, loan: LoanBasis },
}

impl RecordTransferCommand {
    pub const fn kind(&self) -> TransferKind {
        match self {
            Self::Advance(_) => TransferKind::Advance,
            Self::Loan(_) => TransferKind::Loan,
            Self::Gift(_) => TransferKind::Gift,
            Self::Repayment { .. } => TransferKind::Repayment,
        }
    }

    pub const fn base(&self) -> &TransferBase {
        match self {
            Self::Advance(base) | Self::Loan(base) | Self::Gift(base) => base,
            Self::Repayment { base, .. } => base,
        }
    }

    pub const fn loan(&self) -> Option<&LoanBasis> {
        match self {
            Self::Repayment { loan, .. } => Some(loan),
            Self::Advance(_) | Self::Loan(_) | Self::Gift(_) => None,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PostingSide {
    Debit,
    Credit,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct JournalLine {
    pub line_no: u32,
    pub side: PostingSide,
    pub account_id: Option<AccountId>,
    pub pool_id: Option<PoolId>,
    pub member_id: Option<MemberId>,
    pub amount: PositiveYen,
    pub memo: &'static str,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SettlementAdjustment {
    pub member_id: MemberId,
    pub amount_yen: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TransferRecordedEvent {
    pub actor_id: MemberId,
    pub revision: u64,
    pub changed_fields: &'static [&'static str],
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RecordTransferDecision {
    pub actor_id: MemberId,
    pub from_member_id: MemberId,
    pub to_member_id: MemberId,
    pub kind: TransferKind,
    pub amount: PositiveYen,
    pub description: String,
    pub payment_account_id: Option<AccountId>,
    pub destination_account_id: Option<AccountId>,
    pub loan_id: Option<LoanId>,
    pub journal_lines: Vec<JournalLine>,
    pub settlement_adjustments: Vec<SettlementAdjustment>,
    pub event: TransferRecordedEvent,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RecordTransferError {
    AccountingPeriodClosed,
    SameParticipants,
    SameAccounts,
    LoanDirectionMismatch,
    RepaymentExceedsOutstanding,
    EmptyLoanId,
    AmountOverflow,
    JournalLineCountOverflow,
}

pub fn decide_record_transfer(
    command: RecordTransferCommand,
) -> Result<RecordTransferDecision, RecordTransferError> {
    let base = command.base();
    if base.period_status == AccountingPeriodStatus::Closed {
        return Err(RecordTransferError::AccountingPeriodClosed);
    }
    if base.from_member_id == base.to_member_id {
        return Err(RecordTransferError::SameParticipants);
    }
    if base
        .payment_account
        .as_ref()
        .zip(base.destination_account.as_ref())
        .is_some_and(|(payment, destination)| payment.id == destination.id)
    {
        return Err(RecordTransferError::SameAccounts);
    }

    if let RecordTransferCommand::Repayment { base, loan } = &command {
        if base.from_member_id != loan.borrower_id || base.to_member_id != loan.lender_id {
            return Err(RecordTransferError::LoanDirectionMismatch);
        }
        let outstanding = loan
            .principal_yen
            .value()
            .checked_sub(loan.repaid_yen.value())
            .ok_or(RecordTransferError::AmountOverflow)?;
        if outstanding < 0 || base.amount.value() > outstanding {
            return Err(RecordTransferError::RepaymentExceedsOutstanding);
        }
    }

    let journal_lines = build_journal_lines(&command)?;
    let settlement_adjustments = settlement_adjustments(&command);
    let kind = command.kind();
    let description = normalized_description(&command.base().description, kind);
    let payment_account_id = command
        .base()
        .payment_account
        .as_ref()
        .map(|account| account.id.clone());
    let destination_account_id = command
        .base()
        .destination_account
        .as_ref()
        .map(|account| account.id.clone());
    let loan_id = command.loan().cloned().map(|loan| loan.id);

    Ok(RecordTransferDecision {
        actor_id: command.base().actor_id.clone(),
        from_member_id: command.base().from_member_id.clone(),
        to_member_id: command.base().to_member_id.clone(),
        kind,
        amount: command.base().amount,
        description,
        payment_account_id,
        destination_account_id,
        loan_id,
        journal_lines,
        settlement_adjustments,
        event: TransferRecordedEvent {
            actor_id: command.base().actor_id.clone(),
            revision: 1,
            changed_fields: &["amountYen", "kind"],
        },
    })
}

fn build_journal_lines(
    command: &RecordTransferCommand,
) -> Result<Vec<JournalLine>, RecordTransferError> {
    let base = command.base();
    let kind = command.kind();
    let mut lines = vec![
        JournalLine {
            line_no: 1,
            side: PostingSide::Debit,
            account_id: None,
            pool_id: None,
            member_id: Some(base.to_member_id.clone()),
            amount: base.amount,
            memo: kind.as_str(),
        },
        JournalLine {
            line_no: 2,
            side: PostingSide::Credit,
            account_id: None,
            pool_id: None,
            member_id: Some(base.from_member_id.clone()),
            amount: base.amount,
            memo: kind.as_str(),
        },
    ];

    if let Some(account) = &base.payment_account {
        lines.push(JournalLine {
            line_no: next_line_no(&lines)?,
            side: PostingSide::Credit,
            account_id: None,
            pool_id: Some(account.pool_id.clone()),
            member_id: None,
            amount: base.amount,
            memo: "Transfer settlement",
        });
        lines.push(JournalLine {
            line_no: next_line_no(&lines)?,
            side: PostingSide::Debit,
            account_id: None,
            pool_id: None,
            member_id: None,
            amount: base.amount,
            memo: "Transfer settlement offset",
        });
        lines.push(JournalLine {
            line_no: next_line_no(&lines)?,
            side: PostingSide::Credit,
            account_id: Some(account.id.clone()),
            pool_id: None,
            member_id: None,
            amount: base.amount,
            memo: "Transfer source account",
        });
        lines.push(JournalLine {
            line_no: next_line_no(&lines)?,
            side: PostingSide::Debit,
            account_id: None,
            pool_id: None,
            member_id: None,
            amount: base.amount,
            memo: "Transfer source account offset",
        });
    }

    if let Some(account) = &base.destination_account {
        lines.push(JournalLine {
            line_no: next_line_no(&lines)?,
            side: PostingSide::Debit,
            account_id: Some(account.id.clone()),
            pool_id: None,
            member_id: None,
            amount: base.amount,
            memo: "Transfer destination account",
        });
        lines.push(JournalLine {
            line_no: next_line_no(&lines)?,
            side: PostingSide::Credit,
            account_id: None,
            pool_id: None,
            member_id: None,
            amount: base.amount,
            memo: "Transfer destination account offset",
        });
    }
    Ok(lines)
}

fn next_line_no(lines: &[JournalLine]) -> Result<u32, RecordTransferError> {
    u32::try_from(lines.len() + 1).map_err(|_| RecordTransferError::JournalLineCountOverflow)
}

fn settlement_adjustments(command: &RecordTransferCommand) -> Vec<SettlementAdjustment> {
    let base = command.base();
    match command.kind() {
        TransferKind::Gift => Vec::new(),
        TransferKind::Repayment
            if base
                .payment_account
                .as_ref()
                .is_some_and(|account| account.pool_kind == PoolKind::Shared) =>
        {
            vec![SettlementAdjustment {
                member_id: base.to_member_id.clone(),
                amount_yen: base.amount.value(),
            }]
        }
        TransferKind::Advance | TransferKind::Loan | TransferKind::Repayment => vec![
            SettlementAdjustment {
                member_id: base.from_member_id.clone(),
                amount_yen: -base.amount.value(),
            },
            SettlementAdjustment {
                member_id: base.to_member_id.clone(),
                amount_yen: base.amount.value(),
            },
        ],
    }
}

fn normalized_description(description: &str, kind: TransferKind) -> String {
    let description = description.trim();
    if description.is_empty() {
        kind.as_str().to_owned()
    } else {
        description.to_owned()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{AccountId, MemberId};

    fn member(value: &str) -> MemberId {
        MemberId::new(value).unwrap()
    }

    fn account(value: &str, pool: &str, pool_kind: PoolKind) -> TransferAccount {
        TransferAccount {
            id: AccountId::new(value).unwrap(),
            pool_id: PoolId::new(pool).unwrap(),
            pool_kind,
        }
    }

    fn base() -> TransferBase {
        TransferBase {
            actor_id: member("actor"),
            from_member_id: member("from"),
            to_member_id: member("to"),
            amount: PositiveYen::new(80).unwrap(),
            description: "  rent  ".into(),
            period_status: AccountingPeriodStatus::Open,
            payment_account: None,
            destination_account: None,
        }
    }

    fn command(kind: TransferKind) -> RecordTransferCommand {
        let base = base();
        match kind {
            TransferKind::Advance => RecordTransferCommand::Advance(base),
            TransferKind::Loan => RecordTransferCommand::Loan(base),
            TransferKind::Gift => RecordTransferCommand::Gift(base),
            TransferKind::Repayment => RecordTransferCommand::Repayment {
                base,
                loan: loan_basis(),
            },
        }
    }

    fn loan_basis() -> LoanBasis {
        LoanBasis {
            id: LoanId::new("loan-1").unwrap(),
            lender_id: member("from"),
            borrower_id: member("to"),
            principal_yen: PositiveYen::new(100).unwrap(),
            repaid_yen: Yen::new(20).unwrap(),
        }
    }

    #[test]
    fn transfer_builds_balanced_ordered_journal_and_normalizes_description() {
        let mut base = base();
        base.description = "  ".into();
        base.payment_account = Some(account("payment", "pool", PoolKind::Shared));
        base.destination_account = Some(account("destination", "pool", PoolKind::Personal));

        let decision = decide_record_transfer(RecordTransferCommand::Gift(base)).unwrap();

        assert_eq!(decision.description, "GIFT");
        assert_eq!(decision.journal_lines.len(), 8);
        assert_eq!(decision.settlement_adjustments, []);
        assert_eq!(
            decision
                .journal_lines
                .iter()
                .map(|line| line.line_no)
                .collect::<Vec<_>>(),
            (1..=8).collect::<Vec<_>>()
        );
        let debits = decision
            .journal_lines
            .iter()
            .filter(|line| line.side == PostingSide::Debit)
            .map(|line| line.amount.value())
            .sum::<i64>();
        let credits = decision
            .journal_lines
            .iter()
            .filter(|line| line.side == PostingSide::Credit)
            .map(|line| line.amount.value())
            .sum::<i64>();
        assert_eq!(debits, credits);
    }

    #[test]
    fn transfer_applies_loan_and_shared_repayment_adjustments() {
        let mut base = base();
        base.from_member_id = member("to");
        base.to_member_id = member("from");
        let decision =
            decide_record_transfer(RecordTransferCommand::Repayment { base, loan: loan_basis() })
                .unwrap();
        assert_eq!(decision.loan_id.as_ref().unwrap().as_str(), "loan-1");
        assert_eq!(
            decision
                .settlement_adjustments
                .iter()
                .map(|adjustment| (adjustment.member_id.as_str(), adjustment.amount_yen))
                .collect::<Vec<_>>(),
            vec![("to", -80), ("from", 80)]
        );

        let mut shared_base = base();
        shared_base.from_member_id = member("to");
        shared_base.to_member_id = member("from");
        shared_base.payment_account = Some(account("payment", "shared-pool", PoolKind::Shared));
        let decision = decide_record_transfer(RecordTransferCommand::Repayment {
            base: shared_base,
            loan: loan_basis(),
        })
        .unwrap();
        assert_eq!(decision.settlement_adjustments.len(), 1);
        assert_eq!(
            decision.settlement_adjustments[0].member_id.as_str(),
            "from"
        );
        assert_eq!(decision.settlement_adjustments[0].amount_yen, 80);
    }

    #[test]
    fn transfer_rejects_closed_period_invalid_repayment_and_same_accounts() {
        let mut closed = command(TransferKind::Gift);
        closed.period_status = AccountingPeriodStatus::Closed;
        assert_eq!(
            decide_record_transfer(closed),
            Err(RecordTransferError::AccountingPeriodClosed)
        );

        let mut no_loan = command(TransferKind::Repayment);
        assert_eq!(
            decide_record_transfer(no_loan.clone()),
            Err(RecordTransferError::RepaymentRequiresLoan)
        );
        no_loan.loan = Some(loan_basis());
        no_loan.from_member_id = member("from");
        no_loan.to_member_id = member("to");
        assert_eq!(
            decide_record_transfer(no_loan),
            Err(RecordTransferError::LoanDirectionMismatch)
        );

        let mut same_account = command(TransferKind::Gift);
        same_account.payment_account = Some(account("same", "pool", PoolKind::Personal));
        same_account.destination_account = same_account.payment_account.clone();
        assert_eq!(
            decide_record_transfer(same_account),
            Err(RecordTransferError::SameAccounts)
        );
    }

    #[test]
    fn repayment_cannot_exceed_outstanding_balance() {
        let mut input = command(TransferKind::Repayment);
        input.from_member_id = member("to");
        input.to_member_id = member("from");
        input.amount = PositiveYen::new(81).unwrap();
        input.loan = Some(loan_basis());
        assert_eq!(
            decide_record_transfer(input),
            Err(RecordTransferError::RepaymentExceedsOutstanding)
        );
    }
}
