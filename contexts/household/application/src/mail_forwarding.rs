use uuid::Uuid;

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
pub struct Decision {
    pub actor_id: Uuid,
    pub household_id: Uuid,
    pub forwarding_id: Uuid,
    pub address: String,
    pub event_type: &'static str,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Status {
    pub id: String,
    pub active: bool,
    pub created_at: chrono::DateTime<chrono::Utc>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ForwardingAddress {
    pub id: String,
    pub address: String,
    pub created_at: chrono::DateTime<chrono::Utc>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Error {
    InvalidIdentifier,
    InvalidMailboxAddress,
    Forbidden,
    AlreadyActive,
    NotActive,
}

pub fn decide_active(command: Command, actor_role: Option<&str>) -> Result<(Uuid, Uuid), Error> {
    let actor_id = parse_id(&command.actor_id)?;
    let household_id = parse_id(&command.household_id)?;
    if actor_role != Some("OWNER") {
        return Err(Error::Forbidden);
    }
    Ok((actor_id, household_id))
}

pub fn decide_issue(
    command: IssueCommand,
    actor_role: Option<&str>,
    active_exists: bool,
    forwarding_id: Uuid,
    token: &str,
    rotate: bool,
) -> Result<Decision, Error> {
    let actor_id = parse_id(&command.command.actor_id)?;
    let household_id = parse_id(&command.command.household_id)?;
    if actor_role != Some("OWNER") {
        return Err(Error::Forbidden);
    }
    if rotate && !active_exists {
        return Err(Error::NotActive);
    }
    if !rotate && active_exists {
        return Err(Error::AlreadyActive);
    }
    if !valid_mailbox_domain(&command.mailbox_domain)
        || !valid_local_part_prefix(&command.local_part_prefix)
        || token.is_empty()
        || !token
            .bytes()
            .all(|byte| byte.is_ascii_lowercase() || matches!(byte, b'2'..=b'7'))
    {
        return Err(Error::InvalidMailboxAddress);
    }
    let address = format!(
        "{}{token}@{}",
        command.local_part_prefix, command.mailbox_domain
    );
    let local_part = format!("{}{token}", command.local_part_prefix);
    if local_part.len() > 64 || address.len() > 254 {
        return Err(Error::InvalidMailboxAddress);
    }
    Ok(Decision {
        actor_id,
        household_id,
        forwarding_id,
        address,
        event_type: if rotate {
            "household.mail_forwarding.rotated"
        } else {
            "household.mail_forwarding.issued"
        },
    })
}

pub fn decide_revoke(
    command: Command,
    actor_role: Option<&str>,
    active_exists: bool,
) -> Result<(Uuid, Uuid), Error> {
    let actor_id = parse_id(&command.actor_id)?;
    let household_id = parse_id(&command.household_id)?;
    if actor_role != Some("OWNER") {
        return Err(Error::Forbidden);
    }
    if !active_exists {
        return Err(Error::NotActive);
    }
    Ok((actor_id, household_id))
}

fn parse_id(value: &str) -> Result<Uuid, Error> {
    Uuid::parse_str(value).map_err(|_| Error::InvalidIdentifier)
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
