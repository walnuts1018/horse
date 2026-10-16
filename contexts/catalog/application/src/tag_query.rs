use std::{future::Future, pin::Pin};
use uuid::Uuid;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TagCursor {
    pub sort_key: String,
    pub id: Uuid,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TagQueryInput {
    pub include_archived: bool,
    pub after: Option<TagCursor>,
    pub before: Option<TagCursor>,
    pub first: Option<i32>,
    pub last: Option<i32>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TagProjection {
    pub id: Uuid,
    pub household_id: Uuid,
    pub name: String,
    pub archived: bool,
    pub revision: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TagPage {
    pub rows: Vec<TagProjection>,
    pub total_count: i64,
    pub has_next_page: bool,
    pub has_previous_page: bool,
}

#[derive(Debug)]
pub enum TagQueryError {
    HouseholdNotFound,
    InvalidPagination,
    InvalidCursor,
    Infrastructure(String),
}

pub trait TagQuery: Send + Sync {
    fn query<'a>(
        &'a self,
        actor_id: Uuid,
        household_id: Uuid,
        input: TagQueryInput,
    ) -> Pin<Box<dyn Future<Output = Result<TagPage, TagQueryError>> + Send + 'a>>;
}
