pub mod budget;
pub mod create_pool;
pub mod recurring_expense;
pub mod set_allocation_rule;

pub use budget::{
    ArchiveBudgetCommand, ArchiveBudgetDecision, Budget, BudgetActorId, BudgetAmountYen,
    BudgetCategoryId, BudgetError, BudgetHouseholdId, BudgetId, BudgetName, BudgetRevision,
    CreateBudgetCommand, CreateBudgetDecision, EndDateChange, UpdateBudgetCommand,
    UpdateBudgetDecision,
};
pub use create_pool::{
    CreatePoolCommand, CreatePoolDecision, CreatePoolError, NewPoolId, PoolActorId,
    PoolHouseholdId, decide_create_pool,
};
pub use recurring_expense::{
    ArchiveRecurringExpenseCommand, CURRENCY, Cadence, CreateRecurringExpenseCommand,
    MatchRecurringExpenseCommand, MatchRecurringExpenseDecision, RecurringExpense,
    RecurringExpenseActorId, RecurringExpenseAmountYen, RecurringExpenseCategoryId,
    RecurringExpenseDecision, RecurringExpenseError, RecurringExpenseHouseholdId,
    RecurringExpenseId, RecurringExpensePaymentAccountId, RecurringExpensePurchaseId,
    RecurringExpenseRevision, UpdateRecurringExpenseCommand,
};
pub use set_allocation_rule::{
    AllocationActorId, AllocationCategoryId, AllocationHouseholdId, AllocationPoolId,
    AllocationProductConceptId, AllocationProductVariantId, AllocationRuleId,
    AllocationRuleRevision, AllocationRuleTarget, AllocationRuleWeight, AllocationRuleWeightValue,
    NormalizedMerchant, SetAllocationRuleCommand, SetAllocationRuleDecision,
    SetAllocationRuleError, SetAllocationRuleResult, ValidatedAllocationRuleTarget,
    ValidatedAllocationRuleWeight, ValidatedSetAllocationRuleCommand,
};

use chrono::NaiveDate;
use uuid::Uuid;

macro_rules! id_type {
    ($name:ident) => {
        #[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
        pub struct $name(Uuid);

        impl $name {
            pub const fn new(value: Uuid) -> Self {
                Self(value)
            }

            pub const fn value(self) -> Uuid {
                self.0
            }
        }

        impl From<Uuid> for $name {
            fn from(value: Uuid) -> Self {
                Self(value)
            }
        }
    };
}

id_type!(ActorId);
id_type!(HouseholdId);
id_type!(MemberId);
id_type!(PoolId);
id_type!(FundingScheduleId);

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct FundingScheduleAmount(i64);

impl FundingScheduleAmount {
    pub fn new(value: i64) -> Result<Self, SetFundingScheduleError> {
        if !(0..=1_000_000_000_000).contains(&value) {
            return Err(SetFundingScheduleError::InvalidAmount);
        }
        Ok(Self(value))
    }

    pub const fn value(self) -> i64 {
        self.0
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FundingScheduleCadence {
    Monthly,
    Weekly,
    Custom,
}

impl FundingScheduleCadence {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Monthly => "MONTHLY",
            Self::Weekly => "WEEKLY",
            Self::Custom => "CUSTOM",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub struct FundingScheduleDate(NaiveDate);

impl FundingScheduleDate {
    pub const fn new(value: NaiveDate) -> Self {
        Self(value)
    }

    pub const fn value(self) -> NaiveDate {
        self.0
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetFundingScheduleCommand {
    pub actor_id: ActorId,
    pub household_id: HouseholdId,
    pub member_id: MemberId,
    pub pool_id: PoolId,
    pub schedule_id: FundingScheduleId,
    pub amount: FundingScheduleAmount,
    pub cadence: FundingScheduleCadence,
    pub starts_on: FundingScheduleDate,
    pub ends_on: Option<FundingScheduleDate>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ActiveFundingSchedule {
    pub id: FundingScheduleId,
    pub starts_on: FundingScheduleDate,
    pub ends_on: Option<FundingScheduleDate>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FundingScheduleUpdate {
    pub id: FundingScheduleId,
    pub ends_on: Option<FundingScheduleDate>,
    pub active: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetFundingScheduleDecision {
    pub actor_id: ActorId,
    pub household_id: HouseholdId,
    pub member_id: MemberId,
    pub pool_id: PoolId,
    pub schedule_id: FundingScheduleId,
    pub amount: FundingScheduleAmount,
    pub cadence: FundingScheduleCadence,
    pub starts_on: FundingScheduleDate,
    pub ends_on: Option<FundingScheduleDate>,
    pub prior_updates: Vec<FundingScheduleUpdate>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SetFundingScheduleError {
    InvalidAmount,
    InvalidEndDate,
    DuplicateActiveSchedule,
    InvalidExistingSchedule,
}

pub fn decide_set_funding_schedule(
    command: SetFundingScheduleCommand,
    active_schedules: Vec<ActiveFundingSchedule>,
) -> Result<SetFundingScheduleDecision, SetFundingScheduleError> {
    if command.ends_on.is_some_and(|end| end < command.starts_on) {
        return Err(SetFundingScheduleError::InvalidEndDate);
    }
    if active_schedules.len() > 1 {
        return Err(SetFundingScheduleError::DuplicateActiveSchedule);
    }

    let mut prior_updates = Vec::with_capacity(active_schedules.len());
    for schedule in active_schedules {
        if schedule.ends_on.is_some_and(|end| end < schedule.starts_on) {
            return Err(SetFundingScheduleError::InvalidExistingSchedule);
        }

        let adjusted_end = if schedule.starts_on < command.starts_on {
            let day_before = FundingScheduleDate::new(
                command
                    .starts_on
                    .value()
                    .pred_opt()
                    .ok_or(SetFundingScheduleError::InvalidEndDate)?,
            );
            Some(
                schedule
                    .ends_on
                    .map_or(day_before, |end| end.min(day_before)),
            )
        } else {
            schedule.ends_on
        };
        prior_updates.push(FundingScheduleUpdate {
            id: schedule.id,
            ends_on: adjusted_end,
            active: false,
        });
    }

    Ok(SetFundingScheduleDecision {
        actor_id: command.actor_id,
        household_id: command.household_id,
        member_id: command.member_id,
        pool_id: command.pool_id,
        schedule_id: command.schedule_id,
        amount: command.amount,
        cadence: command.cadence,
        starts_on: command.starts_on,
        ends_on: command.ends_on,
        prior_updates,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn date(value: &str) -> FundingScheduleDate {
        FundingScheduleDate::new(NaiveDate::parse_from_str(value, "%Y-%m-%d").unwrap())
    }

    fn command(starts_on: &str) -> SetFundingScheduleCommand {
        SetFundingScheduleCommand {
            actor_id: Uuid::nil().into(),
            household_id: Uuid::nil().into(),
            member_id: Uuid::nil().into(),
            pool_id: Uuid::nil().into(),
            schedule_id: Uuid::nil().into(),
            amount: FundingScheduleAmount::new(100).unwrap(),
            cadence: FundingScheduleCadence::Monthly,
            starts_on: date(starts_on),
            ends_on: None,
        }
    }

    #[test]
    fn closes_previous_active_schedule_before_new_start() {
        let decision = decide_set_funding_schedule(
            command("2024-03-01"),
            vec![ActiveFundingSchedule {
                id: Uuid::nil().into(),
                starts_on: date("2024-01-01"),
                ends_on: None,
            }],
        )
        .expect("valid schedule");

        assert_eq!(decision.prior_updates[0].ends_on, Some(date("2024-02-29")));
        assert!(!decision.prior_updates[0].active);
    }

    #[test]
    fn deactivates_future_active_schedule_without_changing_its_range() {
        let decision = decide_set_funding_schedule(
            command("2024-02-01"),
            vec![ActiveFundingSchedule {
                id: Uuid::nil().into(),
                starts_on: date("2024-03-01"),
                ends_on: Some(date("2024-04-30")),
            }],
        )
        .expect("valid schedule");

        assert_eq!(decision.prior_updates[0].ends_on, Some(date("2024-04-30")));
        assert!(!decision.prior_updates[0].active);
    }

    #[test]
    fn rejects_end_dates_before_start_and_duplicate_active_schedules() {
        let mut invalid = command("2024-02-01");
        invalid.ends_on = Some(date("2024-01-31"));
        assert_eq!(
            decide_set_funding_schedule(invalid, vec![]),
            Err(SetFundingScheduleError::InvalidEndDate)
        );

        assert_eq!(
            decide_set_funding_schedule(
                command("2024-02-01"),
                vec![
                    ActiveFundingSchedule {
                        id: Uuid::nil().into(),
                        starts_on: date("2024-01-01"),
                        ends_on: None,
                    },
                    ActiveFundingSchedule {
                        id: Uuid::nil().into(),
                        starts_on: date("2024-01-01"),
                        ends_on: None,
                    },
                ],
            ),
            Err(SetFundingScheduleError::DuplicateActiveSchedule)
        );
    }
}
