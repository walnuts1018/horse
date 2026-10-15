use std::collections::HashSet;

use bookkeeping_domain::{AllocationShares, PositiveYen, Yen, allocate};

use crate::{AccountId, ItemId, PoolId, PurchaseId, PurchaseStatus, Side};

const MAX_CORRECTION_YEN: i64 = 1_000_000_000_000;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SignedYenDelta(i64);

impl SignedYenDelta {
    pub fn new(value: i64) -> Result<Self, CorrectPurchaseError> {
        if value == 0 || !(-MAX_CORRECTION_YEN..=MAX_CORRECTION_YEN).contains(&value) {
            return Err(CorrectPurchaseError::InvalidAmountDelta);
        }
        Ok(Self(value))
    }

    pub const fn value(&self) -> i64 {
        self.0
    }

    fn magnitude(&self) -> Yen {
        Yen::new(self.0.abs()).expect("correction magnitude is within the Yen range")
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CorrectionTarget {
    Purchase,
    Item(ItemId),
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum OriginalPeriodStatus {
    Open,
    Closed,
    Invalid,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CorrectionPayment {
    pub account_id: AccountId,
    pub amount: PositiveYen,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CorrectionCommand {
    pub purchase_id: PurchaseId,
    pub purchase_status: PurchaseStatus,
    pub target: CorrectionTarget,
    pub amount_delta: SignedYenDelta,
    pub reason: String,
    pub original_period: String,
    pub original_period_status: OriginalPeriodStatus,
    pub effective_period: String,
    pub effective_period_open: bool,
    pub purchase_refundable: Yen,
    pub item_refundable: Option<Yen>,
    pub saved_allocations: AllocationShares,
    pub requested_allocations: Option<AllocationShares>,
    pub payments: Vec<CorrectionPayment>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CorrectPurchaseDecision {
    pub purchase_id: PurchaseId,
    pub target: CorrectionTarget,
    pub amount_delta: SignedYenDelta,
    pub reason: String,
    pub effective_period: String,
    pub allocations: Vec<CorrectionAllocation>,
    pub journal_lines: Vec<CorrectionJournalLine>,
    pub event: CorrectionEvent,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CorrectionJournalLine {
    pub line_no: u32,
    pub side: Side,
    pub pool_id: Option<PoolId>,
    pub account_id: Option<AccountId>,
    pub amount: PositiveYen,
    pub memo: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CorrectionAllocation {
    pub pool_id: PoolId,
    pub amount: Yen,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CorrectionEvent {
    pub event_type: &'static str,
    pub purchase_id: PurchaseId,
    pub changed_fields: Vec<&'static str>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CorrectPurchaseError {
    InvalidAmountDelta,
    BlankReason,
    InvalidAccountingPeriod,
    InvalidOriginalPeriodState,
    EffectivePeriodMismatch,
    PurchaseNotPosted,
    AccountingPeriodClosed,
    RefundExceedsPurchaseBalance,
    RefundExceedsItemBalance,
    MissingItemBalance,
    RequestedAllocationDiffers,
    InvalidPaymentAllocations,
    PaymentTotalMismatch,
    JournalLineCountOverflow,
}

pub fn decide_correct_purchase(
    command: CorrectionCommand,
) -> Result<CorrectPurchaseDecision, CorrectPurchaseError> {
    if command.amount_delta.value() == 0 || command.amount_delta.value().abs() > MAX_CORRECTION_YEN
    {
        return Err(CorrectPurchaseError::InvalidAmountDelta);
    }
    if command.reason.trim().is_empty() {
        return Err(CorrectPurchaseError::BlankReason);
    }
    if !valid_period(&command.original_period) || !valid_period(&command.effective_period) {
        return Err(CorrectPurchaseError::InvalidAccountingPeriod);
    }
    match command.original_period_status {
        OriginalPeriodStatus::Open if command.effective_period != command.original_period => {
            return Err(CorrectPurchaseError::EffectivePeriodMismatch);
        }
        OriginalPeriodStatus::Closed if command.effective_period == command.original_period => {
            return Err(CorrectPurchaseError::EffectivePeriodMismatch);
        }
        OriginalPeriodStatus::Invalid => {
            return Err(CorrectPurchaseError::InvalidOriginalPeriodState);
        }
        OriginalPeriodStatus::Open | OriginalPeriodStatus::Closed => {}
    }
    if command.purchase_status != PurchaseStatus::Posted {
        return Err(CorrectPurchaseError::PurchaseNotPosted);
    }
    if !command.effective_period_open {
        return Err(CorrectPurchaseError::AccountingPeriodClosed);
    }

    let delta = command.amount_delta.value();
    if delta < 0 {
        let refund = command.amount_delta.magnitude().value();
        if refund > command.purchase_refundable.value() {
            return Err(CorrectPurchaseError::RefundExceedsPurchaseBalance);
        }
        if matches!(&command.target, CorrectionTarget::Item(_)) {
            let item_balance = command
                .item_refundable
                .ok_or(CorrectPurchaseError::MissingItemBalance)?;
            if refund > item_balance.value() {
                return Err(CorrectPurchaseError::RefundExceedsItemBalance);
            }
        }
    }

    if let Some(requested) = &command.requested_allocations
        && !same_allocation_weights(requested, &command.saved_allocations)
    {
        return Err(CorrectPurchaseError::RequestedAllocationDiffers);
    }

    let payment_total = command
        .payments
        .iter()
        .try_fold(0_i64, |total, payment| {
            total.checked_add(payment.amount.value())
        })
        .ok_or(CorrectPurchaseError::InvalidPaymentAllocations)?;
    let correction_amount = command.amount_delta.magnitude();
    if !command.payments.is_empty() && payment_total != correction_amount.value() {
        return Err(CorrectPurchaseError::PaymentTotalMismatch);
    }
    let mut account_ids = HashSet::with_capacity(command.payments.len());
    if command
        .payments
        .iter()
        .any(|payment| !account_ids.insert(&payment.account_id))
    {
        return Err(CorrectPurchaseError::InvalidPaymentAllocations);
    }

    let allocations: Vec<CorrectionAllocation> =
        allocate(correction_amount, &command.saved_allocations)
            .into_iter()
            .map(|allocation| CorrectionAllocation {
                pool_id: allocation.pool_id().clone(),
                amount: allocation.amount(),
            })
            .collect();

    let (pool_side, payment_side) = if delta > 0 {
        (Side::Debit, Side::Credit)
    } else {
        (Side::Credit, Side::Debit)
    };
    let mut journal_lines =
        Vec::with_capacity(command.saved_allocations.weights().len() + command.payments.len() + 1);
    for allocation in &allocations {
        if allocation.amount.value() == 0 {
            continue;
        }
        journal_lines.push(CorrectionJournalLine {
            line_no: next_line_no(journal_lines.len())?,
            side: pool_side,
            pool_id: Some(allocation.pool_id.clone()),
            account_id: None,
            amount: PositiveYen::new(allocation.amount.value()).expect("nonzero allocation"),
            memo: command.reason.clone(),
        });
    }
    if command.payments.is_empty() {
        journal_lines.push(CorrectionJournalLine {
            line_no: next_line_no(journal_lines.len())?,
            side: payment_side,
            pool_id: None,
            account_id: None,
            amount: PositiveYen::new(correction_amount.value()).expect("correction is nonzero"),
            memo: "Correction offset".into(),
        });
    } else {
        for payment in &command.payments {
            journal_lines.push(CorrectionJournalLine {
                line_no: next_line_no(journal_lines.len())?,
                side: payment_side,
                pool_id: None,
                account_id: Some(payment.account_id.clone()),
                amount: payment.amount,
                memo: "Correction payment".into(),
            });
        }
    }

    Ok(CorrectPurchaseDecision {
        purchase_id: command.purchase_id.clone(),
        target: command.target,
        amount_delta: command.amount_delta,
        reason: command.reason,
        effective_period: command.effective_period,
        allocations,
        journal_lines,
        event: CorrectionEvent {
            event_type: "purchase.corrected",
            purchase_id: command.purchase_id,
            changed_fields: vec!["correction"],
        },
    })
}

fn valid_period(value: &str) -> bool {
    let Some((year, month)) = value.split_once('-') else {
        return false;
    };
    year.len() == 4
        && year.bytes().all(|byte| byte.is_ascii_digit())
        && month.len() == 2
        && month.bytes().all(|byte| byte.is_ascii_digit())
        && (1..=12).contains(&month.parse::<u8>().unwrap_or(0))
}

fn same_allocation_weights(left: &AllocationShares, right: &AllocationShares) -> bool {
    if left.weights().len() != right.weights().len() {
        return false;
    }
    let left_gcd = left
        .weights()
        .iter()
        .map(|weight| weight.value())
        .reduce(gcd)
        .unwrap_or(0);
    let right_gcd = right
        .weights()
        .iter()
        .map(|weight| weight.value())
        .reduce(gcd)
        .unwrap_or(0);
    left.weights().iter().all(|weight| {
        right.weights().iter().any(|other| {
            weight.pool_id() == other.pool_id()
                && weight.value() / left_gcd == other.value() / right_gcd
        })
    })
}

fn gcd(mut left: u64, mut right: u64) -> u64 {
    while right != 0 {
        (left, right) = (right, left % right);
    }
    left
}

fn next_line_no(current_count: usize) -> Result<u32, CorrectPurchaseError> {
    u32::try_from(current_count + 1).map_err(|_| CorrectPurchaseError::JournalLineCountOverflow)
}

#[cfg(test)]
mod tests {
    use bookkeeping_domain::{AllocationWeight, PoolId};

    use super::*;

    fn id<T>(new: impl FnOnce(String) -> Result<T, crate::IdentifierError>, value: &str) -> T {
        new(value.to_owned()).unwrap()
    }

    fn shares() -> AllocationShares {
        AllocationShares::new(vec![
            AllocationWeight::new(PoolId::new("pool-b").unwrap(), 1).unwrap(),
            AllocationWeight::new(PoolId::new("pool-a").unwrap(), 1).unwrap(),
        ])
        .unwrap()
    }

    fn command(delta: i64) -> CorrectionCommand {
        CorrectionCommand {
            purchase_id: id(PurchaseId::new, "purchase-1"),
            purchase_status: PurchaseStatus::Posted,
            target: CorrectionTarget::Item(id(ItemId::new, "item-1")),
            amount_delta: SignedYenDelta::new(delta).unwrap(),
            reason: "refund".into(),
            original_period: "2026-10".into(),
            original_period_status: OriginalPeriodStatus::Open,
            effective_period: "2026-10".into(),
            effective_period_open: true,
            purchase_refundable: Yen::new(100).unwrap(),
            item_refundable: Some(Yen::new(80).unwrap()),
            saved_allocations: shares(),
            requested_allocations: None,
            payments: Vec::new(),
        }
    }

    #[test]
    fn refund_decision_preserves_amount_and_balances_the_journal() {
        let decision = decide_correct_purchase(command(-31)).unwrap();

        assert_eq!(
            decision
                .allocations
                .iter()
                .map(|row| row.amount.value())
                .sum::<i64>(),
            31
        );
        assert_eq!(decision.journal_lines.len(), 3);
        assert_eq!(decision.journal_lines[0].side, Side::Credit);
        assert_eq!(decision.journal_lines[2].side, Side::Debit);
        assert_eq!(decision.journal_lines[0].line_no, 1);
        assert_eq!(decision.journal_lines[2].line_no, 3);
        let credits = decision
            .journal_lines
            .iter()
            .filter(|line| line.side == Side::Credit)
            .map(|line| line.amount.value())
            .sum::<i64>();
        let debits = decision
            .journal_lines
            .iter()
            .filter(|line| line.side == Side::Debit)
            .map(|line| line.amount.value())
            .sum::<i64>();
        assert_eq!(credits, debits);
        assert_eq!(decision.event.event_type, "purchase.corrected");
    }

    #[test]
    fn correction_rejects_refunds_beyond_purchase_or_item_balance() {
        let mut purchase_overdraw = command(-101);
        purchase_overdraw.item_refundable = Some(Yen::new(200).unwrap());
        assert_eq!(
            decide_correct_purchase(purchase_overdraw),
            Err(CorrectPurchaseError::RefundExceedsPurchaseBalance)
        );

        let mut item_overdraw = command(-81);
        item_overdraw.purchase_refundable = Yen::new(200).unwrap();
        assert_eq!(
            decide_correct_purchase(item_overdraw),
            Err(CorrectPurchaseError::RefundExceedsItemBalance)
        );
    }

    #[test]
    fn correction_requires_posted_purchase_open_period_and_matching_payment_total() {
        let mut draft = command(50);
        draft.purchase_status = PurchaseStatus::Draft;
        assert_eq!(
            decide_correct_purchase(draft),
            Err(CorrectPurchaseError::PurchaseNotPosted)
        );

        let mut closed = command(50);
        closed.effective_period_open = false;
        assert_eq!(
            decide_correct_purchase(closed),
            Err(CorrectPurchaseError::AccountingPeriodClosed)
        );

        let mut mismatched_period = command(50);
        mismatched_period.original_period_status = OriginalPeriodStatus::Closed;
        assert_eq!(
            decide_correct_purchase(mismatched_period),
            Err(CorrectPurchaseError::EffectivePeriodMismatch)
        );

        let mut mismatched_payments = command(50);
        mismatched_payments.payments.push(CorrectionPayment {
            account_id: id(AccountId::new, "account-1"),
            amount: PositiveYen::new(49).unwrap(),
        });
        assert_eq!(
            decide_correct_purchase(mismatched_payments),
            Err(CorrectPurchaseError::PaymentTotalMismatch)
        );
    }
}
