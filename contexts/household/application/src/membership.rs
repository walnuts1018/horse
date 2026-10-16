use household_domain::membership::{self, ActorId, HouseholdId, MemberId};
pub use household_domain::membership::{
    MembershipChange, MembershipChangeDecision, MembershipError, MembershipFacts,
};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LeaveHouseholdCommand {
    pub member_id: String,
    pub household_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RemoveHouseholdMemberCommand {
    pub actor_id: String,
    pub household_id: String,
    pub member_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TransferOwnerCommand {
    pub actor_id: String,
    pub household_id: String,
    pub new_owner_id: String,
}

pub trait LeaveHouseholdOperations {
    type Error: From<MembershipError>;

    async fn load_facts(
        &mut self,
        household_id: &str,
        actor_id: &str,
        target_id: &str,
    ) -> Result<MembershipFacts, Self::Error>;
    async fn persist(&mut self, decision: MembershipChangeDecision) -> Result<String, Self::Error>;
}

pub async fn leave_household<O: LeaveHouseholdOperations>(
    operations: &mut O,
    command: LeaveHouseholdCommand,
) -> Result<String, O::Error> {
    let actor_id = ActorId::new(&command.member_id)?;
    let household_id = HouseholdId::new(&command.household_id)?;
    let member_id = MemberId::new(&command.member_id)?;
    let facts = operations
        .load_facts(
            &command.household_id,
            &command.member_id,
            &command.member_id,
        )
        .await?;
    let decision = membership::decide_leave_household(
        actor_id,
        household_id,
        member_id,
        facts.target_role.as_deref(),
        facts.owner_count,
        facts.has_active_funding_reference,
    )?;
    operations.persist(decision).await
}

pub trait RemoveHouseholdMemberOperations {
    type Error: From<MembershipError>;

    async fn load_facts(
        &mut self,
        household_id: &str,
        actor_id: &str,
        target_id: &str,
    ) -> Result<MembershipFacts, Self::Error>;
    async fn persist(&mut self, decision: MembershipChangeDecision) -> Result<String, Self::Error>;
}

pub async fn remove_household_member<O: RemoveHouseholdMemberOperations>(
    operations: &mut O,
    command: RemoveHouseholdMemberCommand,
) -> Result<String, O::Error> {
    let actor_id = ActorId::new(&command.actor_id)?;
    let household_id = HouseholdId::new(&command.household_id)?;
    let member_id = MemberId::new(&command.member_id)?;
    let facts = operations
        .load_facts(&command.household_id, &command.actor_id, &command.member_id)
        .await?;
    let decision = membership::decide_remove_household_member(
        actor_id,
        household_id,
        member_id,
        facts.actor_role.as_deref(),
        facts.target_role.as_deref(),
        facts.owner_count,
        facts.has_active_funding_reference,
    )?;
    operations.persist(decision).await
}

pub trait TransferHouseholdOwnerOperations {
    type Error: From<MembershipError>;

    async fn load_facts(
        &mut self,
        household_id: &str,
        actor_id: &str,
        target_id: &str,
    ) -> Result<MembershipFacts, Self::Error>;
    async fn persist(&mut self, decision: MembershipChangeDecision) -> Result<String, Self::Error>;
}

pub async fn transfer_owner<O: TransferHouseholdOwnerOperations>(
    operations: &mut O,
    command: TransferOwnerCommand,
) -> Result<String, O::Error> {
    let actor_id = ActorId::new(&command.actor_id)?;
    let household_id = HouseholdId::new(&command.household_id)?;
    let new_owner_id = MemberId::new(&command.new_owner_id)?;
    let facts = operations
        .load_facts(
            &command.household_id,
            &command.actor_id,
            &command.new_owner_id,
        )
        .await?;
    let decision = membership::decide_transfer_owner(
        actor_id,
        household_id,
        new_owner_id,
        facts.actor_role.as_deref(),
        facts.target_role.as_deref(),
    )?;
    operations.persist(decision).await
}
