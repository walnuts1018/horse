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
        let (year, month) = value
            .split_once('-')
            .ok_or(AccountingPeriodError::InvalidMonth)?;
        if year.len() != 4 || month.len() != 2 {
            return Err(AccountingPeriodError::InvalidMonth);
        }
        Self::new(
            year.parse().map_err(|_| AccountingPeriodError::InvalidMonth)?,
            month.parse().map_err(|_| AccountingPeriodError::InvalidMonth)?,
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
pub struct AccountingPeriod {
    year_month: YearMonth,
    revision: u64,
    status: AccountingPeriodStatus,
}

impl AccountingPeriod {
    pub fn open(year_month: YearMonth, revision: u64) -> Result<Self, AccountingPeriodError> {
        if revision == 0 {
            return Err(AccountingPeriodError::InvalidRevision);
        }
        Ok(Self {
            year_month,
            revision,
            status: AccountingPeriodStatus::Open,
        })
    }

    pub fn closed(year_month: YearMonth, revision: u64) -> Result<Self, AccountingPeriodError> {
        if revision == 0 {
            return Err(AccountingPeriodError::InvalidRevision);
        }
        Ok(Self {
            year_month,
            revision,
            status: AccountingPeriodStatus::Closed,
        })
    }

    pub fn close(
        self,
        expected_revision: u64,
    ) -> Result<Self, AccountingPeriodTransitionError> {
        if self.status == AccountingPeriodStatus::Closed {
            return Err(AccountingPeriodTransitionError::AlreadyClosed);
        }
        if self.revision != expected_revision {
            return Err(AccountingPeriodTransitionError::RevisionMismatch);
        }
        let revision = self
            .revision
            .checked_add(1)
            .ok_or(AccountingPeriodTransitionError::RevisionOverflow)?;
        Ok(Self {
            year_month: self.year_month,
            revision,
            status: AccountingPeriodStatus::Closed,
        })
    }

    pub const fn year_month(&self) -> YearMonth {
        self.year_month
    }

    pub const fn revision(&self) -> u64 {
        self.revision
    }

    pub const fn status(&self) -> AccountingPeriodStatus {
        self.status
    }

    pub fn as_string(&self) -> String {
        self.year_month.as_string()
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AccountingPeriodTransitionError {
    AlreadyClosed,
    RevisionMismatch,
    RevisionOverflow,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AccountingPeriodError {
    InvalidMonth,
    InvalidRevision,
}
