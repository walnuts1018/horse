use std::collections::BTreeSet;
use std::num::NonZeroU64;

use crate::{
    AccountId, AllocationShares, AllocationWeight as ShareWeight, ItemId, PoolId, PositiveYen,
    PurchaseId, Yen, allocate,
};
use chrono::{DateTime, Utc};
use uuid::Uuid;

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

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PurchaseOrigin {
    AiAssisted,
    Manual,
    Unknown,
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

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ReceiptCandidateId(String);

impl ReceiptCandidateId {
    pub fn new(value: impl Into<String>) -> Option<Self> {
        let value = value.into();
        (!value.is_empty()).then_some(Self(value))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct PurchaseAllocationWeight(NonZeroU64);

impl PurchaseAllocationWeight {
    pub fn new(value: i64) -> Option<Self> {
        u64::try_from(value)
            .ok()
            .and_then(NonZeroU64::new)
            .map(Self)
    }

    pub const fn value(self) -> i64 {
        self.0.get() as i64
    }

    const fn as_u64(self) -> u64 {
        self.0.get()
    }
}

macro_rules! uuid_identifier {
    ($name:ident) => {
        #[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
        pub struct $name(Uuid);

        impl $name {
            pub const fn new(value: Uuid) -> Self {
                Self(value)
            }

            pub const fn value(self) -> Uuid {
                self.0
            }
        }
    };
}

uuid_identifier!(CategoryId);
uuid_identifier!(ProductVariantId);
uuid_identifier!(AllocationRuleId);
uuid_identifier!(SourceDocumentId);

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
    pub purchase_id: PurchaseId,
    pub occurred_at: PurchaseOccurredAt,
    pub date_precision: DatePrecision,
    pub merchant: String,
    pub origin: PurchaseOrigin,
    pub source_document_id: Option<SourceDocumentId>,
    pub source_reference: Option<String>,
    pub receipt_candidate_id: Option<ReceiptCandidateId>,
    pub manual_amount_override_reason: Option<String>,
    pub items: Vec<CreatePurchaseItem>,
    pub payments: Vec<CreatePurchasePayment>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreatePurchaseItem {
    pub item_id: ItemId,
    pub name: String,
    pub amount_yen: Yen,
    pub category_id: Option<CategoryId>,
    pub product_variant_id: Option<ProductVariantId>,
    pub quantity: Option<Measure>,
    pub printed_unit_price: Option<PrintedUnitPrice>,
    pub requested_allocations: Vec<RequestedPurchaseAllocation>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RequestedPurchaseAllocation {
    pub pool_id: PoolId,
    pub weight: PurchaseAllocationWeight,
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
    pub amount_yen: Yen,
    pub basis: Measure,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreatePurchaseAllocation {
    pub pool_id: PoolId,
    pub weight: PurchaseAllocationWeight,
    pub rule_version: AllocationRuleVersion,
    pub rule_id: Option<AllocationRuleId>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AllocatedPurchaseItem {
    pub category_id: Option<CategoryId>,
    pub allocations: Vec<CreatePurchaseAllocation>,
    pub allocation_override: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AllocatedPurchase {
    command: CreatePurchaseCommand,
    allocation_rule_version: AllocationRuleVersion,
    items: Vec<AllocatedPurchaseItem>,
}

impl AllocatedPurchase {
    pub fn resolve(
        command: CreatePurchaseCommand,
        allocation_rule_version: AllocationRuleVersion,
        items: Vec<AllocatedPurchaseItem>,
    ) -> Result<Self, CreatePurchaseError> {
        if command.items.len() != items.len() {
            return Err(CreatePurchaseError::InvalidAllocation);
        }
        Ok(Self {
            command,
            allocation_rule_version,
            items,
        })
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreatePurchasePayment {
    pub account_id: AccountId,
    pub amount_yen: PositiveYen,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreatePurchaseDecision {
    pub purchase_id: PurchaseId,
    pub occurred_at: PurchaseOccurredAt,
    pub date_precision: DatePrecision,
    pub merchant: String,
    pub origin: PurchaseOrigin,
    pub allocation_rule_version: AllocationRuleVersion,
    pub source_document_id: Option<SourceDocumentId>,
    pub source_reference: Option<String>,
    pub receipt_candidate_id: Option<ReceiptCandidateId>,
    pub manual_amount_override_reason: Option<String>,
    pub items: Vec<DecidedPurchaseItem>,
    pub payments: Vec<CreatePurchasePayment>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DecidedPurchaseItem {
    pub item_id: ItemId,
    pub name: String,
    pub amount_yen: Yen,
    pub category_id: Option<CategoryId>,
    pub product_variant_id: Option<ProductVariantId>,
    pub quantity: Option<Measure>,
    pub printed_unit_price: Option<PrintedUnitPrice>,
    pub allocation_override: bool,
    pub allocations: Vec<DecidedPurchaseAllocation>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DecidedPurchaseAllocation {
    pub pool_id: PoolId,
    pub amount_yen: Yen,
    pub weight: PurchaseAllocationWeight,
    pub rule_version: AllocationRuleVersion,
    pub rule_id: Option<AllocationRuleId>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AllocationRuleCandidate {
    pub rule_id: String,
    pub revision: u64,
    pub product_variant_id: Option<String>,
    pub product_concept_id: Option<String>,
    pub category_id: Option<String>,
    pub merchant_key: Option<String>,
}

pub fn select_allocation_rule<'a>(
    candidates: &'a [AllocationRuleCandidate],
    product_variant_id: Option<&str>,
    product_concept_id: Option<&str>,
    category_id: Option<&str>,
    merchant_key: &str,
) -> Option<&'a AllocationRuleCandidate> {
    product_variant_id
        .and_then(|id| {
            candidates
                .iter()
                .find(|candidate| candidate.product_variant_id.as_deref() == Some(id))
        })
        .or_else(|| {
            product_concept_id.and_then(|id| {
                candidates
                    .iter()
                    .find(|candidate| candidate.product_concept_id.as_deref() == Some(id))
            })
        })
        .or_else(|| {
            category_id.and_then(|id| {
                candidates
                    .iter()
                    .find(|candidate| candidate.category_id.as_deref() == Some(id))
            })
        })
        .or_else(|| {
            if merchant_key.is_empty() {
                None
            } else {
                candidates
                    .iter()
                    .find(|candidate| candidate.merchant_key.as_deref() == Some(merchant_key))
            }
        })
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CreatePurchaseError {
    InvalidSourceReference,
    InvalidReceiptOverrideReason,
    InvalidMerchant,
    InvalidItemCount,
    InvalidItemId,
    InvalidItemName,
    InvalidAmount,
    InvalidAllocation,
    DuplicateAllocationPool,
    DuplicatePaymentAccount,
    AmountOverflow,
}

pub fn decide_create_purchase(
    purchase: AllocatedPurchase,
) -> Result<CreatePurchaseDecision, CreatePurchaseError> {
    let AllocatedPurchase {
        command,
        allocation_rule_version,
        items: allocated_items,
    } = purchase;
    if let Some(reference) = &command.source_reference
        && (command.source_document_id.is_none()
            || reference.len() > 256
            || reference.contains(['\r', '\n']))
    {
        return Err(CreatePurchaseError::InvalidSourceReference);
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
    for (item, allocated_item) in command.items.into_iter().zip(allocated_items) {
        if !item_ids.insert(item.item_id.as_str().to_owned()) {
            return Err(CreatePurchaseError::InvalidItemId);
        }
        let name = item.name.trim();
        if name.is_empty() || name.chars().count() > 240 {
            return Err(CreatePurchaseError::InvalidItemName);
        }
        if !(0..=crate::MAX_YEN).contains(&item.amount_yen.value()) {
            return Err(CreatePurchaseError::InvalidAmount);
        }
        total = total
            .checked_add(item.amount_yen.value())
            .ok_or(CreatePurchaseError::AmountOverflow)?;
        if item
            .printed_unit_price
            .as_ref()
            .is_some_and(|price| !(0..=crate::MAX_YEN).contains(&price.amount_yen.value()))
        {
            return Err(CreatePurchaseError::InvalidAmount);
        }
        let mut seen_pools = BTreeSet::new();
        let mut weights = Vec::with_capacity(allocated_item.allocations.len());
        for allocation in &allocated_item.allocations {
            if !seen_pools.insert(allocation.pool_id.as_str().to_owned()) {
                return Err(CreatePurchaseError::DuplicateAllocationPool);
            }
            weights.push(
                ShareWeight::new(allocation.pool_id.clone(), allocation.weight.as_u64())
                    .map_err(|_| CreatePurchaseError::InvalidAllocation)?,
            );
        }
        let shares =
            AllocationShares::new(weights).map_err(|_| CreatePurchaseError::InvalidAllocation)?;
        let allocations = allocate(item.amount_yen, &shares)
            .into_iter()
            .map(|allocation| {
                let source = allocated_item
                    .allocations
                    .iter()
                    .find(|candidate| candidate.pool_id.as_str() == allocation.pool_id().as_str())
                    .expect("allocation preserves its pool id");
                DecidedPurchaseAllocation {
                    pool_id: allocation.pool_id().clone(),
                    amount_yen: allocation.amount(),
                    weight: source.weight,
                    rule_version: source.rule_version,
                    rule_id: source.rule_id,
                }
            })
            .collect();
        items.push(DecidedPurchaseItem {
            item_id: item.item_id,
            name: name.to_owned(),
            amount_yen: item.amount_yen,
            category_id: allocated_item.category_id,
            product_variant_id: item.product_variant_id,
            quantity: item.quantity,
            printed_unit_price: item.printed_unit_price,
            allocation_override: allocated_item.allocation_override,
            allocations,
        });
    }

    let mut payment_accounts = BTreeSet::new();
    for payment in &command.payments {
        if !payment_accounts.insert(payment.account_id.as_str().to_owned()) {
            return Err(CreatePurchaseError::DuplicatePaymentAccount);
        }
    }

    Ok(CreatePurchaseDecision {
        purchase_id: command.purchase_id,
        occurred_at: command.occurred_at,
        date_precision: command.date_precision,
        merchant: command.merchant.trim().to_owned(),
        origin: command.origin,
        allocation_rule_version,
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
            purchase_id: PurchaseId::new("purchase-1").unwrap(),
            occurred_at: PurchaseOccurredAt::new("2026-10-15T12:00:00Z".parse().unwrap()),
            date_precision: DatePrecision::DateTime,
            merchant: "  Market  ".to_owned(),
            origin: PurchaseOrigin::Manual,
            source_document_id: None,
            source_reference: None,
            receipt_candidate_id: None,
            manual_amount_override_reason: None,
            items: vec![CreatePurchaseItem {
                item_id: ItemId::new("item-1").unwrap(),
                name: "  milk  ".to_owned(),
                amount_yen: Yen::new(101).unwrap(),
                category_id: None,
                product_variant_id: None,
                quantity: None,
                printed_unit_price: None,
                requested_allocations: vec![],
            }],
            payments: vec![],
        }
    }

    fn allocated(command: CreatePurchaseCommand) -> AllocatedPurchase {
        AllocatedPurchase::resolve(
            command,
            AllocationRuleVersion::new(1).unwrap(),
            vec![AllocatedPurchaseItem {
                category_id: None,
                allocation_override: false,
                allocations: vec![CreatePurchaseAllocation {
                    pool_id: PoolId::new("pool-1").unwrap(),
                    weight: PurchaseAllocationWeight::new(1).unwrap(),
                    rule_version: AllocationRuleVersion::new(1).unwrap(),
                    rule_id: None,
                }],
            }],
        )
        .unwrap()
    }

    #[test]
    fn create_normalizes_text_and_allocates_item_amount() {
        let decision = decide_create_purchase(allocated(command())).unwrap();
        assert_eq!(decision.merchant, "Market");
        assert_eq!(decision.date_precision, DatePrecision::DateTime);
        assert_eq!(decision.origin, PurchaseOrigin::Manual);
        assert_eq!(decision.items[0].name, "milk");
        assert_eq!(decision.items[0].allocations[0].amount_yen.value(), 101);
    }

    #[test]
    fn create_rejects_duplicate_payment_accounts() {
        let mut input = command();
        input.payments.push(CreatePurchasePayment {
            account_id: AccountId::new("account-1").unwrap(),
            amount_yen: PositiveYen::new(50).unwrap(),
        });
        input.payments.push(CreatePurchasePayment {
            account_id: AccountId::new("account-1").unwrap(),
            amount_yen: PositiveYen::new(50).unwrap(),
        });
        assert_eq!(
            decide_create_purchase(allocated(input)),
            Err(CreatePurchaseError::DuplicatePaymentAccount)
        );
    }

    #[test]
    fn allocation_weight_rejects_nonpositive_values() {
        assert_eq!(PurchaseAllocationWeight::new(0), None);
        assert_eq!(PurchaseAllocationWeight::new(-1), None);
    }

    #[test]
    fn measure_constructor_rejects_values_outside_the_validated_range() {
        assert_eq!(
            Measure::new(0, 1, QuantityUnit::Piece),
            Err(MeasureError::InvalidMeasure)
        );
        assert_eq!(
            Measure::new(1, 0, QuantityUnit::Piece),
            Err(MeasureError::InvalidMeasure)
        );
        assert_eq!(
            Measure::new(2, 4, QuantityUnit::Piece),
            Err(MeasureError::InvalidMeasure)
        );
        assert_eq!(
            Measure::new(1_000_000_000_001, 1, QuantityUnit::Piece),
            Err(MeasureError::InvalidMeasure)
        );
        assert_eq!(
            Measure::new(1, 1_000_001, QuantityUnit::Piece),
            Err(MeasureError::InvalidMeasure)
        );
    }

    #[test]
    fn allocation_rule_selection_uses_scope_precedence() {
        let candidates = vec![
            AllocationRuleCandidate {
                rule_id: "merchant".to_owned(),
                revision: 1,
                product_variant_id: None,
                product_concept_id: None,
                category_id: None,
                merchant_key: Some("market".to_owned()),
            },
            AllocationRuleCandidate {
                rule_id: "category".to_owned(),
                revision: 1,
                product_variant_id: None,
                product_concept_id: None,
                category_id: Some("category-1".to_owned()),
                merchant_key: None,
            },
            AllocationRuleCandidate {
                rule_id: "concept".to_owned(),
                revision: 1,
                product_variant_id: None,
                product_concept_id: Some("concept-1".to_owned()),
                category_id: None,
                merchant_key: None,
            },
            AllocationRuleCandidate {
                rule_id: "variant".to_owned(),
                revision: 1,
                product_variant_id: Some("variant-1".to_owned()),
                product_concept_id: None,
                category_id: None,
                merchant_key: None,
            },
        ];

        assert_eq!(
            select_allocation_rule(
                &candidates,
                Some("variant-1"),
                Some("concept-1"),
                Some("category-1"),
                "market",
            )
            .map(|candidate| candidate.rule_id.as_str()),
            Some("variant")
        );
    }
}
