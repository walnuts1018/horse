use std::collections::BTreeSet;

use bookkeeping_domain::{AllocationShares, AllocationWeight, PoolId, Yen, allocate};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreatePurchaseCommand {
    pub purchase_id: String,
    pub occurred_at: String,
    pub date_precision: String,
    pub merchant: String,
    pub origin: String,
    pub allocation_rule_version: u64,
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

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Measure {
    pub numerator: i64,
    pub denominator: i64,
    pub unit: String,
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
    pub occurred_at: String,
    pub date_precision: String,
    pub merchant: String,
    pub origin: String,
    pub allocation_rule_version: u64,
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
    InvalidOccurredAt,
    InvalidDatePrecision,
    InvalidOrigin,
    InvalidSourceReference,
    InvalidReceiptOverrideReason,
    InvalidMerchant,
    InvalidAllocationRuleVersion,
    InvalidItemCount,
    InvalidItemId,
    InvalidItemName,
    InvalidAmount,
    InvalidAllocation,
    DuplicateAllocationPool,
    InvalidMeasure,
    InvalidUnit,
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
    if command.occurred_at.is_empty() {
        return Err(CreatePurchaseError::InvalidOccurredAt);
    }
    let date_precision = if command.date_precision.is_empty() {
        "DATE_TIME"
    } else {
        command.date_precision.as_str()
    };
    if !matches!(date_precision, "DATE_TIME" | "DATE_ONLY") {
        return Err(CreatePurchaseError::InvalidDatePrecision);
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
    let origin = if command.origin.is_empty() {
        if command.source_document_id.is_some() {
            "AI"
        } else {
            "MANUAL"
        }
    } else {
        command.origin.as_str()
    };
    if !matches!(origin, "AI" | "MANUAL" | "UNKNOWN") {
        return Err(CreatePurchaseError::InvalidOrigin);
    }
    if command.items.is_empty() || command.items.len() > 200 {
        return Err(CreatePurchaseError::InvalidItemCount);
    }
    if command.merchant.chars().count() > 240 {
        return Err(CreatePurchaseError::InvalidMerchant);
    }
    if command.allocation_rule_version == 0 {
        return Err(CreatePurchaseError::InvalidAllocationRuleVersion);
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
        validate_measure(item.quantity.as_ref())?;
        if let Some(price) = &item.printed_unit_price {
            if !(0..=1_000_000_000_000).contains(&price.amount_yen) {
                return Err(CreatePurchaseError::InvalidMeasure);
            }
            validate_measure(Some(&price.basis))?;
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
        date_precision: date_precision.to_owned(),
        merchant: command.merchant.trim().to_owned(),
        origin: origin.to_owned(),
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

fn validate_measure(measure: Option<&Measure>) -> Result<(), CreatePurchaseError> {
    let Some(measure) = measure else {
        return Ok(());
    };
    if measure.numerator <= 0
        || measure.numerator > 1_000_000_000_000
        || measure.denominator <= 0
        || measure.denominator > 1_000_000
        || gcd(measure.numerator, measure.denominator) != 1
    {
        return Err(CreatePurchaseError::InvalidMeasure);
    }
    if measure.unit.trim().is_empty() || measure.unit.len() > 32 {
        return Err(CreatePurchaseError::InvalidUnit);
    }
    if !matches!(
        measure.unit.as_str(),
        "個" | "本" | "枚" | "袋" | "箱" | "g" | "kg" | "ml" | "L"
    ) {
        return Err(CreatePurchaseError::InvalidUnit);
    }
    Ok(())
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
            occurred_at: "2026-10-15T12:00:00Z".to_owned(),
            date_precision: String::new(),
            merchant: "  Market  ".to_owned(),
            origin: String::new(),
            allocation_rule_version: 1,
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
        assert_eq!(decision.date_precision, "DATE_TIME");
        assert_eq!(decision.origin, "MANUAL");
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
