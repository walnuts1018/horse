#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateAccountCommand {
    pub actor_id: String,
    pub household_id: String,
    pub name: String,
    pub kind: String,
    pub card_last4: Option<String>,
    pub pool_id: Option<String>,
    pub opening_balance: Option<OpeningBalance>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct OpeningBalance {
    pub amount_yen: i64,
    pub at: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateAccountDecision {
    pub actor_id: String,
    pub household_id: String,
    pub name: String,
    pub kind: String,
    pub card_last4: Option<String>,
    pub pool_id: Option<String>,
    pub opening_balance: Option<OpeningBalance>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CreateAccountError {
    InvalidActorId,
    InvalidHouseholdId,
    InvalidName,
    InvalidKind,
    InvalidCardLast4,
    InvalidPoolId,
    InvalidOpeningBalance,
    InvalidOpeningDate,
}

pub fn decide_create_account(
    command: CreateAccountCommand,
) -> Result<CreateAccountDecision, CreateAccountError> {
    if command.actor_id.is_empty() {
        return Err(CreateAccountError::InvalidActorId);
    }
    if command.household_id.is_empty() {
        return Err(CreateAccountError::InvalidHouseholdId);
    }
    let name = command.name.trim();
    if name.is_empty() || name.len() > 120 {
        return Err(CreateAccountError::InvalidName);
    }
    if !matches!(command.kind.as_str(), "BANK" | "CASH" | "CARD" | "OTHER") {
        return Err(CreateAccountError::InvalidKind);
    }
    if command.card_last4.as_ref().is_some_and(|value| {
        command.kind != "CARD"
            || value.len() != 4
            || !value.bytes().all(|character| character.is_ascii_digit())
    }) {
        return Err(CreateAccountError::InvalidCardLast4);
    }
    if command.pool_id.as_ref().is_some_and(String::is_empty) {
        return Err(CreateAccountError::InvalidPoolId);
    }
    if let Some(opening) = &command.opening_balance {
        if !(1..=1_000_000_000_000).contains(&opening.amount_yen) {
            return Err(CreateAccountError::InvalidOpeningBalance);
        }
        if opening.at.is_empty() {
            return Err(CreateAccountError::InvalidOpeningDate);
        }
    }

    Ok(CreateAccountDecision {
        actor_id: command.actor_id,
        household_id: command.household_id,
        name: name.to_owned(),
        kind: command.kind,
        card_last4: command.card_last4,
        pool_id: command.pool_id,
        opening_balance: command.opening_balance,
    })
}
