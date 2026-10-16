use std::{future::Future, pin::Pin};
use uuid::Uuid;

use crate::{
    product_query::{ProductAliasProjection, ProductConceptProjection, ProductVariantProjection},
    tag_query::TagProjection,
};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CatalogNodeBatchInput {
    pub household_ids: Vec<Uuid>,
    pub tag_ids: Vec<Uuid>,
    pub concept_ids: Vec<Uuid>,
    pub variant_ids: Vec<Uuid>,
    pub concept_variant_ids: Vec<Uuid>,
    pub alias_ids: Vec<Uuid>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CatalogNodeBatch {
    pub tags: Vec<TagProjection>,
    pub concepts: Vec<ProductConceptProjection>,
    pub variants: Vec<ProductVariantProjection>,
    pub aliases: Vec<ProductAliasProjection>,
}

#[derive(Debug)]
pub enum CatalogNodeBatchError {
    Infrastructure(String),
}

pub trait CatalogNodeBatchQuery: Send + Sync {
    fn query<'a>(
        &'a self,
        input: CatalogNodeBatchInput,
    ) -> Pin<Box<dyn Future<Output = Result<CatalogNodeBatch, CatalogNodeBatchError>> + Send + 'a>>;
}
