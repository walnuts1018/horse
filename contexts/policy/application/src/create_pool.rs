pub use policy_domain::create_pool::*;
use std::{future::Future, pin::Pin};

pub trait CreatePoolHandler: Send + Sync {
    fn create<'a>(
        &'a self,
        command: CreatePoolCommand,
    ) -> Pin<Box<dyn Future<Output = Result<uuid::Uuid, super::WorkflowFailure>> + Send + 'a>>;
}
