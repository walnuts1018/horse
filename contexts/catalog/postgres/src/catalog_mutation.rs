use catalog_application::catalog_mutation::{
    CatalogMutationError, CatalogMutationHandler, CreatedConcept, CreatedVariant,
    SetProductAliasResult,
};
use catalog_application::workflow_failure::WorkflowFailure;
use catalog_domain::{
    product_alias::SetProductAliasCommand,
    product_creation::{CreateConceptCommand, CreateVariantCommand},
    tag::{CreateTagCommand, SetTagArchivedCommand, UpdateTagCommand},
};
use catalog_domain::{product_alias::decide_set_product_alias, tag::Tag};

use crate::{PgPool, create_product, product_alias, tag};

pub struct PostgresCatalogMutation {
    pool: PgPool,
}

impl PostgresCatalogMutation {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl CatalogMutationHandler for PostgresCatalogMutation {
    fn create_tag<'a>(
        &'a self,
        command: CreateTagCommand,
    ) -> std::pin::Pin<
        Box<dyn std::future::Future<Output = Result<Tag, CatalogMutationError>> + Send + 'a>,
    > {
        Box::pin(async move {
            tag::persist_create_tag(&self.pool, command)
                .await
                .map_err(map_workflow_failure)
        })
    }

    fn update_tag<'a>(
        &'a self,
        command: UpdateTagCommand,
    ) -> std::pin::Pin<
        Box<dyn std::future::Future<Output = Result<Tag, CatalogMutationError>> + Send + 'a>,
    > {
        Box::pin(async move {
            tag::persist_update_tag(&self.pool, command)
                .await
                .map_err(map_workflow_failure)
        })
    }

    fn set_tag_archived<'a>(
        &'a self,
        command: SetTagArchivedCommand,
    ) -> std::pin::Pin<
        Box<dyn std::future::Future<Output = Result<Tag, CatalogMutationError>> + Send + 'a>,
    > {
        Box::pin(async move {
            tag::persist_set_tag_archived(&self.pool, command)
                .await
                .map_err(map_workflow_failure)
        })
    }

    fn create_concept<'a>(
        &'a self,
        command: CreateConceptCommand,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<Output = Result<CreatedConcept, CatalogMutationError>>
                + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            let (id, decision) = create_product::create_concept(&self.pool, command)
                .await
                .map_err(map_workflow_failure)?;
            Ok(CreatedConcept { id, decision })
        })
    }

    fn create_variant<'a>(
        &'a self,
        command: CreateVariantCommand,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<Output = Result<CreatedVariant, CatalogMutationError>>
                + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            let (id, decision) = create_product::create_variant(&self.pool, command)
                .await
                .map_err(map_workflow_failure)?;
            Ok(CreatedVariant { id, decision })
        })
    }

    fn set_product_alias<'a>(
        &'a self,
        command: SetProductAliasCommand,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<Output = Result<SetProductAliasResult, CatalogMutationError>>
                + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            let decision = decide_set_product_alias(command.clone())
                .map_err(|_| CatalogMutationError::BadInput)?;
            let persisted = product_alias::persist_set_product_alias(&self.pool, command)
                .await
                .map_err(map_workflow_failure)?;
            Ok(SetProductAliasResult {
                id: persisted.id,
                revision: persisted.revision,
                decision,
            })
        })
    }
}

fn map_workflow_failure<DecisionError: std::fmt::Debug, InfrastructureError: std::fmt::Debug>(
    error: WorkflowFailure<DecisionError, InfrastructureError>,
) -> CatalogMutationError {
    match error {
        WorkflowFailure::Decision(_) | WorkflowFailure::InvalidInput => {
            CatalogMutationError::BadInput
        }
        WorkflowFailure::NotFound => CatalogMutationError::NotFound,
        WorkflowFailure::Conflict => CatalogMutationError::Conflict,
        WorkflowFailure::Infrastructure(error) => {
            CatalogMutationError::Infrastructure(format!("{error:?}"))
        }
    }
}
