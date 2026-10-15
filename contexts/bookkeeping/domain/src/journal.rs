use std::collections::BTreeSet;

use crate::{JournalLine, Side};

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum JournalError {
    Empty,
    InvalidLineNumber,
    Unbalanced,
    AmountOverflow,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BalancedJournal {
    entries: Vec<JournalLine>,
    debit_total: i64,
}

impl BalancedJournal {
    pub fn new(entries: Vec<JournalLine>) -> Result<Self, JournalError> {
        if entries.is_empty() {
            return Err(JournalError::Empty);
        }

        let mut line_numbers = BTreeSet::new();
        let mut debit_total = 0_i64;
        let mut credit_total = 0_i64;
        for entry in &entries {
            if entry.line_no() == 0 || !line_numbers.insert(entry.line_no()) {
                return Err(JournalError::InvalidLineNumber);
            }
            let total = match entry.side() {
                Side::Debit => &mut debit_total,
                Side::Credit => &mut credit_total,
            };
            *total = total
                .checked_add(entry.amount().value())
                .ok_or(JournalError::AmountOverflow)?;
        }
        if debit_total == 0 || debit_total != credit_total {
            return Err(JournalError::Unbalanced);
        }

        Ok(Self {
            entries,
            debit_total,
        })
    }

    pub fn entries(&self) -> &[JournalLine] {
        &self.entries
    }

    pub const fn debit_total(&self) -> i64 {
        self.debit_total
    }

    pub const fn credit_total(&self) -> i64 {
        self.debit_total
    }
}
