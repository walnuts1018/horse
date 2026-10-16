use horse_household_change::UiChangeDescriptor;
use uuid::Uuid;

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

impl SetProductAliasDecision {
    pub fn ui_change(&self) -> UiChangeDescriptor {
        UiChangeDescriptor::new(
            "product.alias.learned",
            "product_alias",
            ["alias", "variantId", "merchant"],
        )
    }
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
    if Uuid::parse_str(&command.actor_id).is_err() {
        return Err(SetProductAliasError::InvalidActorId);
    }
    if Uuid::parse_str(&command.household_id).is_err() {
        return Err(SetProductAliasError::InvalidHouseholdId);
    }
    if Uuid::parse_str(&command.variant_id).is_err() {
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
            actor_id: "00000000-0000-4000-8000-000000000001".to_owned(),
            household_id: "00000000-0000-4000-8000-000000000002".to_owned(),
            variant_id: "00000000-0000-4000-8000-000000000003".to_owned(),
            merchant: "  SHOP   NAME ".to_owned(),
            alias: "  Product   Name  ".to_owned(),
        })
        .expect("valid alias");

        assert_eq!(decision.merchant_key, "shop name");
        assert_eq!(decision.alias_text, "Product   Name");
        assert_eq!(decision.normalized_alias, "product name");
    }
}
