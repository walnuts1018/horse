use std::collections::HashSet;

use crate::{
    AccountId, AccountingPeriod, AccountingPeriodStatus, AllocationShares, CorrectionJournalTarget,
    ItemId, OpenAccountingPeriod, PoolId, PositiveYen, PurchaseId, PurchaseLifecycle, Side, Yen,
    allocate,
};

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

    pub fn magnitude(&self) -> Yen {
        Yen::new(self.0.abs()).expect("correction magnitude is within the Yen range")
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CorrectionTarget {
    Purchase,
    Item(ItemId),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PostedPurchaseReference(PurchaseId);

impl PostedPurchaseReference {
    pub fn new(id: PurchaseId, lifecycle: PurchaseLifecycle) -> Result<Self, CorrectPurchaseError> {
        match lifecycle {
            PurchaseLifecycle::Posted => Ok(Self(id)),
            PurchaseLifecycle::Draft => Err(CorrectPurchaseError::PurchaseNotPosted),
        }
    }

    pub fn id(&self) -> &PurchaseId {
        &self.0
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CorrectionPayment {
    pub account_id: AccountId,
    pub amount: PositiveYen,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CorrectionCommand {
    pub purchase: PostedPurchaseReference,
    pub target: CorrectionTarget,
    pub amount_delta: SignedYenDelta,
    pub reason: String,
    pub original_period: AccountingPeriod,
    pub effective_period: OpenAccountingPeriod,
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
    pub original_period: AccountingPeriod,
    pub effective_period: OpenAccountingPeriod,
    pub allocations: Vec<CorrectionAllocation>,
    pub journal_lines: Vec<CorrectionJournalLine>,
    pub event: CorrectionEvent,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CorrectionJournalLine {
    pub line_no: u32,
    pub side: Side,
    pub target: CorrectionJournalTarget,
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
    match command.original_period.status() {
        AccountingPeriodStatus::Open
            if command.effective_period.year_month() != command.original_period.year_month() =>
        {
            return Err(CorrectPurchaseError::EffectivePeriodMismatch);
        }
        AccountingPeriodStatus::Closed
            if command.effective_period.year_month() == command.original_period.year_month() =>
        {
            return Err(CorrectPurchaseError::EffectivePeriodMismatch);
        }
        AccountingPeriodStatus::Open | AccountingPeriodStatus::Closed => {}
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
            target: CorrectionJournalTarget::Pool(allocation.pool_id.clone()),
            amount: PositiveYen::new(allocation.amount.value()).expect("nonzero allocation"),
            memo: command.reason.clone(),
        });
    }
    if command.payments.is_empty() {
        journal_lines.push(CorrectionJournalLine {
            line_no: next_line_no(journal_lines.len())?,
            side: payment_side,
            target: CorrectionJournalTarget::Offset,
            amount: PositiveYen::new(correction_amount.value()).expect("correction is nonzero"),
            memo: "Correction offset".into(),
        });
    } else {
        for payment in &command.payments {
            journal_lines.push(CorrectionJournalLine {
                line_no: next_line_no(journal_lines.len())?,
                side: payment_side,
                target: CorrectionJournalTarget::Account(payment.account_id.clone()),
                amount: payment.amount,
                memo: "Correction payment".into(),
            });
        }
    }

    Ok(CorrectPurchaseDecision {
        purchase_id: command.purchase.id().clone(),
        target: command.target,
        amount_delta: command.amount_delta,
        reason: command.reason,
        original_period: command.original_period,
        effective_period: command.effective_period,
        allocations,
        journal_lines,
        event: CorrectionEvent {
            event_type: "purchase.corrected",
            purchase_id: command.purchase.id().clone(),
            changed_fields: vec!["correction"],
        },
    })
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
