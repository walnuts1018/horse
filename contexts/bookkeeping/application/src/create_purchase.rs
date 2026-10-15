use std::collections::BTreeSet;
use std::num::NonZeroU64;

use bookkeeping_domain::{AllocationShares, AllocationWeight, PoolId, Yen, allocate};
use chrono::{DateTime, Utc};

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct PurchaseOccurredAt(DateTime<Utc>);

impl PurchaseOccurredAt {
    pub fn new(value: DateTime<Utc>) -> Self {
        Self(value)
    }

    pub fn value(self) -> DateTime<Utc> {
        self.0
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum DatePrecision {
    DateTime,
    DateOnly,
}

impl DatePrecision {
    pub const fn as_db_value(self) -> &'static str {
        match self {
            Self::DateTime => "DATE_TIME",
            Self::DateOnly => "DATE_ONLY",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PurchaseOrigin {
    AiAssisted,
    Manual,
    Unknown,
}

impl PurchaseOrigin {
    pub const fn as_db_value(self) -> &'static str {
        match self {
            Self::AiAssisted => "AI",
            Self::Manual => "MANUAL",
            Self::Unknown => "UNKNOWN",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct AllocationRuleVersion(NonZeroU64);

impl AllocationRuleVersion {
    pub fn new(value: u64) -> Option<Self> {
        NonZeroU64::new(value).map(Self)
    }

    pub const fn value(self) -> u64 {
        self.0.get()
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum QuantityUnit {
    Piece,
    Bottle,
    Sheet,
    Bag,
    Box,
    Gram,
    Kilogram,
    Milliliter,
    Liter,
}

impl QuantityUnit {
    pub const fn as_db_value(self) -> &'static str {
        match self {
            Self::Piece => "個",
            Self::Bottle => "本",
            Self::Sheet => "枚",
            Self::Bag => "袋",
            Self::Box => "箱",
            Self::Gram => "g",
            Self::Kilogram => "kg",
            Self::Milliliter => "ml",
            Self::Liter => "L",
        }
    }

    pub fn parse(value: &str) -> Result<Self, MeasureError> {
        match value {
            "個" => Ok(Self::Piece),
            "本" => Ok(Self::Bottle),
            "枚" => Ok(Self::Sheet),
            "袋" => Ok(Self::Bag),
            "箱" => Ok(Self::Box),
            "g" => Ok(Self::Gram),
            "kg" => Ok(Self::Kilogram),
            "ml" => Ok(Self::Milliliter),
            "L" => Ok(Self::Liter),
            _ => Err(MeasureError::InvalidUnit),
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum MeasureError {
    InvalidMeasure,
    InvalidUnit,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreatePurchaseCommand {
    pub purchase_id: String,
    pub occurred_at: PurchaseOccurredAt,
    pub date_precision: DatePrecision,
    pub merchant: String,
    pub origin: PurchaseOrigin,
    pub allocation_rule_version: AllocationRuleVersion,
    pub source_document_id: Option<String>,
    pub source_reference: Option<String>,
    pub receipt_candidate_id: Option<String>,
    pub manual_amount_override_reason: Option<String>,
    pub items: Vec<CreatePurchaseItem>,
    pub payments: Vec<CreatePurchasePayment>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreatePurchaseItem {
    pub item_id: String,
    pub name: String,
    pub amount_yen: i64,
    pub category_id: Option<String>,
    pub product_variant_id: Option<String>,
    pub quantity: Option<Measure>,
    pub printed_unit_price: Option<PrintedUnitPrice>,
    pub allocation_override: bool,
    pub allocations: Vec<CreatePurchaseAllocation>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Measure {
    numerator: i64,
    denominator: i64,
    unit: QuantityUnit,
}

impl Measure {
    pub fn new(numerator: i64, denominator: i64, unit: QuantityUnit) -> Result<Self, MeasureError> {
        if numerator <= 0
            || numerator > 1_000_000_000_000
            || denominator <= 0
            || denominator > 1_000_000
            || gcd(numerator, denominator) != 1
        {
            return Err(MeasureError::InvalidMeasure);
        }
        Ok(Self {
            numerator,
            denominator,
            unit,
        })
    }

    pub const fn numerator(self) -> i64 {
        self.numerator
    }
    pub const fn denominator(self) -> i64 {
        self.denominator
    }
    pub const fn unit(self) -> QuantityUnit {
        self.unit
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PrintedUnitPrice {
    pub amount_yen: i64,
    pub basis: Measure,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreatePurchaseAllocation {
    pub pool_id: String,
    pub weight: i64,
    pub rule_version: u64,
    pub rule_id: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreatePurchasePayment {
    pub account_id: String,
    pub amount_yen: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreatePurchaseDecision {
    pub purchase_id: String,
    pub occurred_at: PurchaseOccurredAt,
    pub date_precision: DatePrecision,
    pub merchant: String,
    pub origin: PurchaseOrigin,
    pub allocation_rule_version: AllocationRuleVersion,
    pub source_document_id: Option<String>,
    pub source_reference: Option<String>,
    pub receipt_candidate_id: Option<String>,
    pub manual_amount_override_reason: Option<String>,
    pub items: Vec<DecidedPurchaseItem>,
    pub payments: Vec<CreatePurchasePayment>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DecidedPurchaseItem {
    pub item_id: String,
    pub name: String,
    pub amount_yen: i64,
    pub category_id: Option<String>,
    pub product_variant_id: Option<String>,
    pub quantity: Option<Measure>,
    pub printed_unit_price: Option<PrintedUnitPrice>,
    pub allocation_override: bool,
    pub allocations: Vec<DecidedPurchaseAllocation>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DecidedPurchaseAllocation {
    pub pool_id: String,
    pub amount_yen: i64,
    pub weight: i64,
    pub rule_version: u64,
    pub rule_id: Option<String>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CreatePurchaseError {
    InvalidPurchaseId,
    InvalidSourceReference,
    InvalidReceiptOverrideReason,
    InvalidMerchant,
    InvalidItemCount,
    InvalidItemId,
    InvalidItemName,
    InvalidAmount,
    InvalidAllocation,
    DuplicateAllocationPool,
    InvalidPayment,
    DuplicatePaymentAccount,
    AmountOverflow,
}

pub fn decide_create_purchase(
    command: CreatePurchaseCommand,
) -> Result<CreatePurchaseDecision, CreatePurchaseError> {
    if command.purchase_id.is_empty() {
        return Err(CreatePurchaseError::InvalidPurchaseId);
    }
    if let Some(reference) = &command.source_reference {
        if command.source_document_id.is_none()
            || reference.len() > 256
            || reference.contains(['\r', '\n'])
        {
            return Err(CreatePurchaseError::InvalidSourceReference);
        }
    }
    if command
        .manual_amount_override_reason
        .as_deref()
        .unwrap_or_default()
        .trim()
        .len()
        > 500
    {
        return Err(CreatePurchaseError::InvalidReceiptOverrideReason);
    }
    if command.items.is_empty() || command.items.len() > 200 {
        return Err(CreatePurchaseError::InvalidItemCount);
    }
    if command.merchant.chars().count() > 240 {
        return Err(CreatePurchaseError::InvalidMerchant);
    }
    let mut total = 0_i64;
    let mut item_ids = BTreeSet::new();
    let mut items = Vec::with_capacity(command.items.len());
    for item in command.items {
        if item.item_id.is_empty() || !item_ids.insert(item.item_id.clone()) {
            return Err(CreatePurchaseError::InvalidItemId);
        }
        let name = item.name.trim();
        if name.is_empty() || name.chars().count() > 240 {
            return Err(CreatePurchaseError::InvalidItemName);
        }
        if !(0..=1_000_000_000_000).contains(&item.amount_yen) {
            return Err(CreatePurchaseError::InvalidAmount);
        }
        total = total
            .checked_add(item.amount_yen)
            .ok_or(CreatePurchaseError::AmountOverflow)?;
        if let Some(price) = &item.printed_unit_price {
            if !(0..=1_000_000_000_000).contains(&price.amount_yen) {
                return Err(CreatePurchaseError::InvalidAmount);
            }
        }
        let mut seen_pools = BTreeSet::new();
        let mut weights = Vec::with_capacity(item.allocations.len());
        for allocation in &item.allocations {
            if allocation.pool_id.is_empty()
                || allocation.weight <= 0
                || allocation.rule_version == 0
            {
                return Err(CreatePurchaseError::InvalidAllocation);
            }
            if !seen_pools.insert(allocation.pool_id.clone()) {
                return Err(CreatePurchaseError::DuplicateAllocationPool);
            }
            weights.push(
                AllocationWeight::new(
                    PoolId::new(allocation.pool_id.clone())
                        .map_err(|_| CreatePurchaseError::InvalidAllocation)?,
                    allocation.weight as u64,
                )
                .map_err(|_| CreatePurchaseError::InvalidAllocation)?,
            );
        }
        let shares =
            AllocationShares::new(weights).map_err(|_| CreatePurchaseError::InvalidAllocation)?;
        let allocations = allocate(
            Yen::new(item.amount_yen).map_err(|_| CreatePurchaseError::InvalidAmount)?,
            &shares,
        )
        .into_iter()
        .map(|allocation| {
            let source = item
                .allocations
                .iter()
                .find(|candidate| candidate.pool_id == allocation.pool_id().as_str())
                .expect("allocation preserves its pool id");
            DecidedPurchaseAllocation {
                pool_id: allocation.pool_id().as_str().to_owned(),
                amount_yen: allocation.amount().value(),
                weight: source.weight,
                rule_version: source.rule_version,
                rule_id: source.rule_id.clone(),
            }
        })
        .collect();
        items.push(DecidedPurchaseItem {
            item_id: item.item_id,
            name: name.to_owned(),
            amount_yen: item.amount_yen,
            category_id: item.category_id,
            product_variant_id: item.product_variant_id,
            quantity: item.quantity,
            printed_unit_price: item.printed_unit_price,
            allocation_override: item.allocation_override,
            allocations,
        });
    }

    let mut payment_accounts = BTreeSet::new();
    for payment in &command.payments {
        if payment.account_id.is_empty() || !(1..=1_000_000_000_000).contains(&payment.amount_yen) {
            return Err(CreatePurchaseError::InvalidPayment);
        }
        if !payment_accounts.insert(payment.account_id.clone()) {
            return Err(CreatePurchaseError::DuplicatePaymentAccount);
        }
    }

    Ok(CreatePurchaseDecision {
        purchase_id: command.purchase_id,
        occurred_at: command.occurred_at,
        date_precision: command.date_precision,
        merchant: command.merchant.trim().to_owned(),
        origin: command.origin,
        allocation_rule_version: command.allocation_rule_version,
        source_document_id: command.source_document_id,
        source_reference: command.source_reference,
        receipt_candidate_id: command.receipt_candidate_id,
        manual_amount_override_reason: command
            .manual_amount_override_reason
            .map(|reason| reason.trim().to_owned()),
        items,
        payments: command.payments,
    })
}

fn gcd(mut left: i64, mut right: i64) -> i64 {
    while right != 0 {
        (left, right) = (right, left % right);
    }
    left
}

#[cfg(test)]
mod tests {
    use super::*;

    fn command() -> CreatePurchaseCommand {
        CreatePurchaseCommand {
            purchase_id: "purchase-1".to_owned(),
            occurred_at: PurchaseOccurredAt::new("2026-10-15T12:00:00Z".parse().unwrap()),
            date_precision: DatePrecision::DateTime,
            merchant: "  Market  ".to_owned(),
            origin: PurchaseOrigin::Manual,
            allocation_rule_version: AllocationRuleVersion::new(1).unwrap(),
            source_document_id: None,
            source_reference: None,
            receipt_candidate_id: None,
            manual_amount_override_reason: None,
            items: vec![CreatePurchaseItem {
                item_id: "item-1".to_owned(),
                name: "  milk  ".to_owned(),
                amount_yen: 101,
                category_id: None,
                product_variant_id: None,
                quantity: None,
                printed_unit_price: None,
                allocation_override: false,
                allocations: vec![CreatePurchaseAllocation {
                    pool_id: "pool-1".to_owned(),
                    weight: 1,
                    rule_version: 1,
                    rule_id: None,
                }],
            }],
            payments: vec![],
        }
    }

    #[test]
    fn create_normalizes_text_and_allocates_item_amount() {
        let decision = decide_create_purchase(command()).unwrap();
        assert_eq!(decision.merchant, "Market");
        assert_eq!(decision.date_precision, DatePrecision::DateTime);
        assert_eq!(decision.origin, PurchaseOrigin::Manual);
        assert_eq!(decision.items[0].name, "milk");
        assert_eq!(decision.items[0].allocations[0].amount_yen, 101);
    }

    #[test]
    fn create_rejects_duplicate_payment_accounts() {
        let mut input = command();
        input.payments.push(CreatePurchasePayment {
            account_id: "account-1".to_owned(),
            amount_yen: 50,
        });
        input.payments.push(CreatePurchasePayment {
            account_id: "account-1".to_owned(),
            amount_yen: 50,
        });
        assert_eq!(
            decide_create_purchase(input),
            Err(CreatePurchaseError::DuplicatePaymentAccount)
        );
    }
}
