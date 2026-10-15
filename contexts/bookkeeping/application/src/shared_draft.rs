pub const MAX_SHARED_PURCHASE_DRAFT_INPUT_BYTES: usize = 256 * 1024;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum DraftReferenceKind {
    Account,
    Category,
    Pool,
    ProductVariant,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DraftReference {
    pub kind: DraftReferenceKind,
    pub id: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateSharedPurchaseDraftCommand {
    pub actor_id: String,
    pub household_id: String,
    pub draft_id: String,
    pub payload: String,
    pub references: Vec<DraftReference>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateSharedPurchaseDraftDecision {
    pub actor_id: String,
    pub household_id: String,
    pub draft_id: String,
    pub payload: String,
    pub references: Vec<DraftReference>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct GetSharedPurchaseDraftCommand {
    pub actor_id: String,
    pub draft_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdateSharedPurchaseDraftCommand {
    pub actor_id: String,
    pub draft_id: String,
    pub expected_revision: i64,
    pub payload: String,
    pub references: Vec<DraftReference>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdateSharedPurchaseDraftDecision {
    pub actor_id: String,
    pub draft_id: String,
    pub expected_revision: i64,
    pub payload: String,
    pub references: Vec<DraftReference>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SharedPurchaseDraftError {
    InvalidActorId,
    InvalidHouseholdId,
    InvalidDraftId,
    InvalidPayload,
}

pub fn decide_create_shared_purchase_draft(
    command: CreateSharedPurchaseDraftCommand,
) -> Result<CreateSharedPurchaseDraftDecision, SharedPurchaseDraftError> {
    if command.actor_id.is_empty() {
        return Err(SharedPurchaseDraftError::InvalidActorId);
    }
    if command.household_id.is_empty() {
        return Err(SharedPurchaseDraftError::InvalidHouseholdId);
    }
    if command.draft_id.is_empty() {
        return Err(SharedPurchaseDraftError::InvalidDraftId);
    }
    validate_payload(&command.payload, &command.references)?;
    Ok(CreateSharedPurchaseDraftDecision {
        actor_id: command.actor_id,
        household_id: command.household_id,
        draft_id: command.draft_id,
        payload: command.payload,
        references: command.references,
    })
}

pub fn decide_get_shared_purchase_draft(
    command: GetSharedPurchaseDraftCommand,
) -> Result<GetSharedPurchaseDraftCommand, SharedPurchaseDraftError> {
    if command.actor_id.is_empty() {
        return Err(SharedPurchaseDraftError::InvalidActorId);
    }
    if command.draft_id.is_empty() {
        return Err(SharedPurchaseDraftError::InvalidDraftId);
    }
    Ok(command)
}

pub fn decide_update_shared_purchase_draft(
    command: UpdateSharedPurchaseDraftCommand,
) -> Result<UpdateSharedPurchaseDraftDecision, SharedPurchaseDraftError> {
    if command.actor_id.is_empty() {
        return Err(SharedPurchaseDraftError::InvalidActorId);
    }
    if command.draft_id.is_empty() {
        return Err(SharedPurchaseDraftError::InvalidDraftId);
    }
    Ok(UpdateSharedPurchaseDraftDecision {
        actor_id: command.actor_id,
        draft_id: command.draft_id,
        expected_revision: command.expected_revision,
        payload: command.payload,
        references: command.references,
    })
}

pub fn validate_shared_purchase_draft_payload(
    payload: &str,
    references: &[DraftReference],
) -> Result<(), SharedPurchaseDraftError> {
    validate_payload(payload, references)
}

fn validate_payload(
    payload: &str,
    references: &[DraftReference],
) -> Result<(), SharedPurchaseDraftError> {
    if payload.is_empty() || payload.len() > MAX_SHARED_PURCHASE_DRAFT_INPUT_BYTES {
        return Err(SharedPurchaseDraftError::InvalidPayload);
    }
    if references.iter().any(|reference| reference.id.is_empty()) {
        return Err(SharedPurchaseDraftError::InvalidPayload);
    }
    Ok(())
}
