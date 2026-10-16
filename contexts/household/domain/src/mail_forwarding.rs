use horse_authorization::{Operation, authorize};
use uuid::Uuid;

macro_rules! id_type {
    ($name:ident) => {
        #[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
        pub struct $name(Uuid);

        impl $name {
            pub fn new(value: impl AsRef<str>) -> Result<Self, MailForwardingError> {
                Uuid::parse_str(value.as_ref())
                    .map(Self)
                    .map_err(|_| MailForwardingError::InvalidIdentifier)
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
id_type!(ForwardingId);

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MailboxAddress(String);

impl MailboxAddress {
    pub fn value(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct IssueCommand {
    pub actor_id: ActorId,
    pub household_id: HouseholdId,
    pub forwarding_id: ForwardingId,
    pub mailbox_domain: String,
    pub local_part_prefix: String,
    pub token: String,
    pub rotate: bool,
}

#[derive(Debug, Clone, PartialEq)]
pub struct IssueDecision {
    pub actor_id: ActorId,
    pub household_id: HouseholdId,
    pub forwarding_id: ForwardingId,
    pub address: MailboxAddress,
    pub rotated: bool,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum MailForwardingError {
    InvalidIdentifier,
    InvalidMailboxAddress,
    Forbidden,
    AlreadyActive,
    NotActive,
}

pub fn decide_access(actor_role: Option<&str>) -> Result<(), MailForwardingError> {
    authorize(actor_role, Operation::ManageMailForwarding)
        .map(|_| ())
        .map_err(|_| MailForwardingError::Forbidden)
}

pub fn decide_issue(
    command: IssueCommand,
    actor_role: Option<&str>,
    active_exists: bool,
) -> Result<IssueDecision, MailForwardingError> {
    decide_access(actor_role)?;
    if command.rotate && !active_exists {
        return Err(MailForwardingError::NotActive);
    }
    if !command.rotate && active_exists {
        return Err(MailForwardingError::AlreadyActive);
    }
    if !valid_mailbox_domain(&command.mailbox_domain)
        || !valid_local_part_prefix(&command.local_part_prefix)
        || command.token.is_empty()
        || !command
            .token
            .bytes()
            .all(|byte| byte.is_ascii_lowercase() || matches!(byte, b'2'..=b'7'))
    {
        return Err(MailForwardingError::InvalidMailboxAddress);
    }
    let local_part = format!(
        "{}{token}",
        command.local_part_prefix,
        token = command.token
    );
    let address = format!("{}@{}", local_part, command.mailbox_domain);
    if local_part.len() > 64 || address.len() > 254 {
        return Err(MailForwardingError::InvalidMailboxAddress);
    }
    Ok(IssueDecision {
        actor_id: command.actor_id,
        household_id: command.household_id,
        forwarding_id: command.forwarding_id,
        address: MailboxAddress(address),
        rotated: command.rotate,
    })
}

pub fn decide_revoke(
    actor_role: Option<&str>,
    active_exists: bool,
) -> Result<(), MailForwardingError> {
    decide_access(actor_role)?;
    if !active_exists {
        return Err(MailForwardingError::NotActive);
    }
    Ok(())
}

fn valid_mailbox_domain(domain: &str) -> bool {
    if domain.is_empty() || domain.len() > 253 || domain.starts_with('.') || domain.ends_with('.') {
        return false;
    }
    domain.split('.').all(|label| {
        !label.is_empty()
            && label.len() <= 63
            && label.as_bytes()[0].is_ascii_alphanumeric()
            && label.as_bytes()[label.len() - 1].is_ascii_alphanumeric()
            && label
                .bytes()
                .all(|byte| byte.is_ascii_alphanumeric() || byte == b'-')
    })
}

fn valid_local_part_prefix(prefix: &str) -> bool {
    if prefix.is_empty() || prefix.len() > 64 || prefix.starts_with('.') || prefix.ends_with('.') {
        return false;
    }
    let mut previous_dot = false;
    for byte in prefix.bytes() {
        if byte == b'.' {
            if previous_dot {
                return false;
            }
            previous_dot = true;
        } else {
            previous_dot = false;
            if !(byte.is_ascii_alphanumeric()
                || matches!(
                    byte,
                    b'!' | b'#'
                        | b'$'
                        | b'%'
                        | b'&'
                        | b'\''
                        | b'*'
                        | b'+'
                        | b'-'
                        | b'/'
                        | b'='
                        | b'?'
                        | b'^'
                        | b'_'
                        | b'`'
                        | b'{'
                        | b'|'
                        | b'}'
                        | b'~'
                ))
            {
                return false;
            }
        }
    }
    true
}
