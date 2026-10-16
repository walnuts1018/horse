use chrono::{DateTime, Utc};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Command {
    pub actor_id: String,
    pub household_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct IssueCommand {
    pub command: Command,
    pub mailbox_domain: String,
    pub local_part_prefix: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Status {
    pub id: String,
    pub active: bool,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ForwardingAddress {
    pub id: String,
    pub address: String,
    pub created_at: DateTime<Utc>,
}
