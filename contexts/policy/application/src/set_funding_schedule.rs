use horse_household_change::UiChangeDescriptor;
pub use policy_domain::{
    ActiveFundingSchedule, ActorId, FundingScheduleAmount, FundingScheduleCadence,
    FundingScheduleDate, FundingScheduleId, FundingScheduleUpdate, HouseholdId, MemberId, PoolId,
    SetFundingScheduleCommand, SetFundingScheduleDecision, SetFundingScheduleError,
};

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum SetFundingScheduleWorkflowError<E> {
    Decision(SetFundingScheduleError),
    Operations(E),
}

pub trait SetFundingScheduleOperations {
    type Error;
    type Output;

    async fn load_authorized_active_schedules(
        &mut self,
        actor_id: ActorId,
        household_id: HouseholdId,
        member_id: MemberId,
        pool_id: PoolId,
    ) -> Result<Vec<ActiveFundingSchedule>, Self::Error>;

    async fn persist(
        &mut self,
        decision: SetFundingScheduleDecision,
        change: UiChangeDescriptor,
    ) -> Result<Self::Output, Self::Error>;
}

pub async fn set_funding_schedule<O: SetFundingScheduleOperations>(
    operations: &mut O,
    command: SetFundingScheduleCommand,
) -> Result<O::Output, SetFundingScheduleWorkflowError<O::Error>> {
    let active_schedules = operations
        .load_authorized_active_schedules(
            command.actor_id,
            command.household_id,
            command.member_id,
            command.pool_id,
        )
        .await
        .map_err(SetFundingScheduleWorkflowError::Operations)?;
    let decision = policy_domain::decide_set_funding_schedule(command, active_schedules)
        .map_err(SetFundingScheduleWorkflowError::Decision)?;
    let change = funding_schedule_ui_change();
    operations
        .persist(decision, change)
        .await
        .map_err(SetFundingScheduleWorkflowError::Operations)
}

fn funding_schedule_ui_change() -> UiChangeDescriptor {
    UiChangeDescriptor::new(
        "funding.schedule.updated",
        "fundingSchedule",
        ["amountYen", "startsOn", "endsOn"],
    )
}
