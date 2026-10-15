use crate::{BalancedJournal, PositiveYen};

#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct PurchaseId(String);

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct PurchaseIdError;

impl PurchaseId {
    pub fn new(value: impl Into<String>) -> Result<Self, PurchaseIdError> {
        let value = value.into();
        if value.is_empty() {
            return Err(PurchaseIdError);
        }
        Ok(Self(value))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DraftPurchase {
    id: PurchaseId,
    total: PositiveYen,
}

impl DraftPurchase {
    pub fn new(id: PurchaseId, total: PositiveYen) -> Self {
        Self { id, total }
    }

    pub fn id(&self) -> &PurchaseId {
        &self.id
    }

    pub const fn total(&self) -> PositiveYen {
        self.total
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PostedPurchase {
    id: PurchaseId,
    total: PositiveYen,
    journal: BalancedJournal,
}

impl PostedPurchase {
    pub fn id(&self) -> &PurchaseId {
        &self.id
    }

    pub const fn total(&self) -> PositiveYen {
        self.total
    }

    pub fn journal(&self) -> &BalancedJournal {
        &self.journal
    }
}

/// 永続的な購入状態。会計期間の締め状態はここに含めない。
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Purchase {
    Draft(DraftPurchase),
    Posted(PostedPurchase),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Decision<State, Event> {
    next: State,
    events: Vec<Event>,
}

impl<State, Event> Decision<State, Event> {
    pub fn new(next: State, events: Vec<Event>) -> Self {
        Self { next, events }
    }

    pub fn next(&self) -> &State {
        &self.next
    }

    pub fn into_next(self) -> State {
        self.next
    }

    pub fn events(&self) -> &[Event] {
        &self.events
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PurchaseEvent {
    Posted { purchase_id: PurchaseId, total: PositiveYen },
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PurchaseError {
    JournalTotalMismatch,
}

/// Draftの購入を受け取り、仕訳と合計が一致するときだけPostedへ遷移させる。
pub fn decide_post_purchase(
    draft: DraftPurchase,
    journal: BalancedJournal,
) -> Result<Decision<Purchase, PurchaseEvent>, PurchaseError> {
    if journal.total_debits() != i128::from(draft.total.value()) {
        return Err(PurchaseError::JournalTotalMismatch);
    }

    let posted = PostedPurchase {
        id: draft.id.clone(),
        total: draft.total,
        journal,
    };
    let event = PurchaseEvent::Posted {
        purchase_id: draft.id,
        total: draft.total,
    };
    Ok(Decision::new(Purchase::Posted(posted), vec![event]))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{JournalEntry, JournalSide, LedgerAccountId};

    fn journal(debit: i64, credit: i64) -> BalancedJournal {
        BalancedJournal::new(vec![
            JournalEntry::new(
                LedgerAccountId::new("expense").unwrap(),
                JournalSide::Debit,
                PositiveYen::new(debit).unwrap(),
            ),
            JournalEntry::new(
                LedgerAccountId::new("cash").unwrap(),
                JournalSide::Credit,
                PositiveYen::new(credit).unwrap(),
            ),
        ])
        .unwrap()
    }

    #[test]
    fn posting_returns_new_state_and_event_only_when_journal_matches_purchase() {
        let draft = DraftPurchase::new(
            PurchaseId::new("purchase-1").unwrap(),
            PositiveYen::new(100).unwrap(),
        );
        let decision = decide_post_purchase(draft.clone(), journal(100, 100)).unwrap();
        assert!(matches!(decision.next(), Purchase::Posted(_)));
        assert_eq!(decision.events().len(), 1);
        assert_eq!(
            decide_post_purchase(draft, journal(90, 90)),
            Err(PurchaseError::JournalTotalMismatch)
        );
    }
}
