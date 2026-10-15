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
    validate_id(&command.actor_id).map_err(|_| TagError::InvalidActorId)?;
    validate_id(&command.household_id).map_err(|_| TagError::InvalidHouseholdId)?;
    validate_id(&tag_id).map_err(|_| TagError::InvalidTagId)?;
    let name = validated_name(command.name)?;

    Ok(CreateTagDecision {
        actor_id: command.actor_id,
        household_id: command.household_id,
        tag_id,
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
    validate_id(&command.actor_id).map_err(|_| TagError::InvalidActorId)?;
    validate_id(&command.tag_id).map_err(|_| TagError::InvalidTagId)?;
    if command.tag_id != current.id || i64::from(command.base_revision) != current.revision {
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
        actor_id: command.actor_id,
        tag,
        event_type: "tag.updated",
        changed_fields: vec!["name".to_owned()],
    })
}

pub fn decide_set_tag_archived(
    command: SetTagArchivedCommand,
    current: Tag,
) -> Result<UpdateTagDecision, TagError> {
    validate_id(&command.actor_id).map_err(|_| TagError::InvalidActorId)?;
    validate_id(&command.tag_id).map_err(|_| TagError::InvalidTagId)?;
    if command.tag_id != current.id || i64::from(command.base_revision) != current.revision {
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
        actor_id: command.actor_id,
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

fn validate_id(value: &str) -> Result<(), ()> {
    if value.is_empty() { Err(()) } else { Ok(()) }
}
