use std::collections::HashSet;

use crate::Yen;

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum AllocationError {
    EmptyShares,
    DuplicatePoolId,
    NonPositiveWeight,
    WeightTotalOverflow,
}

#[derive(Debug, Clone, PartialEq, Eq, Hash, PartialOrd, Ord)]
pub struct PoolId(String);

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct PoolIdError;

impl PoolId {
    pub fn new(value: impl Into<String>) -> Result<Self, PoolIdError> {
        let value = value.into();
        if value.is_empty() {
            return Err(PoolIdError);
        }

        Ok(Self(value))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AllocationWeight {
    pool_id: PoolId,
    value: u64,
}

impl AllocationWeight {
    pub fn new(pool_id: PoolId, value: u64) -> Result<Self, AllocationError> {
        if value == 0 {
            return Err(AllocationError::NonPositiveWeight);
        }

        Ok(Self { pool_id, value })
    }

    pub fn pool_id(&self) -> &PoolId {
        &self.pool_id
    }

    pub const fn value(&self) -> u64 {
        self.value
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AllocationShares(Vec<AllocationWeight>);

impl AllocationShares {
    pub fn new(weights: Vec<AllocationWeight>) -> Result<Self, AllocationError> {
        if weights.is_empty() {
            return Err(AllocationError::EmptyShares);
        }

        let mut pool_ids = HashSet::with_capacity(weights.len());
        let mut total_weight = 0_i128;
        for weight in &weights {
            if !pool_ids.insert(&weight.pool_id) {
                return Err(AllocationError::DuplicatePoolId);
            }
            total_weight = total_weight
                .checked_add(i128::from(weight.value))
                .ok_or(AllocationError::WeightTotalOverflow)?;
        }

        Ok(Self(weights))
    }

    pub fn weights(&self) -> &[AllocationWeight] {
        &self.0
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Allocation {
    pool_id: PoolId,
    weight: u64,
    amount: Yen,
}

impl Allocation {
    pub fn pool_id(&self) -> &PoolId {
        &self.pool_id
    }

    pub const fn weight(&self) -> u64 {
        self.weight
    }

    pub const fn amount(&self) -> Yen {
        self.amount
    }
}

struct Remainder {
    index: usize,
    value: i128,
}

/// 最大剰余法で整数円を配分し、余りはpool ID順で決定的に配る。
pub fn allocate(amount: Yen, shares: &AllocationShares) -> Vec<Allocation> {
    let total_weight: i128 = shares.0.iter().map(|weight| i128::from(weight.value)).sum();
    let mut allocated_total = 0_i64;
    let mut result = Vec::with_capacity(shares.0.len());
    let mut remainders = Vec::with_capacity(shares.0.len());

    for (index, weight) in shares.0.iter().enumerate() {
        let numerator = i128::from(amount.value()) * i128::from(weight.value);
        let part = (numerator / total_weight) as i64;
        let remainder = numerator % total_weight;
        allocated_total += part;
        result.push(Allocation {
            pool_id: weight.pool_id.clone(),
            weight: weight.value,
            amount: Yen::new(part).expect("配分額は元の円金額を超えない"),
        });
        remainders.push(Remainder {
            index,
            value: remainder,
        });
    }

    remainders.sort_by(|left, right| {
        right
            .value
            .cmp(&left.value)
            .then_with(|| result[left.index].pool_id.cmp(&result[right.index].pool_id))
    });

    let remaining = (amount.value() - allocated_total) as usize;
    for remainder in remainders.into_iter().take(remaining) {
        let current = result[remainder.index].amount.value();
        result[remainder.index].amount =
            Yen::new(current + 1).expect("配分額は元の円金額を超えない");
    }

    result.sort_by(|left, right| left.pool_id.cmp(&right.pool_id));
    result
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{MAX_YEN, MoneyError, PositiveYen};

    fn pool_id(value: &str) -> PoolId {
        PoolId::new(value).unwrap()
    }

    #[test]
    fn yen_constructors_enforce_amount_ranges() {
        assert_eq!(Yen::new(0).unwrap().value(), 0);
        assert_eq!(Yen::new(MAX_YEN).unwrap().value(), MAX_YEN);
        assert_eq!(Yen::new(-1), Err(MoneyError::OutOfRange));
        assert_eq!(Yen::new(MAX_YEN + 1), Err(MoneyError::OutOfRange));
        assert_eq!(PositiveYen::new(0), Err(MoneyError::MustBePositive));
    }

    #[test]
    fn allocation_conserves_yen_and_breaks_equal_remainders_by_pool_id() {
        let shares = AllocationShares::new(vec![
            AllocationWeight::new(pool_id("b"), 1).unwrap(),
            AllocationWeight::new(pool_id("c"), 1).unwrap(),
            AllocationWeight::new(pool_id("a"), 1).unwrap(),
        ])
        .unwrap();

        let actual = allocate(Yen::new(1).unwrap(), &shares);

        assert_eq!(
            actual
                .iter()
                .map(|item| (item.pool_id().as_str(), item.amount().value()))
                .collect::<Vec<_>>(),
            vec![("a", 1), ("b", 0), ("c", 0)]
        );
        assert_eq!(
            actual.iter().map(|item| item.amount().value()).sum::<i64>(),
            1
        );
    }

    #[test]
    fn allocation_rejects_invalid_share_sets() {
        assert_eq!(
            AllocationShares::new(vec![]),
            Err(AllocationError::EmptyShares)
        );
        assert_eq!(
            AllocationShares::new(vec![
                AllocationWeight::new(pool_id("pool"), 1).unwrap(),
                AllocationWeight::new(pool_id("pool"), 2).unwrap()
            ]),
            Err(AllocationError::DuplicatePoolId)
        );
        assert_eq!(
            AllocationWeight::new(pool_id("pool"), 0),
            Err(AllocationError::NonPositiveWeight)
        );
    }

    #[test]
    fn pool_id_rejects_empty_values() {
        assert_eq!(PoolId::new(""), Err(PoolIdError));
        assert_eq!(PoolId::new("pool-1").unwrap().as_str(), "pool-1");
    }

    #[test]
    fn allocation_is_input_order_independent_and_handles_maximum_yen_with_large_weights() {
        let first = AllocationShares::new(vec![
            AllocationWeight::new(pool_id("b"), u64::MAX).unwrap(),
            AllocationWeight::new(pool_id("a"), u64::MAX).unwrap(),
            AllocationWeight::new(pool_id("c"), 1).unwrap(),
        ])
        .unwrap();
        let reordered = AllocationShares::new(vec![
            AllocationWeight::new(pool_id("c"), 1).unwrap(),
            AllocationWeight::new(pool_id("a"), u64::MAX).unwrap(),
            AllocationWeight::new(pool_id("b"), u64::MAX).unwrap(),
        ])
        .unwrap();

        let amount = Yen::new(MAX_YEN).unwrap();
        let actual = allocate(amount, &first);

        assert_eq!(actual, allocate(amount, &reordered));
        assert_eq!(
            actual.iter().map(|part| part.amount().value()).sum::<i64>(),
            MAX_YEN
        );
        assert_eq!(
            actual
                .iter()
                .map(|part| (part.pool_id().as_str(), part.amount().value()))
                .collect::<Vec<_>>(),
            vec![("a", MAX_YEN / 2), ("b", MAX_YEN / 2), ("c", 0)]
        );
        assert!(actual.iter().all(|part| part.amount().value() >= 0));
        assert_eq!(
            actual
                .iter()
                .map(|part| part.pool_id().as_str())
                .collect::<Vec<_>>(),
            vec!["a", "b", "c"]
        );
    }
}
