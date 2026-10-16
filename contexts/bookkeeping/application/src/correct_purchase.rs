use std::collections::BTreeMap;

use bookkeeping_domain::{
    AllocationShares, AllocationWeight, CorrectPurchaseDecision, CorrectionFundingBasis,
    CorrectionTarget, PoolId as AllocationPoolId, PositiveYen, Yen, allocate,
};

pub use bookkeeping_domain::SettlementPolicy as CorrectionSettlementPolicy;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CorrectionFundingRule {
    pub pool_id: String,
    pub version: u32,
    pub basis: CorrectionFundingBasis,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CorrectionFundingWeight {
    pub member_id: String,
    pub weight: u64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CorrectionPurchaseBurdenSnapshot {
    pub item_id: String,
    pub pool_id: String,
    pub member_id: String,
    pub funding_rule_version: u32,
    pub funding_weight: u64,
    pub settlement_policy: Option<CorrectionSettlementPolicy>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CorrectionPurchasePayment {
    pub line_no: u32,
    pub account_id: String,
    pub amount: PositiveYen,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CorrectionPaymentFundingSnapshot {
    pub line_no: u32,
    pub account_id: String,
    pub pool_id: String,
    pub member_id: String,
    pub funding_rule_version: u32,
    pub funding_weight: u64,
    pub settlement_policy: Option<CorrectionSettlementPolicy>,
    pub snapshot_source: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CorrectionMemberBurdenSnapshot {
    pub item_id: Option<String>,
    pub pool_id: String,
    pub member_id: String,
    pub amount_delta: i64,
    pub funding_rule_version: u32,
    pub funding_weight: u64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CorrectionSettlementAdjustment {
    pub member_id: String,
    pub amount: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CorrectionFundingEffects {
    pub member_burdens: Vec<CorrectionMemberBurdenSnapshot>,
    pub settlement_adjustments: Vec<CorrectionSettlementAdjustment>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CorrectionFundingError {
    InvalidSnapshot,
    ArithmeticOverflow,
    InvalidAllocation,
}

pub fn calculate_correction_funding_effects(
    decision: &CorrectPurchaseDecision,
    purchase_burdens: &[CorrectionPurchaseBurdenSnapshot],
    purchase_payments: &[CorrectionPurchasePayment],
    payment_snapshots: &[CorrectionPaymentFundingSnapshot],
    historical_rules: &[CorrectionFundingRule],
) -> Result<CorrectionFundingEffects, CorrectionFundingError> {
    let target_item = match &decision.target {
        CorrectionTarget::Purchase => None,
        CorrectionTarget::Item(item_id) => Some(item_id.as_str()),
    };
    let mut burdens = BTreeMap::<String, SavedBurden>::new();
    for snapshot in purchase_burdens {
        if target_item.is_some_and(|item_id| item_id != snapshot.item_id) {
            continue;
        }
        if snapshot.funding_rule_version == 0 || snapshot.funding_weight == 0 {
            return Err(CorrectionFundingError::InvalidSnapshot);
        }
        let burden = burdens
            .entry(snapshot.pool_id.clone())
            .or_insert_with(|| SavedBurden {
                version: snapshot.funding_rule_version,
                policy: snapshot.settlement_policy,
                weights: BTreeMap::new(),
            });
        if burden.version != snapshot.funding_rule_version
            || burden.policy != snapshot.settlement_policy
            || burden
                .weights
                .insert(snapshot.member_id.clone(), snapshot.funding_weight)
                .is_some_and(|weight| weight != snapshot.funding_weight)
        {
            return Err(CorrectionFundingError::InvalidSnapshot);
        }
    }
    if burdens.is_empty() {
        return Err(CorrectionFundingError::InvalidSnapshot);
    }

    let item_id = target_item.map(str::to_owned);
    let mut member_burdens = Vec::new();
    for allocation in &decision.allocations {
        let amount = allocation.amount.value();
        if amount == 0 {
            continue;
        }
        let saved = burdens
            .get(allocation.pool_id.as_str())
            .ok_or(CorrectionFundingError::InvalidSnapshot)?;
        for (member_id, distributed) in distribute(amount, &saved.weights)? {
            if distributed == 0 {
                continue;
            }
            let funding_weight = *saved
                .weights
                .get(&member_id)
                .ok_or(CorrectionFundingError::InvalidSnapshot)?;
            member_burdens.push(CorrectionMemberBurdenSnapshot {
                item_id: item_id.clone(),
                pool_id: allocation.pool_id.as_str().to_owned(),
                member_id,
                amount_delta: if decision.amount_delta.value() < 0 {
                    -distributed
                } else {
                    distributed
                },
                funding_rule_version: saved.version,
                funding_weight,
            });
        }
    }
    member_burdens.sort_by(|left, right| {
        (&left.pool_id, &left.member_id).cmp(&(&right.pool_id, &right.member_id))
    });

    let mut rule_map = BTreeMap::<(String, u32), CorrectionFundingRule>::new();
    for rule in historical_rules {
        if rule.version == 0
            || rule.pool_id.is_empty()
            || rule_map
                .insert((rule.pool_id.clone(), rule.version), rule.clone())
                .is_some()
        {
            return Err(CorrectionFundingError::InvalidSnapshot);
        }
    }
    let mut allocation_rules = BTreeMap::<(String, u32), ResolvedFundingRule>::new();
    for (pool_id, burden) in &burdens {
        let key = (pool_id.clone(), burden.version);
        let rule = rule_map
            .get(&key)
            .ok_or(CorrectionFundingError::InvalidSnapshot)?;
        allocation_rules.insert(
            key,
            make_funding_rule(rule, burden.policy, burden.weights.clone())?,
        );
    }

    let mut payment_groups = BTreeMap::<u32, PaymentSnapshotGroup>::new();
    for snapshot in payment_snapshots {
        if snapshot.line_no == 0
            || snapshot.funding_rule_version == 0
            || snapshot.funding_weight == 0
        {
            return Err(CorrectionFundingError::InvalidSnapshot);
        }
        let group =
            payment_groups
                .entry(snapshot.line_no)
                .or_insert_with(|| PaymentSnapshotGroup {
                    account_id: snapshot.account_id.clone(),
                    pool_id: snapshot.pool_id.clone(),
                    version: snapshot.funding_rule_version,
                    policy: snapshot.settlement_policy,
                    snapshot_source: snapshot.snapshot_source.clone(),
                    weights: BTreeMap::new(),
                });
        if group.account_id != snapshot.account_id
            || group.pool_id != snapshot.pool_id
            || group.version != snapshot.funding_rule_version
            || group.policy != snapshot.settlement_policy
            || group.snapshot_source != snapshot.snapshot_source
            || !matches!(
                snapshot.snapshot_source.as_str(),
                "POSTED" | "LEGACY_CURRENT_RULES"
            )
            || group
                .weights
                .insert(snapshot.member_id.clone(), snapshot.funding_weight)
                .is_some_and(|weight| weight != snapshot.funding_weight)
        {
            return Err(CorrectionFundingError::InvalidSnapshot);
        }
    }
    let mut payment_rules = BTreeMap::<u32, ResolvedFundingRule>::new();
    for (line_no, group) in &payment_groups {
        let key = (group.pool_id.clone(), group.version);
        let historical = rule_map
            .get(&key)
            .ok_or(CorrectionFundingError::InvalidSnapshot)?;
        let rule = make_funding_rule(historical, group.policy, group.weights.clone())?;
        if let Some(saved) = allocation_rules.get(&key)
            && saved != &rule
        {
            return Err(CorrectionFundingError::InvalidSnapshot);
        }
        allocation_rules.insert(key.clone(), rule.clone());
        payment_rules.insert(*line_no, rule);
    }

    let mut payments = purchase_payments.to_vec();
    payments.sort_by_key(|payment| payment.line_no);
    if payments.is_empty() || payment_groups.len() != payments.len() {
        return Err(CorrectionFundingError::InvalidSnapshot);
    }
    let mut seen_lines = BTreeMap::new();
    for payment in &payments {
        if payment.line_no == 0
            || payment.amount.value() == 0
            || seen_lines.insert(payment.line_no, payment).is_some()
            || payment_groups
                .get(&payment.line_no)
                .is_none_or(|snapshot| snapshot.account_id != payment.account_id)
        {
            return Err(CorrectionFundingError::InvalidSnapshot);
        }
    }

    let total = decision
        .amount_delta
        .value()
        .checked_abs()
        .ok_or(CorrectionFundingError::ArithmeticOverflow)?;
    let allocation_total = decision.allocations.iter().try_fold(0_i64, |sum, item| {
        sum.checked_add(item.amount.value())
            .ok_or(CorrectionFundingError::ArithmeticOverflow)
    })?;
    if total == 0 || allocation_total != total {
        return Err(CorrectionFundingError::InvalidAllocation);
    }

    let payment_weights = payments
        .iter()
        .map(|payment| (payment.line_no, payment.amount.value()))
        .collect::<BTreeMap<_, _>>();
    let mut remaining = distribute_by_key(total, &payment_weights)?;
    let correction_payment_amounts = remaining.clone();
    let mut per_payment_allocations = BTreeMap::<u32, BTreeMap<String, i64>>::new();
    let mut ordered_allocations = decision.allocations.iter().collect::<Vec<_>>();
    ordered_allocations.sort_by(|left, right| left.pool_id.cmp(&right.pool_id));
    for allocation in ordered_allocations {
        if allocation.amount.value() == 0 {
            continue;
        }
        let available = remaining
            .iter()
            .filter(|(_, amount)| **amount > 0)
            .map(|(line, amount)| (*line, *amount))
            .collect::<BTreeMap<_, _>>();
        let distributed = distribute_by_key(allocation.amount.value(), &available)?;
        for (line, amount) in distributed {
            let left = remaining
                .get_mut(&line)
                .ok_or(CorrectionFundingError::InvalidSnapshot)?;
            *left = left
                .checked_sub(amount)
                .ok_or(CorrectionFundingError::ArithmeticOverflow)?;
            per_payment_allocations
                .entry(line)
                .or_default()
                .insert(allocation.pool_id.as_str().to_owned(), amount);
        }
    }

    let mut adjustments = BTreeMap::<String, ExpenseAdjustment>::new();
    for payment in &payments {
        let paid = correction_payment_amounts
            .get(&payment.line_no)
            .copied()
            .unwrap_or_default();
        if paid == 0 {
            continue;
        }
        let payment_rule = payment_rules
            .get(&payment.line_no)
            .ok_or(CorrectionFundingError::InvalidSnapshot)?;
        let mut responsibilities = BTreeMap::<String, i64>::new();
        let mut reimbursement_monthly = BTreeMap::<String, i64>::new();
        if let Some(item_allocations) = per_payment_allocations.get(&payment.line_no) {
            for (pool_id, allocation_amount) in item_allocations {
                let saved = burdens
                    .get(pool_id)
                    .ok_or(CorrectionFundingError::InvalidSnapshot)?;
                let allocation_rule = allocation_rules
                    .get(&(pool_id.clone(), saved.version))
                    .ok_or(CorrectionFundingError::InvalidSnapshot)?;
                if payment_rule.owner_member_id.is_none()
                    && payment_rule.settlement_policy
                        == Some(CorrectionSettlementPolicy::ReimbursePersonal)
                    && allocation_rule.owner_member_id.is_none()
                {
                    continue;
                }
                for (member_id, amount) in distribute(*allocation_amount, &allocation_rule.weights)?
                {
                    checked_add_to(&mut responsibilities, member_id, amount)?;
                }
                if payment_rule.owner_member_id.is_none()
                    && payment_rule.settlement_policy
                        == Some(CorrectionSettlementPolicy::ReimbursePersonal)
                    && allocation_rule.owner_member_id.is_some()
                {
                    let owner = allocation_rule
                        .owner_member_id
                        .as_ref()
                        .ok_or(CorrectionFundingError::InvalidSnapshot)?;
                    let current = reimbursement_monthly
                        .get(owner)
                        .copied()
                        .unwrap_or_default();
                    reimbursement_monthly.insert(
                        owner.clone(),
                        current
                            .checked_add(*allocation_amount)
                            .ok_or(CorrectionFundingError::ArithmeticOverflow)?,
                    );
                }
            }
        }

        if let Some(owner) = &payment_rule.owner_member_id {
            for (member_id, burden) in &responsibilities {
                let change = if member_id == owner {
                    paid.checked_sub(*burden)
                } else {
                    burden.checked_neg()
                }
                .ok_or(CorrectionFundingError::ArithmeticOverflow)?;
                let adjustment = adjustments.entry(member_id.clone()).or_default();
                if change > 0 {
                    adjustment.advance = adjustment
                        .advance
                        .checked_add(change)
                        .ok_or(CorrectionFundingError::ArithmeticOverflow)?;
                } else if change < 0 {
                    adjustment.monthly = adjustment
                        .monthly
                        .checked_sub(change)
                        .ok_or(CorrectionFundingError::ArithmeticOverflow)?;
                }
            }
            if !responsibilities.contains_key(owner) {
                let adjustment = adjustments.entry(owner.clone()).or_default();
                adjustment.advance = adjustment
                    .advance
                    .checked_add(paid)
                    .ok_or(CorrectionFundingError::ArithmeticOverflow)?;
            }
            continue;
        }
        match payment_rule.settlement_policy {
            Some(CorrectionSettlementPolicy::BalanceToStandard) => {
                for (member_id, baseline) in distribute(paid, &payment_rule.weights)? {
                    let responsibility = responsibilities.remove(&member_id).unwrap_or_default();
                    let adjustment = adjustments.entry(member_id).or_default();
                    adjustment.monthly = adjustment
                        .monthly
                        .checked_add(responsibility - baseline)
                        .ok_or(CorrectionFundingError::ArithmeticOverflow)?;
                }
                for (member_id, responsibility) in responsibilities {
                    let adjustment = adjustments.entry(member_id).or_default();
                    adjustment.monthly = adjustment
                        .monthly
                        .checked_add(responsibility)
                        .ok_or(CorrectionFundingError::ArithmeticOverflow)?;
                }
            }
            Some(CorrectionSettlementPolicy::ReimbursePersonal) => {
                for (member_id, amount) in reimbursement_monthly {
                    let adjustment = adjustments.entry(member_id).or_default();
                    adjustment.monthly = adjustment
                        .monthly
                        .checked_add(amount)
                        .ok_or(CorrectionFundingError::ArithmeticOverflow)?;
                }
            }
            None => return Err(CorrectionFundingError::InvalidSnapshot),
        }
    }

    let mut settlement_adjustments = Vec::new();
    for (member_id, adjustment) in adjustments {
        let signed = adjustment
            .monthly
            .checked_sub(adjustment.advance)
            .ok_or(CorrectionFundingError::ArithmeticOverflow)?;
        let amount = if decision.amount_delta.value() < 0 {
            signed
                .checked_neg()
                .ok_or(CorrectionFundingError::ArithmeticOverflow)?
        } else {
            signed
        };
        if amount != 0 {
            settlement_adjustments.push(CorrectionSettlementAdjustment { member_id, amount });
        }
    }

    Ok(CorrectionFundingEffects {
        member_burdens,
        settlement_adjustments,
    })
}

#[derive(Debug, Clone, Default)]
struct ExpenseAdjustment {
    monthly: i64,
    advance: i64,
}

#[derive(Debug, Clone)]
struct SavedBurden {
    version: u32,
    policy: Option<CorrectionSettlementPolicy>,
    weights: BTreeMap<String, u64>,
}

#[derive(Debug, Clone)]
struct PaymentSnapshotGroup {
    account_id: String,
    pool_id: String,
    version: u32,
    policy: Option<CorrectionSettlementPolicy>,
    snapshot_source: String,
    weights: BTreeMap<String, u64>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct ResolvedFundingRule {
    owner_member_id: Option<String>,
    settlement_policy: Option<CorrectionSettlementPolicy>,
    weights: BTreeMap<String, u64>,
}

fn make_funding_rule(
    historical: &CorrectionFundingRule,
    snapshot_policy: Option<CorrectionSettlementPolicy>,
    weights: BTreeMap<String, u64>,
) -> Result<ResolvedFundingRule, CorrectionFundingError> {
    if weights.is_empty() || weights.values().any(|weight| *weight == 0) {
        return Err(CorrectionFundingError::InvalidSnapshot);
    }
    let (owner_member_id, settlement_policy) = match &historical.basis {
        CorrectionFundingBasis::Personal { owner_member_id } => (Some(owner_member_id), None),
        CorrectionFundingBasis::Shared { settlement_policy } => (None, Some(*settlement_policy)),
    };
    if let Some(owner) = owner_member_id {
        if settlement_policy.is_some()
            || snapshot_policy.is_some()
            || weights.len() != 1
            || weights.get(owner.as_str()) != Some(&1)
        {
            return Err(CorrectionFundingError::InvalidSnapshot);
        }
    } else if settlement_policy != snapshot_policy || snapshot_policy.is_none() {
        return Err(CorrectionFundingError::InvalidSnapshot);
    }
    Ok(ResolvedFundingRule {
        owner_member_id: owner_member_id.map(|owner| owner.as_str().to_owned()),
        settlement_policy,
        weights,
    })
}

fn distribute(
    amount: i64,
    weights: &BTreeMap<String, u64>,
) -> Result<Vec<(String, i64)>, CorrectionFundingError> {
    if amount < 0 || weights.is_empty() || weights.values().any(|weight| *weight == 0) {
        return Err(CorrectionFundingError::InvalidSnapshot);
    }
    let mut shares = Vec::with_capacity(weights.len());
    for (id, weight) in weights {
        let pool_id = AllocationPoolId::new(id.clone())
            .map_err(|_| CorrectionFundingError::InvalidSnapshot)?;
        let weight = AllocationWeight::new(pool_id, *weight)
            .map_err(|_| CorrectionFundingError::InvalidSnapshot)?;
        shares.push(weight);
    }
    let shares =
        AllocationShares::new(shares).map_err(|_| CorrectionFundingError::InvalidSnapshot)?;
    let allocations = allocate(
        Yen::new(amount).map_err(|_| CorrectionFundingError::InvalidAllocation)?,
        &shares,
    );
    Ok(allocations
        .into_iter()
        .map(|allocation| {
            (
                allocation.pool_id().as_str().to_owned(),
                allocation.amount().value(),
            )
        })
        .collect())
}

fn distribute_by_key(
    amount: i64,
    weights: &BTreeMap<u32, i64>,
) -> Result<BTreeMap<u32, i64>, CorrectionFundingError> {
    if amount < 0 || weights.is_empty() || weights.values().any(|weight| *weight <= 0) {
        return Err(CorrectionFundingError::InvalidSnapshot);
    }
    let string_weights = weights
        .iter()
        .map(|(line_no, weight)| (payment_id(*line_no), *weight as u64))
        .collect::<BTreeMap<_, _>>();
    let distributed = distribute(amount, &string_weights)?;
    distributed
        .into_iter()
        .map(|(id, amount)| {
            let line_no = id
                .parse::<u32>()
                .map_err(|_| CorrectionFundingError::InvalidSnapshot)?;
            Ok((line_no, amount))
        })
        .collect()
}

fn payment_id(line_no: u32) -> String {
    line_no.to_string()
}

fn checked_add_to(
    amounts: &mut BTreeMap<String, i64>,
    member_id: String,
    amount: i64,
) -> Result<(), CorrectionFundingError> {
    let current = amounts.get(&member_id).copied().unwrap_or_default();
    amounts.insert(
        member_id,
        current
            .checked_add(amount)
            .ok_or(CorrectionFundingError::ArithmeticOverflow)?,
    );
    Ok(())
}

#[cfg(test)]
mod tests {
    use bookkeeping_domain::{
        AccountId, AccountingPeriod, AccountingPeriodStatus, AllocationWeight,
        CorrectPurchaseError, CorrectionCommand, CorrectionPayment, ItemId, PoolId,
        PostedPurchaseReference as PostedPurchase, PurchaseId, PurchaseLifecycle, Side,
        SignedYenDelta, YearMonth, Yen, decide_correct_purchase,
    };

    use super::*;

    fn id<T>(new: impl FnOnce(String) -> Result<T, crate::IdentifierError>, value: &str) -> T {
        new(value.to_owned()).unwrap()
    }

    fn shares() -> AllocationShares {
        AllocationShares::new(vec![
            AllocationWeight::new(PoolId::new("pool-b").unwrap(), 1).unwrap(),
            AllocationWeight::new(PoolId::new("pool-a").unwrap(), 1).unwrap(),
        ])
        .unwrap()
    }

    fn period(month: &str, status: AccountingPeriodStatus) -> AccountingPeriod {
        let month = YearMonth::parse(month).unwrap();
        match status {
            AccountingPeriodStatus::Open => AccountingPeriod::open(month, 1).unwrap(),
            AccountingPeriodStatus::Closed => AccountingPeriod::closed(month, 2).unwrap(),
        }
    }

    fn command(delta: i64) -> CorrectionCommand {
        CorrectionCommand {
            purchase: PostedPurchase::new(
                id(PurchaseId::new, "purchase-1"),
                PurchaseLifecycle::Posted,
            )
            .unwrap(),
            target: CorrectionTarget::Item(id(ItemId::new, "item-1")),
            amount_delta: SignedYenDelta::new(delta).unwrap(),
            reason: "refund".into(),
            original_period: period("2026-10", AccountingPeriodStatus::Open),
            effective_period: period("2026-10", AccountingPeriodStatus::Open)
                .into_open()
                .unwrap(),
            purchase_refundable: Yen::new(100).unwrap(),
            item_refundable: Some(Yen::new(80).unwrap()),
            saved_allocations: shares(),
            requested_allocations: None,
            payments: Vec::new(),
        }
    }

    #[test]
    fn signed_delta_magnitude_covers_full_range() {
        // magnitude()内のexpectが数学的に保証されることを境界値で固定する。
        for delta in [1, -1, 1_000_000_000_000, -1_000_000_000_000] {
            let parsed = SignedYenDelta::new(delta).unwrap();
            assert_eq!(parsed.magnitude().value(), delta.abs());
        }
        assert!(SignedYenDelta::new(0).is_err());
        assert!(SignedYenDelta::new(1_000_000_000_001).is_err());
    }

    #[test]
    fn refund_decision_preserves_amount_and_balances_the_journal() {
        let decision = decide_correct_purchase(command(-31)).unwrap();

        assert_eq!(
            decision
                .allocations
                .iter()
                .map(|row| row.amount.value())
                .sum::<i64>(),
            31
        );
        assert_eq!(decision.journal_lines.len(), 3);
        assert_eq!(decision.journal_lines[0].side, Side::Credit);
        assert_eq!(decision.journal_lines[2].side, Side::Debit);
        assert_eq!(decision.journal_lines[0].line_no, 1);
        assert_eq!(decision.journal_lines[2].line_no, 3);
        let credits = decision
            .journal_lines
            .iter()
            .filter(|line| line.side == Side::Credit)
            .map(|line| line.amount.value())
            .sum::<i64>();
        let debits = decision
            .journal_lines
            .iter()
            .filter(|line| line.side == Side::Debit)
            .map(|line| line.amount.value())
            .sum::<i64>();
        assert_eq!(credits, debits);
        assert_eq!(decision.event.event_type, "purchase.corrected");
    }

    #[test]
    fn funding_effects_use_saved_weights_and_payment_rules() {
        let decision = decide_correct_purchase(command(-20)).unwrap();
        let purchase_burdens = ["pool-a", "pool-b"]
            .into_iter()
            .flat_map(|pool_id| {
                ["member-a", "member-b"].map(|member_id| CorrectionPurchaseBurdenSnapshot {
                    item_id: "item-1".into(),
                    pool_id: pool_id.into(),
                    member_id: member_id.into(),
                    funding_rule_version: 1,
                    funding_weight: 1,
                    settlement_policy: Some(CorrectionSettlementPolicy::BalanceToStandard),
                })
            })
            .collect::<Vec<_>>();
        let purchase_payments = vec![CorrectionPurchasePayment {
            line_no: 1,
            account_id: "account-1".into(),
            amount: PositiveYen::new(100).unwrap(),
        }];
        let payment_snapshots = ["member-a", "member-b"]
            .into_iter()
            .map(|member_id| CorrectionPaymentFundingSnapshot {
                line_no: 1,
                account_id: "account-1".into(),
                pool_id: "payment-pool".into(),
                member_id: member_id.into(),
                funding_rule_version: 1,
                funding_weight: 1,
                settlement_policy: Some(CorrectionSettlementPolicy::BalanceToStandard),
                snapshot_source: "POSTED".into(),
            })
            .collect::<Vec<_>>();
        let historical_rules = ["pool-a", "pool-b", "payment-pool"]
            .into_iter()
            .map(|pool_id| CorrectionFundingRule {
                pool_id: pool_id.into(),
                version: 1,
                basis: CorrectionFundingBasis::Shared {
                    settlement_policy: CorrectionSettlementPolicy::BalanceToStandard,
                },
            })
            .collect::<Vec<_>>();

        let effects = calculate_correction_funding_effects(
            &decision,
            &purchase_burdens,
            &purchase_payments,
            &payment_snapshots,
            &historical_rules,
        )
        .unwrap();

        assert_eq!(effects.member_burdens.len(), 4);
        assert!(
            effects
                .member_burdens
                .iter()
                .all(|burden| burden.amount_delta == -5)
        );
        assert!(effects.settlement_adjustments.is_empty());
    }

    #[test]
    fn correction_rejects_refunds_beyond_purchase_or_item_balance() {
        let mut purchase_overdraw = command(-101);
        purchase_overdraw.item_refundable = Some(Yen::new(200).unwrap());
        assert_eq!(
            decide_correct_purchase(purchase_overdraw),
            Err(CorrectPurchaseError::RefundExceedsPurchaseBalance)
        );

        let mut item_overdraw = command(-81);
        item_overdraw.purchase_refundable = Yen::new(200).unwrap();
        assert_eq!(
            decide_correct_purchase(item_overdraw),
            Err(CorrectPurchaseError::RefundExceedsItemBalance)
        );
    }

    #[test]
    fn correction_requires_posted_purchase_and_matching_payment_total() {
        assert_eq!(
            PostedPurchase::new(id(PurchaseId::new, "purchase-1"), PurchaseLifecycle::Draft,),
            Err(CorrectPurchaseError::PurchaseNotPosted)
        );

        let mut mismatched_period = command(50);
        mismatched_period.original_period = period("2026-10", AccountingPeriodStatus::Closed);
        assert_eq!(
            decide_correct_purchase(mismatched_period),
            Err(CorrectPurchaseError::EffectivePeriodMismatch)
        );

        let mut mismatched_payments = command(50);
        mismatched_payments.payments.push(CorrectionPayment {
            account_id: id(AccountId::new, "account-1"),
            amount: PositiveYen::new(49).unwrap(),
        });
        assert_eq!(
            decide_correct_purchase(mismatched_payments),
            Err(CorrectPurchaseError::PaymentTotalMismatch)
        );
    }
}
