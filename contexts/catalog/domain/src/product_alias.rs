use horse_household_change::UiChangeDescriptor;
use uuid::Uuid;

macro_rules! alias_value {
    ($name:ident) => {
        #[derive(Debug, Clone, PartialEq, Eq)]
        pub struct $name(String);

        impl $name {
            fn new(value: String) -> Self {
                Self(value)
            }

            pub fn as_str(&self) -> &str {
                &self.0
            }
        }
    };
}

alias_value!(ActorId);
alias_value!(HouseholdId);
alias_value!(VariantId);
alias_value!(MerchantKey);
alias_value!(AliasText);
alias_value!(NormalizedAlias);

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
    pub actor_id: ActorId,
    pub household_id: HouseholdId,
    pub variant_id: VariantId,
    pub merchant_key: MerchantKey,
    pub alias_text: AliasText,
    pub normalized_alias: NormalizedAlias,
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
        actor_id: ActorId::new(command.actor_id),
        household_id: HouseholdId::new(command.household_id),
        variant_id: VariantId::new(command.variant_id),
        merchant_key: MerchantKey::new(merchant_key),
        alias_text: AliasText::new(alias_text),
        normalized_alias: NormalizedAlias::new(normalized_alias),
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

        assert_eq!(decision.merchant_key.as_str(), "shop name");
        assert_eq!(decision.alias_text.as_str(), "Product   Name");
        assert_eq!(decision.normalized_alias.as_str(), "product name");
    }
}
