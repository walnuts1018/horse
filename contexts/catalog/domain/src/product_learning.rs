use uuid::Uuid;

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ProductLearningKind {
    VariantName(String),
    MerchantAlias,
    ItemOnly,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProductLearningCommand {
    pub household_id: Uuid,
    pub actor_id: Uuid,
    pub purchase_id: Uuid,
    pub item_id: Uuid,
    pub raw_name: String,
    pub previous_variant_id: Option<Uuid>,
    pub variant_id: Option<Uuid>,
    pub variant_changed: bool,
    pub merchant: String,
    pub learning: Option<ProductLearningKind>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProductLearningDecision {
    pub household_id: Uuid,
    pub actor_id: Uuid,
    pub purchase_id: Uuid,
    pub item_id: Uuid,
    pub raw_name: String,
    pub previous_variant_id: Option<Uuid>,
    pub variant_id: Uuid,
    pub merchant_key: String,
    pub learning_scope: &'static str,
    pub record_correction: bool,
    pub variant_name: Option<(String, String)>,
    pub alias: Option<(String, String)>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ProductLearningError {
    InvalidVariantName,
    MerchantRequired,
}

pub fn decide_product_learning(
    command: ProductLearningCommand,
) -> Result<Option<ProductLearningDecision>, ProductLearningError> {
    let Some(variant_id) = command.variant_id else {
        return Ok(None);
    };
    let learning_scope = match &command.learning {
        Some(ProductLearningKind::VariantName(_)) => "VARIANT_NAME",
        Some(ProductLearningKind::MerchantAlias) => "MERCHANT_ALIAS",
        Some(ProductLearningKind::ItemOnly) | None => "ITEM_ONLY",
    };
    let variant_name = match &command.learning {
        Some(ProductLearningKind::VariantName(name)) => {
            let name = name.trim();
            let normalized_name = normalize(name);
            if normalized_name.is_empty() || name.len() > 240 {
                return Err(ProductLearningError::InvalidVariantName);
            }
            Some((name.to_owned(), normalized_name))
        }
        _ => None,
    };
    let merchant_key = normalize(&command.merchant);
    let alias = if matches!(command.learning, Some(ProductLearningKind::MerchantAlias)) {
        if merchant_key.is_empty() {
            return Err(ProductLearningError::MerchantRequired);
        }
        Some((command.raw_name.clone(), normalize(&command.raw_name)))
    } else {
        None
    };
    if !command.variant_changed && variant_name.is_none() {
        return Ok(None);
    }
    Ok(Some(ProductLearningDecision {
        household_id: command.household_id,
        actor_id: command.actor_id,
        purchase_id: command.purchase_id,
        item_id: command.item_id,
        raw_name: command.raw_name,
        previous_variant_id: command.previous_variant_id,
        variant_id,
        merchant_key,
        learning_scope,
        record_correction: command.variant_changed,
        variant_name,
        alias,
    }))
}

fn normalize(value: &str) -> String {
    value
        .split_whitespace()
        .collect::<Vec<_>>()
        .join(" ")
        .to_lowercase()
}
