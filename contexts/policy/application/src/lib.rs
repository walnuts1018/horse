#![allow(async_fn_in_trait)]

pub mod budget;
pub mod create_pool;
pub mod recurring_expense;
pub mod set_allocation_rule;
pub mod set_funding_schedule;
pub mod set_pool_funding_rule;
pub mod ui_changes;

#[derive(Debug)]
pub enum WorkflowFailure {
    Decision,
    NotFound,
    Conflict,
    Infrastructure(String),
}
