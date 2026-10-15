use std::collections::{BTreeMap, BTreeSet};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdatePurchaseCommand {
    pub purchase_id: String,
    pub base_revision: u64,
    pub changed_fields: Vec<String>,
    pub occurred_at: Option<String>,
    pub date_precision: Option<String>,
    pub merchant: Option<String>,
    pub payment_allocations: Option<Vec<UpdatePurchasePayment>>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdatePurchasePayment {
    pub account_id: String,
    pub amount_yen: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdatePurchaseDraft {
    pub revision: u64,
    pub status: String,
    pub occurred_at: String,
    pub date_precision: String,
    pub merchant: String,
    pub field_revisions: BTreeMap<String, u64>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdatePurchaseDecision {
    pub revision: u64,
    pub occurred_at: Option<String>,
    pub date_precision: Option<String>,
    pub merchant: Option<String>,
    pub payment_allocations: Option<Vec<UpdatePurchasePayment>>,
    pub field_revisions: BTreeMap<String, u64>,
    pub changed_fields: Vec<String>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum UpdatePurchaseError {
    InvalidPurchaseId,
    InvalidChangedFields,
    DuplicateChangedField,
    MissingChangedValue,
    InvalidDatePrecision,
    InvalidMerchant,
    InvalidPayment,
    DuplicatePaymentAccount,
    InvalidRevision,
    Conflict,
    NotEditable,
}

pub fn decide_update_purchase(
    command: UpdatePurchaseCommand,
    current: UpdatePurchaseDraft,
) -> Result<UpdatePurchaseDecision, UpdatePurchaseError> {
    if command.purchase_id.is_empty() {
        return Err(UpdatePurchaseError::InvalidPurchaseId);
    }
    if !matches!(current.status.as_str(), "DRAFT" | "POSTED") {
        return Err(UpdatePurchaseError::NotEditable);
    }
    if command.base_revision == 0 || command.base_revision > current.revision {
        return Err(UpdatePurchaseError::Conflict);
    }
    if command.changed_fields.is_empty() {
        return Err(UpdatePurchaseError::InvalidChangedFields);
    }

    let allowed = ["occurredAt", "merchant", "paymentAllocations"];
    let mut seen = BTreeSet::new();
    for field in &command.changed_fields {
        if !allowed.contains(&field.as_str()) {
            return Err(UpdatePurchaseError::InvalidChangedFields);
        }
        if !seen.insert(field.as_str()) {
            return Err(UpdatePurchaseError::DuplicateChangedField);
        }
        if current
            .field_revisions
            .get(field)
            .copied()
            .unwrap_or_default()
            > command.base_revision
        {
            return Err(UpdatePurchaseError::Conflict);
        }
    }
    let has = |field: &str| command.changed_fields.iter().any(|value| value == field);
    if has("occurredAt") && command.occurred_at.is_none() {
        return Err(UpdatePurchaseError::MissingChangedValue);
    }
    if command.date_precision.is_some() && (!has("occurredAt") || command.occurred_at.is_none()) {
        return Err(UpdatePurchaseError::InvalidDatePrecision);
    }
    if let Some(precision) = &command.date_precision {
        if !matches!(precision.as_str(), "DATE_TIME" | "DATE_ONLY") {
            return Err(UpdatePurchaseError::InvalidDatePrecision);
        }
    }
    if has("merchant") && command.merchant.is_none() {
        return Err(UpdatePurchaseError::MissingChangedValue);
    }
    let merchant = command.merchant.map(|value| value.trim().to_owned());
    if merchant
        .as_ref()
        .is_some_and(|value| value.chars().count() > 240)
    {
        return Err(UpdatePurchaseError::InvalidMerchant);
    }
    if has("paymentAllocations") && command.payment_allocations.is_none() {
        return Err(UpdatePurchaseError::MissingChangedValue);
    }
    if let Some(payments) = &command.payment_allocations {
        let mut accounts = BTreeSet::new();
        for payment in payments {
            if payment.account_id.is_empty()
                || payment.amount_yen <= 0
                || payment.amount_yen > 1_000_000_000_000
            {
                return Err(UpdatePurchaseError::InvalidPayment);
            }
            if !accounts.insert(payment.account_id.as_str()) {
                return Err(UpdatePurchaseError::DuplicatePaymentAccount);
            }
        }
    }
    let revision = current
        .revision
        .checked_add(1)
        .ok_or(UpdatePurchaseError::InvalidRevision)?;
    let mut field_revisions = current.field_revisions;
    for field in &command.changed_fields {
        field_revisions.insert(field.clone(), revision);
    }
    Ok(UpdatePurchaseDecision {
        revision,
        occurred_at: command.occurred_at,
        date_precision: command.date_precision,
        merchant,
        payment_allocations: command.payment_allocations,
        field_revisions,
        changed_fields: command.changed_fields,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn current() -> UpdatePurchaseDraft {
        UpdatePurchaseDraft {
            revision: 4,
            status: "DRAFT".to_owned(),
            occurred_at: "2026-10-01T00:00:00+09:00".to_owned(),
            date_precision: "DATE_TIME".to_owned(),
            merchant: "店".to_owned(),
            field_revisions: BTreeMap::from([("merchant".to_owned(), 3)]),
        }
    }

    #[test]
    fn permits_disjoint_field_updates_and_advances_only_changed_field_revisions() {
        let decision = decide_update_purchase(
            UpdatePurchaseCommand {
                purchase_id: "purchase".to_owned(),
                base_revision: 2,
                changed_fields: vec!["occurredAt".to_owned()],
                occurred_at: Some("2026-10-02T00:00:00+09:00".to_owned()),
                date_precision: Some("DATE_ONLY".to_owned()),
                merchant: None,
                payment_allocations: None,
            },
            current(),
        )
        .expect("disjoint update");

        assert_eq!(decision.revision, 5);
        assert_eq!(decision.field_revisions.get("occurredAt"), Some(&5));
        assert_eq!(decision.field_revisions.get("merchant"), Some(&3));
    }

    #[test]
    fn rejects_conflicting_fields_and_closed_purchases() {
        let command = UpdatePurchaseCommand {
            purchase_id: "purchase".to_owned(),
            base_revision: 2,
            changed_fields: vec!["merchant".to_owned()],
            occurred_at: None,
            date_precision: None,
            merchant: Some("更新".to_owned()),
            payment_allocations: None,
        };
        assert_eq!(
            decide_update_purchase(command.clone(), current()),
            Err(UpdatePurchaseError::Conflict)
        );

        let mut posted = current();
        posted.status = "POSTED".to_owned();
        assert_eq!(
            decide_update_purchase(command, posted),
            Err(UpdatePurchaseError::NotEditable)
        );
    }
}
