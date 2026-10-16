use uuid::Uuid;

pub use crate::actor_id::ActorId;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Tag {
    pub id: TagId,
    pub household_id: HouseholdId,
    pub name: TagName,
    pub archived: bool,
    pub revision: TagRevision,
}

impl Tag {
    pub fn reconstitute(
        id: impl AsRef<str>,
        household_id: impl AsRef<str>,
        name: impl AsRef<str>,
        archived: bool,
        revision: i64,
    ) -> Result<Self, TagError> {
        Ok(Self {
            id: TagId::new(id.as_ref())?,
            household_id: HouseholdId::new(household_id.as_ref())?,
            name: TagName::new(name.as_ref())?,
            archived,
            revision: TagRevision::new(revision)?,
        })
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateTagCommand {
    pub actor_id: ActorId,
    pub household_id: HouseholdId,
    pub name: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateTagDecision {
    pub actor_id: ActorId,
    pub household_id: HouseholdId,
    pub tag_id: TagId,
    pub name: TagName,
    pub revision: TagRevision,
    pub event: TagEvent,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdateTagCommand {
    pub actor_id: ActorId,
    pub tag_id: TagId,
    pub base_revision: i32,
    pub name: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetTagArchivedCommand {
    pub actor_id: ActorId,
    pub tag_id: TagId,
    pub base_revision: i32,
    pub archived: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdateTagDecision {
    pub actor_id: ActorId,
    pub tag: Tag,
    pub event: TagEvent,
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
    InvalidRevision,
    RevisionConflict,
}

pub fn decide_create_tag(
    command: CreateTagCommand,
    tag_id: TagId,
) -> Result<CreateTagDecision, TagError> {
    let name = TagName::new(&command.name)?;

    Ok(CreateTagDecision {
        actor_id: command.actor_id,
        household_id: command.household_id,
        tag_id: tag_id.clone(),
        name,
        revision: TagRevision::new(1)?,
        event: TagEvent::Created { tag_id },
    })
}

pub fn decide_update_tag(
    command: UpdateTagCommand,
    current: Tag,
) -> Result<UpdateTagDecision, TagError> {
    let name = TagName::new(&command.name)?;
    let actor_id = command.actor_id;
    let tag_id = command.tag_id;
    let current_tag_id = current.id.clone();
    if tag_id != current_tag_id || i64::from(command.base_revision) != current.revision.as_i64() {
        return Err(TagError::RevisionConflict);
    }
    let tag = Tag {
        name,
        revision: current.revision.next()?,
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
    let actor_id = command.actor_id;
    let tag_id = command.tag_id;
    let current_tag_id = current.id.clone();
    if tag_id != current_tag_id || i64::from(command.base_revision) != current.revision.as_i64() {
        return Err(TagError::RevisionConflict);
    }
    let tag = Tag {
        archived: command.archived,
        revision: current.revision.next()?,
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

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TagName(String);

impl TagName {
    pub fn new(value: impl AsRef<str>) -> Result<Self, TagError> {
        let name = value.as_ref().trim();
        if name.is_empty() || name.chars().count() > 80 {
            return Err(TagError::InvalidName);
        }
        Ok(Self(name.to_owned()))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub struct TagRevision(i64);

impl TagRevision {
    pub fn new(value: i64) -> Result<Self, TagError> {
        if value < 1 {
            return Err(TagError::InvalidRevision);
        }
        Ok(Self(value))
    }

    pub fn as_i64(self) -> i64 {
        self.0
    }

    pub fn next(self) -> Result<Self, TagError> {
        self.0
            .checked_add(1)
            .ok_or(TagError::RevisionConflict)
            .and_then(Self::new)
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

impl From<Uuid> for HouseholdId {
    fn from(value: Uuid) -> Self {
        Self(value.to_string())
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

impl From<Uuid> for TagId {
    fn from(value: Uuid) -> Self {
        Self(value.to_string())
    }
}
