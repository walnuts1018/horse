use bookkeeping_domain::{
    SettlementInput as DomainSettlementInput, SettlementPolicy, calculate_settlement,
};
use std::collections::{BTreeMap, BTreeSet};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PreviewMember {
    pub id: String,
    pub name: String,
    pub left_at_micros: Option<i64>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PreviewObligation {
    pub member_id: usize,
    pub pool_id: String,
    pub scheduled_yen: i64,
    pub paid_yen: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PreviewSchedule {
    pub member_id: usize,
    pub pool_id: String,
    pub amount_yen: i64,
    pub cadence: PreviewScheduleCadence,
    pub starts_on_day: i32,
    pub ends_on_day: Option<i32>,
    pub active: bool,
    pub created_at_micros: i64,
    pub id: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct PreviewAdjustment {
    pub member_id: usize,
    pub amount_yen: i64,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct PreviewPurchaseAdjustment {
    pub member_id: usize,
    pub monthly_adjustment_yen: i64,
    pub advance_credit_yen: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PreviewFundingRule {
    pub id: String,
    pub kind: PreviewFundingRuleKind,
    pub owner_member_id: Option<String>,
    pub settlement_policy: Option<SettlementPolicy>,
    pub shares: Vec<(String, i64)>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PreviewScheduleCadence {
    Monthly,
    Weekly,
    Custom,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PreviewFundingRuleKind {
    Personal,
    Shared,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PreviewPurchase {
    pub payments: Vec<PreviewPurchasePayment>,
    pub items: Vec<PreviewPurchaseItem>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PreviewPurchasePayment {
    pub payment_rule_id: String,
    pub amount_yen: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PreviewPurchaseItem {
    pub amount_yen: i64,
    pub burdens: Vec<(String, String, i64)>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PreviewInput {
    pub period: String,
    pub members: Vec<PreviewMember>,
    pub obligations: Vec<PreviewObligation>,
    pub schedules: Vec<PreviewSchedule>,
    pub adjustments: Vec<PreviewAdjustment>,
    pub purchase_adjustments: Vec<PreviewPurchaseAdjustment>,
    pub funding_rules: Vec<PreviewFundingRule>,
    pub purchases: Vec<PreviewPurchase>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PreviewAmount {
    pub member_id: String,
    pub member_name: String,
    pub scheduled_yen: i64,
    pub paid_yen: i64,
    pub carry_yen: i64,
    pub monthly_adjustment_yen: i64,
    pub advance_credit_yen: i64,
    pub amount_due_yen: i64,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SettlementPreviewError {
    InvalidPeriod,
    InvalidMemberIndex,
    InvalidSchedule,
    InvalidSettlement,
    AmountOverflow,
}

pub fn calculate_settlement_preview(
    mut input: PreviewInput,
    previous_due_by_member: &BTreeMap<String, i64>,
) -> Result<Vec<PreviewAmount>, SettlementPreviewError> {
    let (year, month) = parse_period(&input.period).ok_or(SettlementPreviewError::InvalidPeriod)?;
    let days_in_month = days_in_month(year, month);
    let period_start = day_number(year, month, 1);
    let period_end = period_start + days_in_month;

    let mut totals = vec![Totals::default(); input.members.len()];
    let mut active = BTreeSet::new();
    for (index, member) in input.members.iter().enumerate() {
        let participates_in_period = member
            .left_at_micros
            .is_none_or(|left| left > period_start as i64 * MICROS_PER_DAY - JST_OFFSET_MICROS);
        let has_unsettled_balance = previous_due_by_member
            .get(&member.id)
            .is_some_and(|amount| *amount != 0);
        if participates_in_period || has_unsettled_balance {
            active.insert(index);
        }
    }
    let mut funding = BTreeMap::<(usize, String), Totals>::new();
    for obligation in input.obligations {
        if obligation.member_id >= input.members.len() || !active.contains(&obligation.member_id) {
            return Err(SettlementPreviewError::InvalidMemberIndex);
        }
        funding.insert(
            (obligation.member_id, obligation.pool_id),
            Totals {
                scheduled: obligation.scheduled_yen,
                paid: obligation.paid_yen,
                ..Totals::default()
            },
        );
    }

    input.schedules.sort_by(|left, right| {
        (
            left.member_id,
            &left.pool_id,
            left.starts_on_day,
            left.created_at_micros,
            &left.id,
        )
            .cmp(&(
                right.member_id,
                &right.pool_id,
                right.starts_on_day,
                right.created_at_micros,
                &right.id,
            ))
    });
    let mut latest = BTreeMap::<(usize, String), PreviewSchedule>::new();
    for schedule in input.schedules {
        if schedule.member_id >= input.members.len()
            || schedule.amount_yen < 0
            || schedule
                .ends_on_day
                .is_some_and(|end| end < schedule.starts_on_day)
        {
            return Err(SettlementPreviewError::InvalidSchedule);
        }
        let ends_on = schedule.ends_on_day;
        if (!schedule.active && ends_on.is_none())
            || schedule.starts_on_day >= period_end
            || ends_on.is_some_and(|end| end < period_end - 1)
        {
            continue;
        }
        let key = (schedule.member_id, schedule.pool_id.clone());
        latest.insert(key, schedule);
    }
    for (key, schedule) in latest {
        if !active.contains(&key.0) {
            return Err(SettlementPreviewError::InvalidMemberIndex);
        }
        let scheduled = schedule_amount(period_start, period_end, &schedule)?;
        let entry = funding.entry(key).or_default();
        entry.scheduled = entry.scheduled.max(scheduled);
    }
    for ((member_id, _), value) in funding {
        let total = &mut totals[member_id];
        total.scheduled = total
            .scheduled
            .checked_add(value.scheduled)
            .ok_or(SettlementPreviewError::AmountOverflow)?;
        total.paid = total
            .paid
            .checked_add(value.paid)
            .ok_or(SettlementPreviewError::AmountOverflow)?;
    }
    for adjustment in input.adjustments {
        if adjustment.member_id >= input.members.len() || !active.contains(&adjustment.member_id) {
            return Err(SettlementPreviewError::InvalidMemberIndex);
        }
        totals[adjustment.member_id].monthly = totals[adjustment.member_id]
            .monthly
            .checked_add(adjustment.amount_yen)
            .ok_or(SettlementPreviewError::AmountOverflow)?;
    }
    let mut purchase_adjustments = input.purchase_adjustments;
    purchase_adjustments.extend(calculate_purchase_adjustments(
        &input.purchases,
        &input.funding_rules,
        &input.members,
    )?);
    for adjustment in purchase_adjustments {
        if adjustment.member_id >= input.members.len() || !active.contains(&adjustment.member_id) {
            return Err(SettlementPreviewError::InvalidMemberIndex);
        }
        let total = &mut totals[adjustment.member_id];
        total.monthly = total
            .monthly
            .checked_add(adjustment.monthly_adjustment_yen)
            .ok_or(SettlementPreviewError::AmountOverflow)?;
        total.advance = total
            .advance
            .checked_add(adjustment.advance_credit_yen)
            .ok_or(SettlementPreviewError::AmountOverflow)?;
    }

    let mut result = Vec::with_capacity(active.len());
    for index in active {
        let member = &input.members[index];
        let carry = previous_due_by_member
            .get(&member.id)
            .copied()
            .unwrap_or_default();
        let value = &totals[index];
        let amounts = calculate_settlement(DomainSettlementInput {
            scheduled_yen: value.scheduled,
            paid_yen: value.paid,
            previous_owed_yen: carry,
            monthly_adjustment_yen: value.monthly,
            advance_credit_yen: value.advance,
        })
        .map_err(|_| SettlementPreviewError::InvalidSettlement)?;
        result.push(PreviewAmount {
            member_id: member.id.clone(),
            member_name: member.name.clone(),
            scheduled_yen: value.scheduled,
            paid_yen: value.paid,
            carry_yen: amounts.carry_yen,
            monthly_adjustment_yen: value.monthly,
            advance_credit_yen: value.advance,
            amount_due_yen: amounts.amount_due_yen,
        });
    }
    Ok(result)
}

fn calculate_purchase_adjustments(
    purchases: &[PreviewPurchase],
    rules: &[PreviewFundingRule],
    members: &[PreviewMember],
) -> Result<Vec<PreviewPurchaseAdjustment>, SettlementPreviewError> {
    let rules = rules
        .iter()
        .map(|rule| (rule.id.as_str(), rule))
        .collect::<BTreeMap<_, _>>();
    let mut totals = BTreeMap::<String, (i64, i64)>::new();
    for purchase in purchases {
        if purchase.payments.is_empty() || purchase.items.is_empty() {
            return Err(SettlementPreviewError::InvalidSettlement);
        }
        let mut payment_remaining = purchase
            .payments
            .iter()
            .map(|payment| payment.amount_yen)
            .collect::<Vec<_>>();
        if payment_remaining.iter().any(|amount| *amount <= 0) {
            return Err(SettlementPreviewError::InvalidSettlement);
        }
        let mut expense_items = vec![Vec::<(String, String, i64)>::new(); purchase.payments.len()];
        let mut total_items = 0_i64;
        for item in &purchase.items {
            if item.amount_yen < 0 || item.burdens.is_empty() {
                return Err(SettlementPreviewError::InvalidSettlement);
            }
            let mut burden_sum = 0_i64;
            let mut burdens = item.burdens.clone();
            burdens.sort();
            for (allocation_rule_id, member_id, amount) in burdens {
                if allocation_rule_id.is_empty() || member_id.is_empty() || amount < 0 {
                    return Err(SettlementPreviewError::InvalidSettlement);
                }
                burden_sum = add_amount(burden_sum, amount)?;
                if amount == 0 {
                    continue;
                }
                let weights = payment_remaining
                    .iter()
                    .enumerate()
                    .filter(|(_, amount)| **amount > 0)
                    .map(|(index, amount)| {
                        (purchase.payments[index].payment_rule_id.clone(), *amount)
                    })
                    .collect::<Vec<_>>();
                let allocated = allocate_amount(amount, &weights)?;
                for (payment_id, part) in allocated {
                    let index = purchase
                        .payments
                        .iter()
                        .position(|payment| payment.payment_rule_id == payment_id)
                        .ok_or(SettlementPreviewError::InvalidSettlement)?;
                    payment_remaining[index] = payment_remaining[index]
                        .checked_sub(part)
                        .ok_or(SettlementPreviewError::AmountOverflow)?;
                    if part != 0 {
                        expense_items[index].push((
                            allocation_rule_id.clone(),
                            member_id.clone(),
                            part,
                        ));
                    }
                }
            }
            if burden_sum != item.amount_yen {
                return Err(SettlementPreviewError::InvalidSettlement);
            }
            total_items = add_amount(total_items, item.amount_yen)?;
        }
        let paid_total = purchase
            .payments
            .iter()
            .try_fold(0_i64, |sum, payment| add_amount(sum, payment.amount_yen))?;
        if total_items != paid_total || payment_remaining.iter().any(|remaining| *remaining != 0) {
            return Err(SettlementPreviewError::InvalidSettlement);
        }
        for (payment, burdens) in purchase.payments.iter().zip(expense_items) {
            let rule = rules
                .get(payment.payment_rule_id.as_str())
                .ok_or(SettlementPreviewError::InvalidSettlement)?;
            let mut responsibilities = BTreeMap::<String, i64>::new();
            let mut reimburse_personal_adjustments = BTreeMap::<String, i64>::new();
            for (allocation_rule_id, member_id, burden) in burdens {
                let allocation_rule = rules
                    .get(allocation_rule_id.as_str())
                    .ok_or(SettlementPreviewError::InvalidSettlement)?;
                if rule.kind == PreviewFundingRuleKind::Shared
                    && rule.settlement_policy == Some(SettlementPolicy::ReimbursePersonal)
                {
                    if allocation_rule.kind == PreviewFundingRuleKind::Shared {
                        continue;
                    }
                    let current = reimburse_personal_adjustments
                        .get(&member_id)
                        .copied()
                        .unwrap_or_default();
                    reimburse_personal_adjustments
                        .insert(member_id.clone(), add_amount(current, burden)?);
                    continue;
                }
                let current = responsibilities
                    .get(&member_id)
                    .copied()
                    .unwrap_or_default();
                responsibilities.insert(member_id, add_amount(current, burden)?);
            }
            match rule.kind {
                PreviewFundingRuleKind::Personal => {
                    let owner = rule
                        .owner_member_id
                        .as_ref()
                        .ok_or(SettlementPreviewError::InvalidSettlement)?;
                    for (member_id, burden) in &responsibilities {
                        let change = if member_id == owner {
                            payment
                                .amount_yen
                                .checked_sub(*burden)
                                .ok_or(SettlementPreviewError::AmountOverflow)?
                        } else {
                            -*burden
                        };
                        let entry = totals.entry(member_id.clone()).or_default();
                        if change > 0 {
                            entry.1 = add_amount(entry.1, change)?;
                        }
                        if change < 0 {
                            entry.0 = add_amount(
                                entry.0,
                                change
                                    .checked_neg()
                                    .ok_or(SettlementPreviewError::AmountOverflow)?,
                            )?;
                        }
                    }
                    if !responsibilities.contains_key(owner) {
                        let entry = totals.entry(owner.clone()).or_default();
                        entry.1 = add_amount(entry.1, payment.amount_yen)?;
                    }
                }
                PreviewFundingRuleKind::Shared => match rule.settlement_policy {
                    Some(SettlementPolicy::BalanceToStandard) => {
                        for (member_id, baseline) in
                            allocate_amount(payment.amount_yen, &rule.shares)?
                        {
                            let burden = responsibilities.remove(&member_id).unwrap_or_default();
                            let entry = totals.entry(member_id).or_default();
                            entry.0 = add_amount(
                                entry.0,
                                burden
                                    .checked_sub(baseline)
                                    .ok_or(SettlementPreviewError::AmountOverflow)?,
                            )?;
                        }
                        for (member_id, burden) in responsibilities {
                            let entry = totals.entry(member_id).or_default();
                            entry.0 = add_amount(entry.0, burden)?;
                        }
                    }
                    Some(SettlementPolicy::ReimbursePersonal) => {
                        for (member_id, amount) in reimburse_personal_adjustments {
                            let entry = totals.entry(member_id).or_default();
                            entry.0 = add_amount(entry.0, amount)?;
                        }
                    }
                    _ => return Err(SettlementPreviewError::InvalidSettlement),
                },
            }
        }
    }
    totals
        .into_iter()
        .map(
            |(member_id, (monthly_adjustment_yen, advance_credit_yen))| {
                let member_id = members
                    .iter()
                    .position(|member| member.id == member_id)
                    .ok_or(SettlementPreviewError::InvalidMemberIndex)?;
                Ok(PreviewPurchaseAdjustment {
                    member_id,
                    monthly_adjustment_yen,
                    advance_credit_yen,
                })
            },
        )
        .collect()
}

fn add_amount(left: i64, right: i64) -> Result<i64, SettlementPreviewError> {
    left.checked_add(right)
        .ok_or(SettlementPreviewError::AmountOverflow)
}

fn allocate_amount(
    amount: i64,
    weights: &[(String, i64)],
) -> Result<Vec<(String, i64)>, SettlementPreviewError> {
    if amount == 0 && weights.is_empty() {
        return Ok(Vec::new());
    }
    if amount < 0 || weights.is_empty() || weights.iter().any(|(_, weight)| *weight <= 0) {
        return Err(SettlementPreviewError::InvalidSettlement);
    }
    let total = weights.iter().try_fold(0_i128, |sum, (_, weight)| {
        sum.checked_add(i128::from(*weight))
            .ok_or(SettlementPreviewError::AmountOverflow)
    })?;
    let mut parts = weights
        .iter()
        .map(|(id, weight)| {
            let numerator = i128::from(amount) * i128::from(*weight);
            (id.clone(), (numerator / total) as i64, numerator % total)
        })
        .collect::<Vec<_>>();
    let allocated = parts
        .iter()
        .try_fold(0_i64, |sum, (_, part, _)| add_amount(sum, *part))?;
    let remainder = amount
        .checked_sub(allocated)
        .ok_or(SettlementPreviewError::AmountOverflow)? as usize;
    parts.sort_by(|left, right| right.2.cmp(&left.2).then_with(|| left.0.cmp(&right.0)));
    for (_, part, _) in parts.iter_mut().take(remainder) {
        *part += 1;
    }
    Ok(parts
        .into_iter()
        .map(|(id, amount, _)| (id, amount))
        .collect())
}

#[derive(Clone, Default)]
struct Totals {
    scheduled: i64,
    paid: i64,
    monthly: i64,
    advance: i64,
}

const MICROS_PER_DAY: i64 = 86_400_000_000;
const JST_OFFSET_MICROS: i64 = 9 * 60 * 60 * 1_000_000;

fn schedule_amount(
    start: i32,
    end: i32,
    schedule: &PreviewSchedule,
) -> Result<i64, SettlementPreviewError> {
    let schedule_start = schedule.starts_on_day;
    let schedule_end = schedule.ends_on_day.unwrap_or(i32::MAX);
    match schedule.cadence {
        PreviewScheduleCadence::Monthly => {
            if schedule_start < end && schedule_end >= start {
                Ok(schedule.amount_yen)
            } else {
                Ok(0)
            }
        }
        PreviewScheduleCadence::Custom => {
            if schedule_start >= start && schedule_start < end && schedule_end >= schedule_start {
                Ok(schedule.amount_yen)
            } else {
                Ok(0)
            }
        }
        PreviewScheduleCadence::Weekly => {
            let days = (start - schedule_start).max(0);
            let mut occurrence = schedule_start
                .checked_add((days / 7 + i32::from(days % 7 != 0)) * 7)
                .ok_or(SettlementPreviewError::AmountOverflow)?;
            let mut total = 0_i64;
            while occurrence < end && occurrence <= schedule_end {
                total = total
                    .checked_add(schedule.amount_yen)
                    .ok_or(SettlementPreviewError::AmountOverflow)?;
                occurrence = occurrence
                    .checked_add(7)
                    .ok_or(SettlementPreviewError::AmountOverflow)?;
            }
            Ok(total)
        }
    }
}

fn parse_period(value: &str) -> Option<(i32, u32)> {
    let (year, month) = value.split_once('-')?;
    if year.len() != 4 || month.len() != 2 {
        return None;
    }
    let year = year.parse::<i32>().ok()?;
    let month = month.parse::<u32>().ok()?;
    (1..=12).contains(&month).then_some((year, month))
}

fn days_in_month(year: i32, month: u32) -> i32 {
    let leap = year % 4 == 0 && (year % 100 != 0 || year % 400 == 0);
    match month {
        2 if leap => 29,
        2 => 28,
        4 | 6 | 9 | 11 => 30,
        _ => 31,
    }
}

fn day_number(year: i32, month: u32, day: u32) -> i32 {
    let adjusted_year = year - if month <= 2 { 1 } else { 0 };
    let era = adjusted_year.div_euclid(400);
    let year_of_era = adjusted_year - era * 400;
    let adjusted_month = month as i32 + if month > 2 { -3 } else { 9 };
    let day_of_year = (153 * adjusted_month + 2) / 5 + day as i32 - 1;
    let day_of_era = year_of_era * 365 + year_of_era / 4 - year_of_era / 100 + day_of_year;
    era * 146_097 + day_of_era - 719_468
}

#[cfg(test)]
mod tests {
    use super::{PreviewInput, PreviewMember, PreviewObligation, calculate_settlement_preview};
    use std::collections::BTreeMap;

    #[test]
    fn preview_uses_previous_due_and_current_payment_shortfall() {
        let input = PreviewInput {
            period: "2026-10".to_owned(),
            members: vec![PreviewMember {
                id: "member-1".to_owned(),
                name: "Member".to_owned(),
                left_at_micros: None,
            }],
            obligations: vec![PreviewObligation {
                member_id: 0,
                pool_id: "pool-1".to_owned(),
                scheduled_yen: 10_000,
                paid_yen: 8_000,
            }],
            schedules: vec![],
            adjustments: vec![],
            purchase_adjustments: vec![],
            funding_rules: vec![],
            purchases: vec![],
        };
        let previous_due = BTreeMap::from([("member-1".to_owned(), 2_000)]);

        let result = calculate_settlement_preview(input, &previous_due).unwrap();

        assert_eq!(result.len(), 1);
        assert_eq!(result[0].scheduled_yen, 10_000);
        assert_eq!(result[0].paid_yen, 8_000);
        assert_eq!(result[0].carry_yen, 2_000);
        assert_eq!(result[0].amount_due_yen, 4_000);
    }

    #[test]
    fn preview_carries_departed_members_unsettled_balance_without_new_scheduled_funding() {
        let input = PreviewInput {
            period: "2026-10".to_owned(),
            members: vec![PreviewMember {
                id: "member-1".to_owned(),
                name: "Member".to_owned(),
                left_at_micros: Some(0),
            }],
            obligations: vec![],
            schedules: vec![],
            adjustments: vec![],
            purchase_adjustments: vec![],
            funding_rules: vec![],
            purchases: vec![],
        };
        let previous_due = BTreeMap::from([("member-1".to_owned(), 2_000)]);

        let result = calculate_settlement_preview(input, &previous_due).unwrap();

        assert_eq!(result.len(), 1);
        assert_eq!(result[0].member_id, "member-1");
        assert_eq!(result[0].scheduled_yen, 0);
        assert_eq!(result[0].amount_due_yen, 2_000);
    }
}
