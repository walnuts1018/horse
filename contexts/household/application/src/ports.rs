use crate::create_household::{CreateHouseholdCommand, CreateHouseholdError};

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

pub trait HouseholdStore {
    // RPITIT。async-traitクレートは使わない。具体的なFutureはSendであることが呼び出し側で保証される。
    #[allow(async_fn_in_trait)]
    async fn create_household(
        &self,
        command: CreateHouseholdCommand,
    ) -> Result<CreatedHousehold, CreateHouseholdPortError>;
}
