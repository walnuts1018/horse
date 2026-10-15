use crate::PositiveYen;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LedgerAccountId(String);

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct LedgerAccountIdError;

impl LedgerAccountId {
    pub fn new(value: impl Into<String>) -> Result<Self, LedgerAccountIdError> {
        let value = value.into();
        if value.is_empty() {
            return Err(LedgerAccountIdError);
        }
        Ok(Self(value))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum JournalSide {
    Debit,
    Credit,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct JournalEntry {
    account_id: LedgerAccountId,
    side: JournalSide,
    amount: PositiveYen,
}

impl JournalEntry {
    pub fn new(account_id: LedgerAccountId, side: JournalSide, amount: PositiveYen) -> Self {
        Self {
            account_id,
            side,
            amount,
        }
    }

    pub fn account_id(&self) -> &LedgerAccountId {
        &self.account_id
    }

    pub const fn side(&self) -> JournalSide {
        self.side
    }

    pub const fn amount(&self) -> PositiveYen {
        self.amount
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum JournalError {
    Empty,
    Unbalanced,
}

/// 空でなく、借方と貸方の合計が一致する仕訳。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BalancedJournal {
    entries: Vec<JournalEntry>,
}

impl BalancedJournal {
    pub fn new(entries: Vec<JournalEntry>) -> Result<Self, JournalError> {
        if entries.is_empty() {
            return Err(JournalError::Empty);
        }

        let (debits, credits) = entries.iter().fold((0_i128, 0_i128), |totals, entry| {
            let amount = i128::from(entry.amount.value());
            match entry.side {
                JournalSide::Debit => (totals.0 + amount, totals.1),
                JournalSide::Credit => (totals.0, totals.1 + amount),
            }
        });
        if debits != credits {
            return Err(JournalError::Unbalanced);
        }

        Ok(Self { entries })
    }

    pub fn entries(&self) -> &[JournalEntry] {
        &self.entries
    }

    pub fn total_debits(&self) -> i128 {
        self.entries
            .iter()
            .filter(|entry| entry.side == JournalSide::Debit)
            .map(|entry| i128::from(entry.amount.value()))
            .sum()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn entry(account: &str, side: JournalSide, amount: i64) -> JournalEntry {
        JournalEntry::new(
            LedgerAccountId::new(account).unwrap(),
            side,
            PositiveYen::new(amount).unwrap(),
        )
    }

    #[test]
    fn journal_requires_entries_and_equal_totals() {
        assert_eq!(BalancedJournal::new(vec![]), Err(JournalError::Empty));
        assert_eq!(
            BalancedJournal::new(vec![entry("cash", JournalSide::Credit, 10)]),
            Err(JournalError::Unbalanced)
        );

        let journal = BalancedJournal::new(vec![
            entry("expense", JournalSide::Debit, 10),
            entry("cash", JournalSide::Credit, 10),
        ])
        .unwrap();
        assert_eq!(journal.entries().len(), 2);
    }
}
