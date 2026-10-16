use uuid::Uuid;

macro_rules! learning_id {
    ($name:ident) => {
        #[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
        pub struct $name(Uuid);

        impl From<Uuid> for $name {
            fn from(value: Uuid) -> Self {
                Self(value)
            }
        }

        impl $name {
            pub fn value(self) -> Uuid {
                self.0
            }
        }
    };
}

learning_id!(LearningHouseholdId);
learning_id!(LearningActorId);
learning_id!(LearningPurchaseId);
learning_id!(LearningItemId);
learning_id!(LearningVariantId);

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct VariantName(String);

impl VariantName {
    fn new(value: String) -> Self {
        Self(value)
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct NormalizedVariantName(String);

impl NormalizedVariantName {
    fn new(value: String) -> Self {
        Self(value)
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MerchantKey(String);

impl MerchantKey {
    fn new(value: String) -> Self {
        Self(value)
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum LearningScope {
    VariantName,
    MerchantAlias,
    ItemOnly,
}

impl LearningScope {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::VariantName => "VARIANT_NAME",
            Self::MerchantAlias => "MERCHANT_ALIAS",
            Self::ItemOnly => "ITEM_ONLY",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ProductLearningKind {
    VariantName(String),
    MerchantAlias,
    ItemOnly,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProductLearningCommand {
    pub household_id: LearningHouseholdId,
    pub actor_id: LearningActorId,
    pub purchase_id: LearningPurchaseId,
    pub item_id: LearningItemId,
    pub raw_name: String,
    pub previous_variant_id: Option<LearningVariantId>,
    pub variant_id: Option<LearningVariantId>,
    pub variant_changed: bool,
    pub merchant: String,
    pub learning: Option<ProductLearningKind>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProductLearningDecision {
    pub household_id: LearningHouseholdId,
    pub actor_id: LearningActorId,
    pub purchase_id: LearningPurchaseId,
    pub item_id: LearningItemId,
    pub raw_name: String,
    pub previous_variant_id: Option<LearningVariantId>,
    pub variant_id: LearningVariantId,
    pub merchant_key: MerchantKey,
    pub learning_scope: LearningScope,
    pub record_correction: bool,
    pub variant_name: Option<(VariantName, NormalizedVariantName)>,
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
        Some(ProductLearningKind::VariantName(_)) => LearningScope::VariantName,
        Some(ProductLearningKind::MerchantAlias) => LearningScope::MerchantAlias,
        Some(ProductLearningKind::ItemOnly) | None => LearningScope::ItemOnly,
    };
    let variant_name = match &command.learning {
        Some(ProductLearningKind::VariantName(name)) => {
            let name = name.trim();
            let normalized_name = normalize(name);
            if normalized_name.is_empty() || name.len() > 240 {
                return Err(ProductLearningError::InvalidVariantName);
            }
            Some((
                VariantName::new(name.to_owned()),
                NormalizedVariantName::new(normalized_name),
            ))
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
        merchant_key: MerchantKey::new(merchant_key),
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
