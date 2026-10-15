use bookkeeping_domain::{
    AccountingPeriod, AccountingPeriodStatus, AccountingPeriodTransitionError, SettlementError,
    SettlementInput as DomainSettlementInput, YearMonth,
    calculate_settlement as calculate_domain_settlement,
};

use crate::{MemberId, PurchaseId};
use std::collections::BTreeMap;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SettlementInput {
    pub member_id: MemberId,
    pub member_name: String,
    pub scheduled_yen: i64,
    pub paid_yen: i64,
    pub previous_owed_yen: i64,
    pub monthly_adjustment_yen: i64,
    pub advance_credit_yen: i64,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FundingCadence {
    Monthly,
    Weekly,
    Custom,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FundingScheduleInput {
    pub id: String,
    pub member_id: MemberId,
    pub pool_id: String,
    pub amount_yen: i64,
    pub cadence: FundingCadence,
    pub starts_on_day: i32,
    pub ends_on_day: Option<i32>,
    pub active: bool,
    pub created_at_micros: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FundingObligationDecision {
    pub schedule_id: String,
    pub member_id: MemberId,
    pub pool_id: String,
    pub amount_yen: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PurchaseSnapshot {
    pub id: PurchaseId,
    pub revision: u64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ClosePeriodCommand {
    pub period: AccountingPeriod,
    pub expected_revision: u64,
    pub draft_purchases: Vec<PurchaseId>,
    pub actor_id: MemberId,
    pub settlements: Vec<SettlementInput>,
    pub purchases: Vec<PurchaseSnapshot>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SettlementLine {
    pub member_id: MemberId,
    pub member_name: String,
    pub scheduled_yen: i64,
    pub paid_yen: i64,
    pub carry_yen: i64,
    pub monthly_adjustment_yen: i64,
    pub advance_credit_yen: i64,
    pub amount_due_yen: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PeriodClosedEvent {
    pub actor_id: MemberId,
    pub period: YearMonth,
    pub revision: u64,
    pub changed_fields: &'static [&'static str],
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ClosePeriodDecision {
    pub period: AccountingPeriod,
    pub settlements: Vec<SettlementLine>,
    pub purchases: Vec<PurchaseSnapshot>,
    pub event: PeriodClosedEvent,
}

pub trait ClosePeriodOperations {
    type Error;
    type Result;

    async fn persist(&mut self, decision: ClosePeriodDecision)
    -> Result<Self::Result, Self::Error>;
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ClosePeriodError {
    InvalidPeriod,
    PeriodAlreadyClosed,
    RevisionMismatch { expected: u64, actual: u64 },
    DraftPurchasesExist,
    EmptyMemberId,
    EmptyMemberName,
    DuplicateMember,
    DuplicatePurchase,
    NegativeNonnegativeAmount,
    AmountOverflow,
    RevisionOverflow,
    InvalidFundingSchedule,
    FundingAmountOverflow,
}

pub fn decide_funding_obligations(
    period_start_day: i32,
    period_end_day: i32,
    schedules: Vec<FundingScheduleInput>,
) -> Result<Vec<FundingObligationDecision>, ClosePeriodError> {
    if period_start_day >= period_end_day {
        return Err(ClosePeriodError::InvalidPeriod);
    }
    let month_end_day = period_end_day
        .checked_sub(1)
        .ok_or(ClosePeriodError::InvalidPeriod)?;

    let mut newest_by_member_pool = BTreeMap::<(MemberId, String), FundingScheduleInput>::new();
    for schedule in schedules {
        if schedule.id.is_empty()
            || schedule.member_id.as_str().is_empty()
            || schedule.pool_id.is_empty()
            || schedule.amount_yen < 0
            || schedule
                .ends_on_day
                .is_some_and(|ends_on| ends_on < schedule.starts_on_day)
        {
            return Err(ClosePeriodError::InvalidFundingSchedule);
        }
        if (!schedule.active && schedule.ends_on_day.is_none())
            || schedule.starts_on_day >= period_end_day
            || schedule
                .ends_on_day
                .is_some_and(|ends_on| ends_on < month_end_day)
        {
            continue;
        }

        let key = (schedule.member_id.clone(), schedule.pool_id.clone());
        let is_newer = newest_by_member_pool.get(&key).is_none_or(|current| {
            (
                schedule.starts_on_day,
                schedule.created_at_micros,
                &schedule.id,
            ) > (
                current.starts_on_day,
                current.created_at_micros,
                &current.id,
            )
        });
        if is_newer {
            newest_by_member_pool.insert(key, schedule);
        }
    }

    newest_by_member_pool
        .into_values()
        .map(|schedule| {
            let amount_yen = funding_schedule_amount(period_start_day, period_end_day, &schedule)?;
            Ok(FundingObligationDecision {
                schedule_id: schedule.id,
                member_id: schedule.member_id,
                pool_id: schedule.pool_id,
                amount_yen,
            })
        })
        .collect()
}

fn funding_schedule_amount(
    period_start_day: i32,
    period_end_day: i32,
    schedule: &FundingScheduleInput,
) -> Result<i64, ClosePeriodError> {
    match schedule.cadence {
        FundingCadence::Monthly => Ok(schedule.amount_yen),
        FundingCadence::Custom => {
            if schedule.starts_on_day < period_start_day || schedule.starts_on_day >= period_end_day
            {
                Ok(0)
            } else {
                Ok(schedule.amount_yen)
            }
        }
        FundingCadence::Weekly => {
            let mut occurrence = schedule.starts_on_day;
            if occurrence < period_start_day {
                let days = period_start_day - occurrence;
                let weeks = (days + 6) / 7;
                occurrence = occurrence
                    .checked_add(
                        weeks
                            .checked_mul(7)
                            .ok_or(ClosePeriodError::InvalidFundingSchedule)?,
                    )
                    .ok_or(ClosePeriodError::InvalidFundingSchedule)?;
            }
            let mut total = 0_i64;
            while occurrence < period_end_day
                && schedule
                    .ends_on_day
                    .is_none_or(|ends_on| occurrence <= ends_on)
            {
                total = total
                    .checked_add(schedule.amount_yen)
                    .ok_or(ClosePeriodError::FundingAmountOverflow)?;
                occurrence = occurrence
                    .checked_add(7)
                    .ok_or(ClosePeriodError::InvalidFundingSchedule)?;
            }
            Ok(total)
        }
    }
}

pub fn decide_close_period(
    command: ClosePeriodCommand,
) -> Result<ClosePeriodDecision, ClosePeriodError> {
    if command.period.status() == AccountingPeriodStatus::Closed {
        return Err(ClosePeriodError::PeriodAlreadyClosed);
    }
    if command.period.revision() != command.expected_revision {
        return Err(ClosePeriodError::RevisionMismatch {
            expected: command.expected_revision,
            actual: command.period.revision(),
        });
    }
    if !command.draft_purchases.is_empty() {
        return Err(ClosePeriodError::DraftPurchasesExist);
    }
    let period = command
        .period
        .close(command.expected_revision)
        .map_err(|error| match error {
            AccountingPeriodTransitionError::AlreadyClosed => ClosePeriodError::PeriodAlreadyClosed,
            AccountingPeriodTransitionError::RevisionMismatch => {
                ClosePeriodError::RevisionMismatch {
                    expected: command.expected_revision,
                    actual: command.period.revision(),
                }
            }
            AccountingPeriodTransitionError::RevisionOverflow => ClosePeriodError::RevisionOverflow,
        })?;
    let revision = period.revision();

    let mut settlements = command
        .settlements
        .into_iter()
        .map(calculate_close_settlement)
        .collect::<Result<Vec<_>, _>>()?;
    settlements.sort_by(|left, right| left.member_id.cmp(&right.member_id));
    if settlements
        .windows(2)
        .any(|pair| pair[0].member_id == pair[1].member_id)
    {
        return Err(ClosePeriodError::DuplicateMember);
    }

    let mut purchase_ids = command
        .purchases
        .iter()
        .map(|purchase| purchase.id.clone())
        .collect::<Vec<_>>();
    purchase_ids.sort();
    if purchase_ids.windows(2).any(|pair| pair[0] == pair[1]) {
        return Err(ClosePeriodError::DuplicatePurchase);
    }

    let mut purchases = command.purchases;
    purchases.sort_by(|left, right| left.id.cmp(&right.id));

    Ok(ClosePeriodDecision {
        period: period.clone(),
        settlements,
        purchases,
        event: PeriodClosedEvent {
            actor_id: command.actor_id,
            period: period.year_month(),
            revision,
            changed_fields: &["status"],
        },
    })
}

pub async fn close_period<O: ClosePeriodOperations>(
    operations: &mut O,
    command: ClosePeriodCommand,
) -> Result<O::Result, ClosePeriodWorkflowError<O::Error>> {
    let decision = decide_close_period(command).map_err(ClosePeriodWorkflowError::Decision)?;
    operations
        .persist(decision)
        .await
        .map_err(ClosePeriodWorkflowError::Operations)
}

#[derive(Debug)]
pub enum ClosePeriodWorkflowError<E> {
    Decision(ClosePeriodError),
    Operations(E),
}

fn calculate_close_settlement(input: SettlementInput) -> Result<SettlementLine, ClosePeriodError> {
    if input.member_id.as_str().is_empty() {
        return Err(ClosePeriodError::EmptyMemberId);
    }
    if input.member_name.is_empty() {
        return Err(ClosePeriodError::EmptyMemberName);
    }
    let amounts = calculate_domain_settlement(DomainSettlementInput {
        scheduled_yen: input.scheduled_yen,
        paid_yen: input.paid_yen,
        previous_owed_yen: input.previous_owed_yen,
        monthly_adjustment_yen: input.monthly_adjustment_yen,
        advance_credit_yen: input.advance_credit_yen,
    })
    .map_err(|error| match error {
        SettlementError::NegativeNonnegativeAmount => ClosePeriodError::NegativeNonnegativeAmount,
        SettlementError::AmountOverflow => ClosePeriodError::AmountOverflow,
    })?;

    Ok(SettlementLine {
        member_id: input.member_id,
        member_name: input.member_name,
        scheduled_yen: input.scheduled_yen,
        paid_yen: input.paid_yen,
        carry_yen: amounts.carry_yen,
        monthly_adjustment_yen: input.monthly_adjustment_yen,
        advance_credit_yen: input.advance_credit_yen,
        amount_due_yen: amounts.amount_due_yen,
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{MemberId, PurchaseId};

    fn member(value: &str) -> MemberId {
        MemberId::new(value).unwrap()
    }

    fn purchase(value: &str) -> PurchaseId {
        PurchaseId::new(value).unwrap()
    }

    fn command() -> ClosePeriodCommand {
        ClosePeriodCommand {
            period: AccountingPeriod::open(YearMonth::new(2025, 2).unwrap(), 3).unwrap(),
            expected_revision: 3,
            draft_purchases: Vec::new(),
            actor_id: member("actor"),
            settlements: vec![SettlementInput {
                member_id: member("member-b"),
                member_name: "B".into(),
                scheduled_yen: 10_000,
                paid_yen: 8_000,
                previous_owed_yen: -500,
                monthly_adjustment_yen: 300,
                advance_credit_yen: 100,
            }],
            purchases: vec![PurchaseSnapshot {
                id: purchase("purchase-1"),
                revision: 8,
            }],
        }
    }

    #[test]
    fn closing_preserves_purchase_revision_in_the_period_snapshot() {
        let decision = decide_close_period(command()).unwrap();

        assert_eq!(decision.period.revision(), 4);
        assert_eq!(decision.settlements[0].amount_due_yen, 1_700);
        assert_eq!(decision.purchases[0].revision, 8);
        assert_eq!(decision.event.actor_id.as_str(), "actor");
        assert_eq!(decision.event.changed_fields, &["status"]);
    }

    #[test]
    fn closing_rejects_stale_periods_drafts_and_duplicate_members() {
        let mut stale = command();
        stale.expected_revision -= 1;
        assert!(matches!(
            decide_close_period(stale),
            Err(ClosePeriodError::RevisionMismatch { .. })
        ));

        let mut with_draft = command();
        with_draft.draft_purchases.push(purchase("draft-1"));
        assert_eq!(
            decide_close_period(with_draft),
            Err(ClosePeriodError::DraftPurchasesExist)
        );

        let mut duplicate = command();
        duplicate.settlements.push(duplicate.settlements[0].clone());
        assert_eq!(
            decide_close_period(duplicate),
            Err(ClosePeriodError::DuplicateMember)
        );
    }

    #[test]
    fn closing_rejects_invalid_period_and_amount_overflow() {
        assert!(YearMonth::new(2025, 13).is_err());

        let mut overflow = command();
        overflow.settlements[0].scheduled_yen = i64::MAX;
        overflow.settlements[0].previous_owed_yen = 1;
        assert_eq!(
            decide_close_period(overflow),
            Err(ClosePeriodError::AmountOverflow)
        );
    }

    #[test]
    fn funding_obligations_use_latest_schedule_and_include_weekly_occurrences() {
        let member_id = member("11111111-1111-4111-8111-111111111111");
        let decisions = decide_funding_obligations(
            100,
            131,
            vec![
                FundingScheduleInput {
                    id: "old".into(),
                    member_id: member_id.clone(),
                    pool_id: "pool".into(),
                    amount_yen: 100,
                    cadence: FundingCadence::Monthly,
                    starts_on_day: 90,
                    ends_on_day: None,
                    active: true,
                    created_at_micros: 1,
                },
                FundingScheduleInput {
                    id: "new".into(),
                    member_id,
                    pool_id: "pool".into(),
                    amount_yen: 10,
                    cadence: FundingCadence::Weekly,
                    starts_on_day: 95,
                    ends_on_day: None,
                    active: true,
                    created_at_micros: 2,
                },
            ],
        )
        .unwrap();

        assert_eq!(decisions.len(), 1);
        assert_eq!(decisions[0].schedule_id, "new");
        assert_eq!(decisions[0].amount_yen, 50);
    }

    #[test]
    fn funding_obligations_ignore_schedules_ending_before_month_end() {
        let member_id = member("11111111-1111-4111-8111-111111111111");
        let decisions = decide_funding_obligations(
            100,
            131,
            vec![
                FundingScheduleInput {
                    id: "month-end".into(),
                    member_id: member_id.clone(),
                    pool_id: "pool".into(),
                    amount_yen: 30,
                    cadence: FundingCadence::Monthly,
                    starts_on_day: 90,
                    ends_on_day: Some(130),
                    active: false,
                    created_at_micros: 1,
                },
                FundingScheduleInput {
                    id: "ended-mid-month".into(),
                    member_id,
                    pool_id: "pool".into(),
                    amount_yen: 100,
                    cadence: FundingCadence::Monthly,
                    starts_on_day: 120,
                    ends_on_day: Some(125),
                    active: false,
                    created_at_micros: 2,
                },
            ],
        )
        .unwrap();

        assert_eq!(decisions.len(), 1);
        assert_eq!(decisions[0].schedule_id, "month-end");
        assert_eq!(decisions[0].amount_yen, 30);
    }
}
