use chrono::{DateTime, Utc};
use uuid::Uuid;

#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct SourceReference(String);

impl SourceReference {
    pub fn new(value: impl Into<String>) -> Result<Self, InvalidSourceReference> {
        let value = value.into();
        if value.trim().is_empty() || value.len() > 256 || value.trim() != value {
            return Err(InvalidSourceReference);
        }
        Ok(Self(value))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct InvalidSourceReference;

#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct SourceObservationKey {
    pub document_id: Uuid,
    pub source_reference: SourceReference,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SourceObservation {
    pub key: SourceObservationKey,
    pub source_kind: String,
    pub merchant: Option<String>,
    pub occurred_at: Option<DateTime<Utc>>,
    pub amount_yen: Option<i64>,
    pub external_order_id: Option<String>,
    pub external_transaction_id: Option<String>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum MatchSignal {
    ExternalOrderId,
    ExternalTransactionId,
    MerchantAmountAndDate,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Reconciliation {
    SameObservation,
    Candidate(Vec<MatchSignal>),
    NeedsReview,
    Unrelated,
}

pub fn reconcile_observations(
    left: &SourceObservation,
    right: &SourceObservation,
) -> Reconciliation {
    if left.key == right.key {
        return Reconciliation::SameObservation;
    }

    let shared_order = same_non_empty(
        left.external_order_id.as_deref(),
        right.external_order_id.as_deref(),
    );
    let shared_transaction = same_non_empty(
        left.external_transaction_id.as_deref(),
        right.external_transaction_id.as_deref(),
    );
    let same_merchant = normalized_equal(left.merchant.as_deref(), right.merchant.as_deref());
    let same_amount = left.amount_yen.is_some() && left.amount_yen == right.amount_yen;
    let near_date = left
        .occurred_at
        .zip(right.occurred_at)
        .is_some_and(|(left, right)| (left - right).num_seconds().unsigned_abs() <= 36 * 60 * 60);

    if shared_transaction
        && ((left.amount_yen.is_some() && right.amount_yen.is_some() && !same_amount)
            || (left.merchant.is_some() && right.merchant.is_some() && !same_merchant))
    {
        return Reconciliation::NeedsReview;
    }

    let mut signals = Vec::new();
    if shared_order {
        signals.push(MatchSignal::ExternalOrderId);
    }
    if shared_transaction {
        signals.push(MatchSignal::ExternalTransactionId);
    }
    if same_merchant && same_amount && near_date {
        signals.push(MatchSignal::MerchantAmountAndDate);
    }

    if signals.is_empty() {
        Reconciliation::Unrelated
    } else {
        Reconciliation::Candidate(signals)
    }
}

fn same_non_empty(left: Option<&str>, right: Option<&str>) -> bool {
    matches!((left, right), (Some(left), Some(right)) if !left.is_empty() && left == right)
}

fn normalized_equal(left: Option<&str>, right: Option<&str>) -> bool {
    match (left, right) {
        (Some(left), Some(right)) => {
            let left = left.split_whitespace().collect::<Vec<_>>().join(" ");
            let right = right.split_whitespace().collect::<Vec<_>>().join(" ");
            !left.is_empty() && left.eq_ignore_ascii_case(&right)
        }
        _ => false,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::TimeZone;

    fn observation(
        reference: &str,
        order_id: Option<&str>,
        transaction_id: Option<&str>,
        merchant: Option<&str>,
        amount_yen: Option<i64>,
        hour: i64,
    ) -> SourceObservation {
        SourceObservation {
            key: SourceObservationKey {
                document_id: Uuid::from_u128(1),
                source_reference: SourceReference::new(reference).unwrap(),
            },
            source_kind: "receipt".to_owned(),
            merchant: merchant.map(str::to_owned),
            occurred_at: Some(Utc.timestamp_opt(hour * 3600, 0).unwrap()),
            amount_yen,
            external_order_id: order_id.map(str::to_owned),
            external_transaction_id: transaction_id.map(str::to_owned),
        }
    }

    #[test]
    fn source_reference_rejects_values_that_cannot_be_persisted() {
        assert!(SourceReference::new("").is_err());
        assert!(SourceReference::new("  ").is_err());
        assert!(SourceReference::new(" row:1 ").is_err());
        assert!(SourceReference::new("x".repeat(257)).is_err());
        assert!(SourceReference::new("row:1").is_ok());
    }

    #[test]
    fn same_order_is_a_candidate_without_assuming_one_to_one_payment() {
        let order = observation("order", Some("A-1"), None, Some("Amazon"), Some(4980), 0);
        let card = observation(
            "card",
            Some("A-1"),
            Some("T-1"),
            Some("Amazon"),
            Some(3000),
            5,
        );

        assert_eq!(
            reconcile_observations(&order, &card),
            Reconciliation::Candidate(vec![MatchSignal::ExternalOrderId])
        );
    }

    #[test]
    fn shared_transaction_with_conflicting_amount_requires_review() {
        let left = observation("receipt", None, Some("T-1"), Some("Store"), Some(1000), 0);
        let right = observation("card", None, Some("T-1"), Some("Store"), Some(900), 0);

        assert_eq!(
            reconcile_observations(&left, &right),
            Reconciliation::NeedsReview
        );
    }

    #[test]
    fn merchant_amount_and_near_date_create_only_a_candidate() {
        let left = observation(
            "receipt",
            None,
            None,
            Some("  Example   Store "),
            Some(1000),
            0,
        );
        let right = observation("card", None, None, Some("example store"), Some(1000), 24);

        assert_eq!(
            reconcile_observations(&left, &right),
            Reconciliation::Candidate(vec![MatchSignal::MerchantAmountAndDate])
        );
    }
}
