#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetProductAliasCommand {
    pub actor_id: String,
    pub household_id: String,
    pub variant_id: String,
    pub merchant: String,
    pub alias: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetProductAliasDecision {
    pub actor_id: String,
    pub household_id: String,
    pub variant_id: String,
    pub merchant_key: String,
    pub alias_text: String,
    pub normalized_alias: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SetProductAliasError {
    InvalidActorId,
    InvalidHouseholdId,
    InvalidVariantId,
    InvalidMerchant,
    InvalidAlias,
}

pub fn decide_set_product_alias(
    command: SetProductAliasCommand,
) -> Result<SetProductAliasDecision, SetProductAliasError> {
    if command.actor_id.is_empty() {
        return Err(SetProductAliasError::InvalidActorId);
    }
    if command.household_id.is_empty() {
        return Err(SetProductAliasError::InvalidHouseholdId);
    }
    if command.variant_id.is_empty() {
        return Err(SetProductAliasError::InvalidVariantId);
    }

    let merchant_key = normalize_text(&command.merchant);
    if merchant_key.is_empty() || merchant_key.chars().count() > 240 {
        return Err(SetProductAliasError::InvalidMerchant);
    }
    let alias_text = command.alias.trim().to_owned();
    if alias_text.is_empty() || alias_text.chars().count() > 240 {
        return Err(SetProductAliasError::InvalidAlias);
    }
    let normalized_alias = normalize_text(&alias_text);

    Ok(SetProductAliasDecision {
        actor_id: command.actor_id,
        household_id: command.household_id,
        variant_id: command.variant_id,
        merchant_key,
        alias_text,
        normalized_alias,
    })
}

fn normalize_text(value: &str) -> String {
    value
        .split_whitespace()
        .map(str::to_lowercase)
        .collect::<Vec<_>>()
        .join(" ")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn normalizes_merchant_and_alias_for_persistence() {
        let decision = decide_set_product_alias(SetProductAliasCommand {
            actor_id: "actor".to_owned(),
            household_id: "household".to_owned(),
            variant_id: "variant".to_owned(),
            merchant: "  SHOP   NAME ".to_owned(),
            alias: "  Product   Name  ".to_owned(),
        })
        .expect("valid alias");

        assert_eq!(decision.merchant_key, "shop name");
        assert_eq!(decision.alias_text, "Product   Name");
        assert_eq!(decision.normalized_alias, "product name");
    }
}
