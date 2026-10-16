pub use catalog_domain::product_learning::{
    LearningScope, MerchantKey, NormalizedVariantName, ProductLearningCommand,
    ProductLearningDecision, ProductLearningError, ProductLearningKind, VariantName,
    decide_product_learning,
};
use horse_household_change::UiChangeDescriptor;

pub fn product_variant_renamed() -> UiChangeDescriptor {
    UiChangeDescriptor::new("product.variant.renamed", "product_variant", ["name"])
}

pub fn product_alias_learned() -> UiChangeDescriptor {
    UiChangeDescriptor::new(
        "product.alias.learned",
        "product_alias",
        ["alias", "variantId", "merchant"],
    )
}
