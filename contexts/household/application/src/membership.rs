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

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MembershipChangeDecision {
    pub actor_id: String,
    pub household_id: String,
    pub member_id: String,
    pub change: MembershipChange,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum MembershipChange {
    Leave,
    Remove,
    TransferOwner { new_owner_was_owner: bool },
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum MembershipError {
    InvalidIdentifier,
    Forbidden,
    SelfTransfer,
    LastOwner,
    ReferencedMember,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MembershipFacts {
    pub actor_role: Option<String>,
    pub target_role: Option<String>,
    pub owner_count: usize,
    pub has_active_funding_reference: bool,
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
    validate_identifiers(&command.member_id, &command.household_id, None)?;
    let facts = operations
        .load_facts(
            &command.household_id,
            &command.member_id,
            &command.member_id,
        )
        .await?;
    let decision = decide_leave_household(
        command,
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
    validate_identifiers(
        &command.actor_id,
        &command.household_id,
        Some(&command.member_id),
    )?;
    let facts = operations
        .load_facts(&command.household_id, &command.actor_id, &command.member_id)
        .await?;
    let decision = decide_remove_household_member(
        command,
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
    validate_identifiers(
        &command.actor_id,
        &command.household_id,
        Some(&command.new_owner_id),
    )?;
    let facts = operations
        .load_facts(
            &command.household_id,
            &command.actor_id,
            &command.new_owner_id,
        )
        .await?;
    let decision = decide_transfer_owner(
        command,
        facts.actor_role.as_deref(),
        facts.target_role.as_deref(),
    )?;
    operations.persist(decision).await
}

fn validate_identifiers<E: From<MembershipError>>(
    actor_id: &str,
    household_id: &str,
    target_id: Option<&str>,
) -> Result<(), E> {
    if actor_id.is_empty() || household_id.is_empty() || target_id.is_some_and(str::is_empty) {
        return Err(MembershipError::InvalidIdentifier.into());
    }
    Ok(())
}

pub fn decide_leave_household(
    command: LeaveHouseholdCommand,
    target_role: Option<&str>,
    owner_count: usize,
    has_active_funding_reference: bool,
) -> Result<MembershipChangeDecision, MembershipError> {
    if command.member_id.is_empty() || command.household_id.is_empty() {
        return Err(MembershipError::InvalidIdentifier);
    }
    let Some(target_role) = target_role else {
        return Err(MembershipError::Forbidden);
    };
    if target_role == "OWNER" && owner_count <= 1 {
        return Err(MembershipError::LastOwner);
    }
    if has_active_funding_reference {
        return Err(MembershipError::ReferencedMember);
    }
    Ok(MembershipChangeDecision {
        actor_id: command.member_id.clone(),
        household_id: command.household_id,
        member_id: command.member_id,
        change: MembershipChange::Leave,
    })
}

pub fn decide_remove_household_member(
    command: RemoveHouseholdMemberCommand,
    actor_role: Option<&str>,
    target_role: Option<&str>,
    owner_count: usize,
    has_active_funding_reference: bool,
) -> Result<MembershipChangeDecision, MembershipError> {
    if command.actor_id.is_empty()
        || command.household_id.is_empty()
        || command.member_id.is_empty()
    {
        return Err(MembershipError::InvalidIdentifier);
    }
    if actor_role != Some("OWNER") || target_role.is_none() {
        return Err(MembershipError::Forbidden);
    }
    if target_role == Some("OWNER") && owner_count <= 1 {
        return Err(MembershipError::LastOwner);
    }
    if has_active_funding_reference {
        return Err(MembershipError::ReferencedMember);
    }
    Ok(MembershipChangeDecision {
        actor_id: command.actor_id,
        household_id: command.household_id,
        member_id: command.member_id,
        change: MembershipChange::Remove,
    })
}

pub fn decide_transfer_owner(
    command: TransferOwnerCommand,
    actor_role: Option<&str>,
    new_owner_role: Option<&str>,
) -> Result<MembershipChangeDecision, MembershipError> {
    if command.actor_id.is_empty()
        || command.household_id.is_empty()
        || command.new_owner_id.is_empty()
    {
        return Err(MembershipError::InvalidIdentifier);
    }
    if actor_role != Some("OWNER") || new_owner_role.is_none() {
        return Err(MembershipError::Forbidden);
    }
    if command.actor_id == command.new_owner_id {
        return Err(MembershipError::SelfTransfer);
    }
    Ok(MembershipChangeDecision {
        actor_id: command.actor_id,
        household_id: command.household_id,
        member_id: command.new_owner_id,
        change: MembershipChange::TransferOwner {
            new_owner_was_owner: new_owner_role == Some("OWNER"),
        },
    })
}
