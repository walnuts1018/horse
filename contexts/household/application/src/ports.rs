use crate::create_household::{CreateHouseholdDecision, CreateHouseholdError};
use std::{future::Future, pin::Pin};

// §4の依存方向に従い、Application層が永続化ポート(契約)を定義し、postgresクレートがAdapterとして実装する。
// GraphQL等のAdapterはこのtrait経由で操作し、postgresの具体型に直接依存しない。

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreatedHousehold {
    pub household_id: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CreateHouseholdPortError {
    InvalidInput,
    NotFound,
    Infra,
    Workflow(CreateHouseholdError),
}

impl CreateHouseholdPortError {
    pub const fn from_workflow(error: CreateHouseholdError) -> Self {
        Self::Workflow(error)
    }
}

pub trait HouseholdStore: Send + Sync {
    fn persist<'a>(
        &'a self,
        decision: CreateHouseholdDecision,
    ) -> Pin<Box<dyn Future<Output = Result<CreatedHousehold, CreateHouseholdPortError>> + Send + 'a>>;
}
