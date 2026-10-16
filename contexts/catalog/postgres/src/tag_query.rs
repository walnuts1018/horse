use catalog_application::tag_query::{
    TagPage, TagProjection, TagQuery, TagQueryError, TagQueryInput,
};

use crate::{PgPool, catalog_query};

pub struct PostgresTagQuery {
    pool: PgPool,
}

impl PostgresTagQuery {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl TagQuery for PostgresTagQuery {
    fn query<'a>(
        &'a self,
        actor_id: uuid::Uuid,
        household_id: uuid::Uuid,
        input: TagQueryInput,
    ) -> std::pin::Pin<
        Box<dyn std::future::Future<Output = Result<TagPage, TagQueryError>> + Send + 'a>,
    > {
        Box::pin(async move {
            let page = catalog_query::tags(
                &self.pool,
                actor_id,
                household_id,
                catalog_query::TagQueryArgs {
                    include_archived: input.include_archived,
                    after: input.after.map(|cursor| catalog_query::CatalogCursor {
                        sort_key: cursor.sort_key,
                        id: cursor.id,
                    }),
                    before: input.before.map(|cursor| catalog_query::CatalogCursor {
                        sort_key: cursor.sort_key,
                        id: cursor.id,
                    }),
                    first: input.first,
                    last: input.last,
                },
            )
            .await
            .map_err(map_error)?;

            Ok(TagPage {
                rows: page
                    .rows
                    .into_iter()
                    .map(|row| TagProjection {
                        id: row.id,
                        household_id: row.household_id,
                        name: row.name,
                        archived: row.archived,
                        revision: row.revision,
                    })
                    .collect(),
                total_count: page.total_count,
                has_next_page: page.has_next_page,
                has_previous_page: page.has_previous_page,
            })
        })
    }
}

fn map_error(error: catalog_query::CatalogQueryError) -> TagQueryError {
    use catalog_query::CatalogQueryError as E;
    match error {
        E::HouseholdNotFound => TagQueryError::HouseholdNotFound,
        E::InvalidPagination => TagQueryError::InvalidPagination,
        E::InvalidCursor => TagQueryError::InvalidCursor,
        E::Pool(error) => TagQueryError::Infrastructure(error),
        E::Database(error) => TagQueryError::Infrastructure(format!("{error:?}")),
        E::InvalidPersistedData => TagQueryError::Infrastructure("invalid persisted data".into()),
    }
}
