use catalog_application::product_query::{
    ProductCatalogInput, ProductCatalogPage, ProductCursor, ProductQuery, ProductQueryError,
    ProductSuggestionProjection, ProductSuggestionsInput,
};

use crate::{PgPool, catalog_query};

pub struct PostgresProductQuery {
    pool: PgPool,
}

impl PostgresProductQuery {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl ProductQuery for PostgresProductQuery {
    fn catalog<'a>(
        &'a self,
        actor_id: uuid::Uuid,
        household_id: uuid::Uuid,
        input: ProductCatalogInput,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<Output = Result<ProductCatalogPage, ProductQueryError>>
                + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            catalog_query::product_catalog(
                &self.pool,
                actor_id,
                household_id,
                input.after.map(catalog_cursor),
                input.before.map(catalog_cursor),
                input.first,
                input.last,
            )
            .await
            .map_err(map_error)
        })
    }

    fn suggestions<'a>(
        &'a self,
        actor_id: uuid::Uuid,
        household_id: uuid::Uuid,
        input: ProductSuggestionsInput,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<
                    Output = Result<Vec<ProductSuggestionProjection>, ProductQueryError>,
                > + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            catalog_query::product_suggestions(
                &self.pool,
                actor_id,
                household_id,
                &input.query,
                input.merchant.as_deref(),
                input.first,
            )
            .await
            .map_err(map_error)
        })
    }
}

fn catalog_cursor(cursor: ProductCursor) -> catalog_query::CatalogCursor {
    catalog_query::CatalogCursor {
        sort_key: cursor.sort_key,
        id: cursor.id,
    }
}

fn map_error(error: catalog_query::CatalogQueryError) -> ProductQueryError {
    use catalog_query::CatalogQueryError as E;
    match error {
        E::HouseholdNotFound => ProductQueryError::HouseholdNotFound,
        E::InvalidPagination => ProductQueryError::InvalidPagination,
        E::InvalidCursor => ProductQueryError::InvalidCursor,
        E::Pool(error) => ProductQueryError::Infrastructure(error),
        E::Database(error) => ProductQueryError::Infrastructure(format!("{error:?}")),
        E::InvalidPersistedData => {
            ProductQueryError::Infrastructure("invalid persisted data".into())
        }
    }
}
