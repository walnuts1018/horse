#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct YearMonth {
    year: u16,
    month: u8,
}

impl YearMonth {
    pub fn new(year: u16, month: u8) -> Result<Self, AccountingPeriodError> {
        if year == 0 || month == 0 || month > 12 {
            return Err(AccountingPeriodError::InvalidMonth);
        }
        Ok(Self { year, month })
    }

    pub fn parse(value: &str) -> Result<Self, AccountingPeriodError> {
        if value.len() != 7
            || value.as_bytes()[4] != b'-'
            || !value.as_bytes()[..4].iter().all(u8::is_ascii_digit)
            || !value.as_bytes()[5..].iter().all(u8::is_ascii_digit)
        {
            return Err(AccountingPeriodError::InvalidMonth);
        }
        let year = &value[..4];
        let month = &value[5..];
        Self::new(
            year.parse()
                .map_err(|_| AccountingPeriodError::InvalidMonth)?,
            month
                .parse()
                .map_err(|_| AccountingPeriodError::InvalidMonth)?,
        )
    }

    pub const fn year(self) -> u16 {
        self.year
    }

    pub const fn month(self) -> u8 {
        self.month
    }

    pub fn as_string(self) -> String {
        format!("{:04}-{:02}", self.year, self.month)
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AccountingPeriodStatus {
    Open,
    Closed,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum AccountingPeriod {
    Open(OpenAccountingPeriod),
    Closed(ClosedAccountingPeriod),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct OpenAccountingPeriod {
    year_month: YearMonth,
    revision: u64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ClosedAccountingPeriod {
    year_month: YearMonth,
    revision: u64,
}

impl AccountingPeriod {
    pub fn open(year_month: YearMonth, revision: u64) -> Result<Self, AccountingPeriodError> {
        OpenAccountingPeriod::new(year_month, revision).map(Self::from)
    }

    pub fn closed(year_month: YearMonth, revision: u64) -> Result<Self, AccountingPeriodError> {
        ClosedAccountingPeriod::new(year_month, revision).map(Self::from)
    }

    pub fn into_open(self) -> Result<OpenAccountingPeriod, Self> {
        match self {
            Self::Open(period) => Ok(period),
            closed => Err(closed),
        }
    }

    pub fn into_closed(self) -> Result<ClosedAccountingPeriod, Self> {
        match self {
            Self::Closed(period) => Ok(period),
            open => Err(open),
        }
    }

    pub fn open_period(&self) -> Option<&OpenAccountingPeriod> {
        match self {
            Self::Open(period) => Some(period),
            Self::Closed(_) => None,
        }
    }

    pub fn closed_period(&self) -> Option<&ClosedAccountingPeriod> {
        match self {
            Self::Closed(period) => Some(period),
            Self::Open(_) => None,
        }
    }

    pub const fn year_month(&self) -> YearMonth {
        match self {
            Self::Open(period) => period.year_month,
            Self::Closed(period) => period.year_month,
        }
    }

    pub const fn revision(&self) -> u64 {
        match self {
            Self::Open(period) => period.revision,
            Self::Closed(period) => period.revision,
        }
    }

    pub const fn status(&self) -> AccountingPeriodStatus {
        match self {
            Self::Open(_) => AccountingPeriodStatus::Open,
            Self::Closed(_) => AccountingPeriodStatus::Closed,
        }
    }

    pub fn as_string(&self) -> String {
        self.year_month().as_string()
    }
}

impl OpenAccountingPeriod {
    pub fn new(year_month: YearMonth, revision: u64) -> Result<Self, AccountingPeriodError> {
        if revision == 0 {
            return Err(AccountingPeriodError::InvalidRevision);
        }
        Ok(Self {
            year_month,
            revision,
        })
    }

    pub fn close(
        self,
        expected_revision: u64,
    ) -> Result<ClosedAccountingPeriod, AccountingPeriodTransitionError> {
        if self.revision != expected_revision {
            return Err(AccountingPeriodTransitionError::RevisionMismatch);
        }
        let revision = self
            .revision
            .checked_add(1)
            .ok_or(AccountingPeriodTransitionError::RevisionOverflow)?;
        Ok(ClosedAccountingPeriod {
            year_month: self.year_month,
            revision,
        })
    }

    pub const fn year_month(&self) -> YearMonth {
        self.year_month
    }

    pub const fn revision(&self) -> u64 {
        self.revision
    }

    pub const fn status(&self) -> AccountingPeriodStatus {
        AccountingPeriodStatus::Open
    }

    pub fn as_string(&self) -> String {
        self.year_month.as_string()
    }
}

impl ClosedAccountingPeriod {
    pub fn new(year_month: YearMonth, revision: u64) -> Result<Self, AccountingPeriodError> {
        if revision == 0 {
            return Err(AccountingPeriodError::InvalidRevision);
        }
        Ok(Self {
            year_month,
            revision,
        })
    }

    pub const fn year_month(&self) -> YearMonth {
        self.year_month
    }

    pub const fn revision(&self) -> u64 {
        self.revision
    }

    pub const fn status(&self) -> AccountingPeriodStatus {
        AccountingPeriodStatus::Closed
    }

    pub fn as_string(&self) -> String {
        self.year_month.as_string()
    }
}

impl From<OpenAccountingPeriod> for AccountingPeriod {
    fn from(period: OpenAccountingPeriod) -> Self {
        Self::Open(period)
    }
}

impl From<ClosedAccountingPeriod> for AccountingPeriod {
    fn from(period: ClosedAccountingPeriod) -> Self {
        Self::Closed(period)
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AccountingPeriodTransitionError {
    RevisionMismatch,
    RevisionOverflow,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AccountingPeriodError {
    InvalidMonth,
    InvalidRevision,
}

#[cfg(test)]
mod tests {
    use super::{AccountingPeriodError, YearMonth};

    #[test]
    fn year_month_accepts_only_ascii_yyyy_mm() {
        assert_eq!(YearMonth::parse("2026-10").unwrap().as_string(), "2026-10");
        for invalid in ["+026-10", "2026-+1", "2026-1", "2026/10", "２０２６-10"] {
            assert_eq!(
                YearMonth::parse(invalid),
                Err(AccountingPeriodError::InvalidMonth)
            );
        }
    }
}
