use std::{future::Future, pin::Pin};
use uuid::Uuid;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProductCursor {
    pub sort_key: String,
    pub id: Uuid,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProductCatalogInput {
    pub after: Option<ProductCursor>,
    pub before: Option<ProductCursor>,
    pub first: Option<i32>,
    pub last: Option<i32>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProductAliasProjection {
    pub id: Uuid,
    pub household_id: Uuid,
    pub variant_id: Uuid,
    pub merchant_key: String,
    pub alias: String,
    pub normalized_alias: String,
    pub revision: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProductVariantProjection {
    pub id: Uuid,
    pub household_id: Uuid,
    pub concept_id: Uuid,
    pub name: String,
    pub normalized_name: String,
    pub brand: String,
    pub package_size: String,
    pub barcode: Option<String>,
    pub archived: bool,
    pub revision: i64,
    pub aliases: Vec<ProductAliasProjection>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProductConceptProjection {
    pub id: Uuid,
    pub household_id: Uuid,
    pub name: String,
    pub normalized_name: String,
    pub category_id: Option<Uuid>,
    pub archived: bool,
    pub revision: i64,
    pub variants: Vec<ProductVariantProjection>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProductCatalogPage {
    pub rows: Vec<ProductConceptProjection>,
    pub has_next_page: bool,
    pub has_previous_page: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProductSuggestionsInput {
    pub query: String,
    pub merchant: Option<String>,
    pub first: i32,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProductSuggestionProjection {
    pub concept: ProductConceptProjection,
    pub variant: ProductVariantProjection,
    pub matched_name: String,
    pub score: i32,
}

#[derive(Debug)]
pub enum ProductQueryError {
    HouseholdNotFound,
    InvalidPagination,
    InvalidCursor,
    Infrastructure(String),
}

pub trait ProductQuery: Send + Sync {
    fn catalog<'a>(
        &'a self,
        actor_id: Uuid,
        household_id: Uuid,
        input: ProductCatalogInput,
    ) -> Pin<Box<dyn Future<Output = Result<ProductCatalogPage, ProductQueryError>> + Send + 'a>>;

    fn suggestions<'a>(
        &'a self,
        actor_id: Uuid,
        household_id: Uuid,
        input: ProductSuggestionsInput,
    ) -> Pin<
        Box<
            dyn Future<Output = Result<Vec<ProductSuggestionProjection>, ProductQueryError>>
                + Send
                + 'a,
        >,
    >;
}
