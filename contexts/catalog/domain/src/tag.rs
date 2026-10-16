use horse_household_change::UiChangeDescriptor;
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
    pub actor_id: ActorId,
    pub household_id: HouseholdId,
    pub tag_id: TagId,
    pub name: TagName,
    pub revision: i64,
    pub event: TagEvent,
}

impl CreateTagDecision {
    pub fn ui_change(&self) -> UiChangeDescriptor {
        UiChangeDescriptor::new("tag.created", "tag", ["name"])
    }
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
    pub actor_id: ActorId,
    pub tag: Tag,
    pub event: TagEvent,
}

impl UpdateTagDecision {
    pub fn ui_change(&self) -> UiChangeDescriptor {
        match &self.event {
            TagEvent::NameUpdated { .. } => UiChangeDescriptor::new("tag.updated", "tag", ["name"]),
            TagEvent::ArchiveStateChanged { .. } => {
                UiChangeDescriptor::new("tag.archived", "tag", ["archived"])
            }
            TagEvent::Created { .. } => UiChangeDescriptor::new("tag.created", "tag", ["name"]),
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum TagEvent {
    Created { tag_id: TagId },
    NameUpdated { tag_id: TagId },
    ArchiveStateChanged { tag_id: TagId, archived: bool },
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
        actor_id,
        household_id,
        tag_id: tag_id.clone(),
        name: TagName(name),
        revision: 1,
        event: TagEvent::Created { tag_id },
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
        actor_id,
        tag,
        event: TagEvent::NameUpdated {
            tag_id: current_tag_id,
        },
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
        actor_id,
        tag,
        event: TagEvent::ArchiveStateChanged {
            tag_id: current_tag_id,
            archived: command.archived,
        },
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
pub struct TagName(String);

impl TagName {
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ActorId(String);

impl ActorId {
    fn new(value: &str) -> Result<Self, TagError> {
        Uuid::parse_str(value).map_err(|_| TagError::InvalidActorId)?;
        Ok(Self(value.to_owned()))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HouseholdId(String);

impl HouseholdId {
    fn new(value: &str) -> Result<Self, TagError> {
        Uuid::parse_str(value).map_err(|_| TagError::InvalidHouseholdId)?;
        Ok(Self(value.to_owned()))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TagId(String);

impl TagId {
    fn new(value: &str) -> Result<Self, TagError> {
        Uuid::parse_str(value).map_err(|_| TagError::InvalidTagId)?;
        Ok(Self(value.to_owned()))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}
