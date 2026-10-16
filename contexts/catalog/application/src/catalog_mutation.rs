use std::{future::Future, pin::Pin};

pub use catalog_domain::{
    product_alias::{SetProductAliasCommand, SetProductAliasDecision},
    product_creation::{
        CreateConceptCommand, CreateConceptDecision, CreateVariantCommand, CreateVariantDecision,
    },
    tag::{CreateTagCommand, SetTagArchivedCommand, Tag, UpdateTagCommand},
};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreatedConcept {
    pub id: String,
    pub decision: CreateConceptDecision,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreatedVariant {
    pub id: String,
    pub decision: CreateVariantDecision,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetProductAliasResult {
    pub id: String,
    pub revision: u64,
    pub decision: SetProductAliasDecision,
}

#[derive(Debug)]
pub enum CatalogMutationError {
    BadInput,
    NotFound,
    Conflict,
    Infrastructure(String),
}

pub trait CatalogMutationHandler: Send + Sync {
    fn create_tag<'a>(
        &'a self,
        command: CreateTagCommand,
    ) -> Pin<Box<dyn Future<Output = Result<Tag, CatalogMutationError>> + Send + 'a>>;

    fn update_tag<'a>(
        &'a self,
        command: UpdateTagCommand,
    ) -> Pin<Box<dyn Future<Output = Result<Tag, CatalogMutationError>> + Send + 'a>>;

    fn set_tag_archived<'a>(
        &'a self,
        command: SetTagArchivedCommand,
    ) -> Pin<Box<dyn Future<Output = Result<Tag, CatalogMutationError>> + Send + 'a>>;

    fn create_concept<'a>(
        &'a self,
        command: CreateConceptCommand,
    ) -> Pin<Box<dyn Future<Output = Result<CreatedConcept, CatalogMutationError>> + Send + 'a>>;

    fn create_variant<'a>(
        &'a self,
        command: CreateVariantCommand,
    ) -> Pin<Box<dyn Future<Output = Result<CreatedVariant, CatalogMutationError>> + Send + 'a>>;

    fn set_product_alias<'a>(
        &'a self,
        command: SetProductAliasCommand,
    ) -> Pin<
        Box<dyn Future<Output = Result<SetProductAliasResult, CatalogMutationError>> + Send + 'a>,
    >;
}
