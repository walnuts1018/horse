use uuid::Uuid;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetCardLast4Command {
    pub actor_id: String,
    pub account_id: String,
    pub account_kind: String,
    pub current: Option<String>,
    pub next: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetCardLast4Decision {
    pub actor_id: String,
    pub account_id: String,
    pub card_last4: Option<String>,
    pub changed: bool,
    pub event: Option<AccountCardLast4Updated>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountCardLast4Updated {
    pub event_type: &'static str,
    pub entity_id: String,
    pub revision: u64,
    pub changed_fields: [&'static str; 1],
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SetCardLast4Error {
    InvalidIdentifier,
    InvalidCardLast4,
    CardAccountRequired,
}

pub fn decide_set_card_last4(
    command: SetCardLast4Command,
) -> Result<SetCardLast4Decision, SetCardLast4Error> {
    let actor_id = ActorId::new(command.actor_id)?;
    let account_id = AccountId::new(command.account_id)?;
    if command.account_kind != "CARD" {
        return Err(SetCardLast4Error::CardAccountRequired);
    }
    if command.next.as_ref().is_some_and(|value| {
        value.len() != 4 || !value.bytes().all(|character| character.is_ascii_digit())
    }) {
        return Err(SetCardLast4Error::InvalidCardLast4);
    }

    let changed = command.current != command.next;
    let event = changed.then(|| AccountCardLast4Updated {
        event_type: "account.card_last4_updated",
        entity_id: account_id.0.clone(),
        revision: 1,
        changed_fields: ["cardLast4"],
    });
    Ok(SetCardLast4Decision {
        actor_id: actor_id.0,
        account_id: account_id.0,
        card_last4: command.next,
        changed,
        event,
    })
}
#[derive(Debug, Clone, PartialEq, Eq)]
struct ActorId(String);

impl ActorId {
    fn new(value: String) -> Result<Self, SetCardLast4Error> {
        Uuid::parse_str(&value).map_err(|_| SetCardLast4Error::InvalidIdentifier)?;
        Ok(Self(value))
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct AccountId(String);

impl AccountId {
    fn new(value: String) -> Result<Self, SetCardLast4Error> {
        Uuid::parse_str(&value).map_err(|_| SetCardLast4Error::InvalidIdentifier)?;
        Ok(Self(value))
    }
}
