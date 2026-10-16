use catalog_domain::product_creation::{
    CreateConceptCommand, CreateConceptDecision, CreateProductError, CreateVariantCommand,
    CreateVariantDecision, decide_create_concept, decide_create_variant,
};

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CreateProductWorkflowError<E> {
    Decision(CreateProductError),
    Operations(E),
}

pub trait CreateProductOperations {
    type Error;

    async fn persist_concept(
        &mut self,
        decision: CreateConceptDecision,
    ) -> Result<String, Self::Error>;

    async fn persist_variant(
        &mut self,
        decision: CreateVariantDecision,
    ) -> Result<String, Self::Error>;
}

pub async fn create_concept<O: CreateProductOperations>(
    operations: &mut O,
    command: CreateConceptCommand,
) -> Result<(String, CreateConceptDecision), CreateProductWorkflowError<O::Error>> {
    let decision = decide_create_concept(command).map_err(CreateProductWorkflowError::Decision)?;
    let id = operations
        .persist_concept(decision.clone())
        .await
        .map_err(CreateProductWorkflowError::Operations)?;
    Ok((id, decision))
}

pub async fn create_variant<O: CreateProductOperations>(
    operations: &mut O,
    command: CreateVariantCommand,
) -> Result<(String, CreateVariantDecision), CreateProductWorkflowError<O::Error>> {
    let decision = decide_create_variant(command).map_err(CreateProductWorkflowError::Decision)?;
    let id = operations
        .persist_variant(decision.clone())
        .await
        .map_err(CreateProductWorkflowError::Operations)?;
    Ok((id, decision))
}
