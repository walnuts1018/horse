/// Horseで扱う円金額の上限。
pub const MAX_YEN: i64 = 1_000_000_000_000;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum MoneyError {
    OutOfRange,
    MustBePositive,
    Overflow,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct Yen(i64);

impl Yen {
    pub fn new(value: i64) -> Result<Self, MoneyError> {
        if !(0..=MAX_YEN).contains(&value) {
            return Err(MoneyError::OutOfRange);
        }

        Ok(Self(value))
    }

    pub const fn value(self) -> i64 {
        self.0
    }

    pub fn checked_add(self, other: Self) -> Result<Self, MoneyError> {
        let sum = self.0.checked_add(other.0).ok_or(MoneyError::Overflow)?;
        Self::new(sum)
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct PositiveYen(Yen);

impl PositiveYen {
    pub fn new(value: i64) -> Result<Self, MoneyError> {
        let yen = Yen::new(value)?;
        if yen.value() == 0 {
            return Err(MoneyError::MustBePositive);
        }

        Ok(Self(yen))
    }

    pub const fn yen(self) -> Yen {
        self.0
    }

    pub const fn value(self) -> i64 {
        self.0.value()
    }
}
