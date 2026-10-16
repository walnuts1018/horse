use horse_household_change::UiChangeDescriptor;

pub use catalog_domain::product_learning::{
    LearningScope, MerchantKey, NormalizedVariantName, ProductLearningCommand,
    ProductLearningDecision, ProductLearningError, ProductLearningKind, VariantName,
    decide_product_learning,
};

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ProductLearningWorkflowError<E> {
    Decision(ProductLearningError),
    Operations(E),
}

pub trait ProductLearningOperations {
    type Error;

    async fn persist_product_learning(
        &mut self,
        decision: ProductLearningDecision,
    ) -> Result<(), Self::Error>;
}

pub async fn learn_product<O: ProductLearningOperations>(
    operations: &mut O,
    command: ProductLearningCommand,
) -> Result<(), ProductLearningWorkflowError<O::Error>> {
    let Some(decision) =
        decide_product_learning(command).map_err(ProductLearningWorkflowError::Decision)?
    else {
        return Ok(());
    };
    operations
        .persist_product_learning(decision)
        .await
        .map_err(ProductLearningWorkflowError::Operations)
}

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
