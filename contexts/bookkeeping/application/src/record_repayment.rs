use crate::{AccountId, MemberId, PoolId, Side};
use bookkeeping_domain::{AccountingPeriod, AccountingPeriodStatus, PositiveYen, Yen};

const MAX_REPAYMENT_YEN: i64 = 1_000_000_000_000;

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct LoanId(String);

impl LoanId {
    pub fn new(value: impl Into<String>) -> Result<Self, RepaymentError> {
        let value = value.into();
        if value.is_empty() {
            return Err(RepaymentError::InvalidLoanId);
        }
        Ok(Self(value))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PoolKind {
    Personal,
    Shared,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RepaymentSource {
    pub account_id: AccountId,
    pub pool_id: PoolId,
    pub pool_kind: PoolKind,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Loan {
    pub id: LoanId,
    pub lender: MemberId,
    pub borrower: MemberId,
    pub principal: PositiveYen,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RecordRepaymentCommand {
    pub loan: Loan,
    pub repaid_so_far: Yen,
    pub from_member: MemberId,
    pub to_member: MemberId,
    pub amount: PositiveYen,
    pub accounting_period: AccountingPeriod,
    pub source: Option<RepaymentSource>,
    pub destination_account: Option<AccountId>,
    pub description: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RecordRepaymentDecision {
    pub loan_id: LoanId,
    pub amount: PositiveYen,
    pub accounting_period: AccountingPeriod,
    pub description: String,
    pub journal_lines: Vec<RepaymentJournalLine>,
    pub settlement_adjustments: Vec<RepaymentSettlementAdjustment>,
    pub event: RepaymentEvent,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RepaymentJournalLine {
    pub line_no: u32,
    pub side: Side,
    pub member_id: Option<MemberId>,
    pub pool_id: Option<PoolId>,
    pub account_id: Option<AccountId>,
    pub amount: PositiveYen,
    pub memo: &'static str,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RepaymentSettlementAdjustment {
    pub member_id: MemberId,
    pub amount_yen: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RepaymentEvent {
    pub event_type: &'static str,
    pub loan_id: LoanId,
    pub changed_fields: Vec<&'static str>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RepaymentError {
    InvalidLoanId,
    AccountingPeriodClosed,
    InvalidLoanParties,
    RepaymentDirectionMismatch,
    RepaidAmountExceedsPrincipal,
    RepaymentExceedsOutstandingPrincipal,
    AmountOutOfRange,
    SameSourceAndDestinationAccount,
    JournalLineCountOverflow,
}

pub fn decide_record_repayment(
    command: RecordRepaymentCommand,
) -> Result<RecordRepaymentDecision, RepaymentError> {
    if command.accounting_period.status() != AccountingPeriodStatus::Open {
        return Err(RepaymentError::AccountingPeriodClosed);
    }
    if command.loan.lender == command.loan.borrower {
        return Err(RepaymentError::InvalidLoanParties);
    }
    if command.from_member != command.loan.borrower || command.to_member != command.loan.lender {
        return Err(RepaymentError::RepaymentDirectionMismatch);
    }
    if command.repaid_so_far.value() > command.loan.principal.value() {
        return Err(RepaymentError::RepaidAmountExceedsPrincipal);
    }
    if command.amount.value() > MAX_REPAYMENT_YEN {
        return Err(RepaymentError::AmountOutOfRange);
    }
    let outstanding = command
        .loan
        .principal
        .value()
        .checked_sub(command.repaid_so_far.value())
        .ok_or(RepaymentError::RepaidAmountExceedsPrincipal)?;
    if command.amount.value() > outstanding {
        return Err(RepaymentError::RepaymentExceedsOutstandingPrincipal);
    }
    if command
        .source
        .as_ref()
        .zip(command.destination_account.as_ref())
        .is_some_and(|(source, destination)| source.account_id == *destination)
    {
        return Err(RepaymentError::SameSourceAndDestinationAccount);
    }

    let amount = command.amount;
    let mut journal_lines = vec![
        RepaymentJournalLine {
            line_no: 1,
            side: Side::Debit,
            member_id: Some(command.loan.lender.clone()),
            pool_id: None,
            account_id: None,
            amount,
            memo: "REPAYMENT",
        },
        RepaymentJournalLine {
            line_no: 2,
            side: Side::Credit,
            member_id: Some(command.loan.borrower.clone()),
            pool_id: None,
            account_id: None,
            amount,
            memo: "REPAYMENT",
        },
    ];
    if let Some(source) = &command.source {
        push_journal_pair(
            &mut journal_lines,
            Side::Credit,
            Side::Debit,
            amount,
            Some(source.pool_id.clone()),
            None,
            "Transfer settlement",
            "Transfer settlement offset",
        )?;
        push_journal_pair(
            &mut journal_lines,
            Side::Credit,
            Side::Debit,
            amount,
            None,
            Some(source.account_id.clone()),
            "Transfer source account",
            "Transfer source account offset",
        )?;
    }
    if let Some(destination) = &command.destination_account {
        push_journal_pair(
            &mut journal_lines,
            Side::Debit,
            Side::Credit,
            amount,
            None,
            Some(destination.clone()),
            "Transfer destination account",
            "Transfer destination account offset",
        )?;
    }

    let signed_amount = amount.value();
    let settlement_adjustments = match command.source.as_ref().map(|source| source.pool_kind) {
        Some(PoolKind::Shared) => vec![RepaymentSettlementAdjustment {
            member_id: command.loan.lender.clone(),
            amount_yen: signed_amount,
        }],
        _ => vec![
            RepaymentSettlementAdjustment {
                member_id: command.loan.borrower.clone(),
                amount_yen: -signed_amount,
            },
            RepaymentSettlementAdjustment {
                member_id: command.loan.lender.clone(),
                amount_yen: signed_amount,
            },
        ],
    };
    let description = if command.description.trim().is_empty() {
        "REPAYMENT".to_owned()
    } else {
        command.description.trim().to_owned()
    };

    Ok(RecordRepaymentDecision {
        loan_id: command.loan.id.clone(),
        amount,
        accounting_period: command.accounting_period,
        description,
        journal_lines,
        settlement_adjustments,
        event: RepaymentEvent {
            event_type: "transfer.recorded",
            loan_id: command.loan.id,
            changed_fields: vec!["amountYen", "kind"],
        },
    })
}

fn push_journal_pair(
    lines: &mut Vec<RepaymentJournalLine>,
    first_side: Side,
    second_side: Side,
    amount: PositiveYen,
    pool_id: Option<PoolId>,
    account_id: Option<AccountId>,
    first_memo: &'static str,
    second_memo: &'static str,
) -> Result<(), RepaymentError> {
    let first_line_no =
        u32::try_from(lines.len() + 1).map_err(|_| RepaymentError::JournalLineCountOverflow)?;
    let second_line_no = first_line_no
        .checked_add(1)
        .ok_or(RepaymentError::JournalLineCountOverflow)?;
    lines.push(RepaymentJournalLine {
        line_no: first_line_no,
        side: first_side,
        member_id: None,
        pool_id,
        account_id: account_id.clone(),
        amount,
        memo: first_memo,
    });
    lines.push(RepaymentJournalLine {
        line_no: second_line_no,
        side: second_side,
        member_id: None,
        pool_id: None,
        account_id: None,
        amount,
        memo: second_memo,
    });
    Ok(())
}

#[cfg(test)]
mod tests {
    use bookkeeping_domain::PositiveYen;

    use super::*;

    fn id<T>(new: impl FnOnce(String) -> Result<T, crate::IdentifierError>, value: &str) -> T {
        new(value.to_owned()).unwrap()
    }

    fn command(pool_kind: PoolKind) -> RecordRepaymentCommand {
        let lender = id(MemberId::new, "lender");
        let borrower = id(MemberId::new, "borrower");
        RecordRepaymentCommand {
            loan: Loan {
                id: LoanId::new("loan-1").unwrap(),
                lender: lender.clone(),
                borrower: borrower.clone(),
                principal: PositiveYen::new(1_000).unwrap(),
            },
            repaid_so_far: Yen::new(600).unwrap(),
            from_member: borrower,
            to_member: lender,
            amount: PositiveYen::new(400).unwrap(),
            accounting_period: AccountingPeriod::open(
                bookkeeping_domain::YearMonth::new(2026, 10).unwrap(),
                1,
            )
            .unwrap(),
            source: Some(RepaymentSource {
                account_id: id(AccountId::new, "source-account"),
                pool_id: PoolId::new("pool-1").unwrap(),
                pool_kind,
            }),
            destination_account: Some(id(AccountId::new, "destination-account")),
            description: "  Repayment  ".into(),
        }
    }

    #[test]
    fn repayment_produces_balanced_journal_and_pool_specific_settlement() {
        let decision = decide_record_repayment(command(PoolKind::Shared)).unwrap();

        assert_eq!(decision.journal_lines.len(), 8);
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
            .filter(|line| line.side == Side::Debit)
            .map(|line| line.amount.value())
            .sum::<i64>();
        let credits = decision
            .journal_lines
            .iter()
            .filter(|line| line.side == Side::Credit)
            .map(|line| line.amount.value())
            .sum::<i64>();
        assert_eq!(debits, credits);
        assert_eq!(decision.description, "Repayment");
        assert_eq!(decision.settlement_adjustments.len(), 1);
        assert_eq!(
            decision.settlement_adjustments[0].member_id.as_str(),
            "lender"
        );
        assert_eq!(decision.settlement_adjustments[0].amount_yen, 400);
    }

    #[test]
    fn repayment_rejects_wrong_direction_closed_period_and_overpayment() {
        let mut wrong_direction = command(PoolKind::Personal);
        wrong_direction.from_member = wrong_direction.loan.lender.clone();
        assert_eq!(
            decide_record_repayment(wrong_direction),
            Err(RepaymentError::RepaymentDirectionMismatch)
        );

        let mut closed_period = command(PoolKind::Personal);
        closed_period.accounting_period =
            AccountingPeriod::closed(bookkeeping_domain::YearMonth::new(2026, 10).unwrap(), 1)
                .unwrap();
        assert_eq!(
            decide_record_repayment(closed_period),
            Err(RepaymentError::AccountingPeriodClosed)
        );

        let mut overpayment = command(PoolKind::Personal);
        overpayment.amount = PositiveYen::new(401).unwrap();
        assert_eq!(
            decide_record_repayment(overpayment),
            Err(RepaymentError::RepaymentExceedsOutstandingPrincipal)
        );
    }

    #[test]
    fn personal_repayment_changes_both_members_settlement_balances() {
        let decision = decide_record_repayment(command(PoolKind::Personal)).unwrap();

        assert_eq!(decision.settlement_adjustments.len(), 2);
        assert_eq!(
            decision.settlement_adjustments[0].member_id.as_str(),
            "borrower"
        );
        assert_eq!(decision.settlement_adjustments[0].amount_yen, -400);
        assert_eq!(
            decision.settlement_adjustments[1].member_id.as_str(),
            "lender"
        );
        assert_eq!(decision.settlement_adjustments[1].amount_yen, 400);
    }
}
