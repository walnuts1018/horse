use uuid::Uuid;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Tag {
    pub id: String,
    pub household_id: String,
    pub name: String,
    pub archived: bool,
    pub revision: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateTagCommand {
    pub actor_id: String,
    pub household_id: String,
    pub name: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateTagDecision {
    pub actor_id: String,
    pub household_id: String,
    pub tag_id: String,
    pub name: String,
    pub revision: i64,
    pub event_type: &'static str,
    pub changed_fields: Vec<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdateTagCommand {
    pub actor_id: String,
    pub tag_id: String,
    pub base_revision: i32,
    pub name: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetTagArchivedCommand {
    pub actor_id: String,
    pub tag_id: String,
    pub base_revision: i32,
    pub archived: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdateTagDecision {
    pub actor_id: String,
    pub tag: Tag,
    pub event_type: &'static str,
    pub changed_fields: Vec<String>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TagError {
    InvalidActorId,
    InvalidHouseholdId,
    InvalidTagId,
    InvalidName,
    RevisionConflict,
}

pub fn decide_create_tag(
    command: CreateTagCommand,
    tag_id: String,
) -> Result<CreateTagDecision, TagError> {
    let actor_id = ActorId::new(&command.actor_id)?;
    let household_id = HouseholdId::new(&command.household_id)?;
    let tag_id = TagId::new(&tag_id)?;
    let name = validated_name(command.name)?;

    Ok(CreateTagDecision {
        actor_id: actor_id.0,
        household_id: household_id.0,
        tag_id: tag_id.0,
        name,
        revision: 1,
        event_type: "tag.created",
        changed_fields: vec!["name".to_owned()],
    })
}

pub fn decide_update_tag(
    command: UpdateTagCommand,
    current: Tag,
) -> Result<UpdateTagDecision, TagError> {
    let name = validate_tag_name(&command.name)?;
    let actor_id = ActorId::new(&command.actor_id)?;
    let tag_id = TagId::new(&command.tag_id)?;
    let current_tag_id = TagId::new(&current.id)?;
    HouseholdId::new(&current.household_id)?;
    if tag_id != current_tag_id || i64::from(command.base_revision) != current.revision {
        return Err(TagError::RevisionConflict);
    }
    let tag = Tag {
        name,
        revision: current
            .revision
            .checked_add(1)
            .ok_or(TagError::RevisionConflict)?,
        ..current
    };

    Ok(UpdateTagDecision {
        actor_id: actor_id.0,
        tag,
        event_type: "tag.updated",
        changed_fields: vec!["name".to_owned()],
    })
}

pub fn decide_set_tag_archived(
    command: SetTagArchivedCommand,
    current: Tag,
) -> Result<UpdateTagDecision, TagError> {
    let actor_id = ActorId::new(&command.actor_id)?;
    let tag_id = TagId::new(&command.tag_id)?;
    let current_tag_id = TagId::new(&current.id)?;
    HouseholdId::new(&current.household_id)?;
    if tag_id != current_tag_id || i64::from(command.base_revision) != current.revision {
        return Err(TagError::RevisionConflict);
    }
    let tag = Tag {
        archived: command.archived,
        revision: current
            .revision
            .checked_add(1)
            .ok_or(TagError::RevisionConflict)?,
        ..current
    };

    Ok(UpdateTagDecision {
        actor_id: actor_id.0,
        tag,
        event_type: "tag.archived",
        changed_fields: vec!["archived".to_owned()],
    })
}

pub fn validate_tag_name(value: &str) -> Result<String, TagError> {
    let name = value.trim();
    if name.is_empty() || name.chars().count() > 80 {
        return Err(TagError::InvalidName);
    }
    Ok(name.to_owned())
}

fn validated_name(value: String) -> Result<String, TagError> {
    validate_tag_name(&value)
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct ActorId(String);

impl ActorId {
    fn new(value: &str) -> Result<Self, TagError> {
        Uuid::parse_str(value).map_err(|_| TagError::InvalidActorId)?;
        Ok(Self(value.to_owned()))
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct HouseholdId(String);

impl HouseholdId {
    fn new(value: &str) -> Result<Self, TagError> {
        Uuid::parse_str(value).map_err(|_| TagError::InvalidHouseholdId)?;
        Ok(Self(value.to_owned()))
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct TagId(String);

impl TagId {
    fn new(value: &str) -> Result<Self, TagError> {
        Uuid::parse_str(value).map_err(|_| TagError::InvalidTagId)?;
        Ok(Self(value.to_owned()))
    }
}
