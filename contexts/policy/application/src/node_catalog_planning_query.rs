use std::{future::Future, pin::Pin};

use chrono::NaiveDate;
use uuid::Uuid;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct PolicyNodeBatchInput {
    pub household_ids: Vec<Uuid>,
    pub recurring_expense_ids: Vec<Uuid>,
    pub allocation_rule_ids: Vec<Uuid>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct RecurringExpenseProjection {
    pub id: Uuid,
    pub household_id: Uuid,
    pub name: String,
    pub merchant: String,
    pub amount_yen: i64,
    pub currency: String,
    pub category_id: Option<Uuid>,
    pub payment_account_id: Option<Uuid>,
    pub cadence: String,
    pub starts_on: NaiveDate,
    pub next_due_on: NaiveDate,
    pub ends_on: Option<NaiveDate>,
    pub archived: bool,
    pub revision: i64,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct AllocationRuleProjection {
    pub id: Uuid,
    pub household_id: Uuid,
    pub category_id: Option<Uuid>,
    pub category_name: Option<String>,
    pub category_parent_id: Option<Uuid>,
    pub category_archived: Option<bool>,
    pub merchant_key: Option<String>,
    pub revision: i64,
    pub product_concept_id: Option<Uuid>,
    pub product_variant_id: Option<Uuid>,
    pub allocations: Vec<AllocationRuleWeightProjection>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct AllocationRuleWeightProjection {
    pub pool_id: Uuid,
    pub pool_name: String,
    pub weight: i64,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct PolicyNodeBatchProjection {
    pub recurring_expenses: Vec<RecurringExpenseProjection>,
    pub allocation_rules: Vec<AllocationRuleProjection>,
}

pub trait PolicyNodeBatchQuery: Send + Sync {
    fn load<'a>(
        &'a self,
        input: PolicyNodeBatchInput,
    ) -> Pin<Box<dyn Future<Output = Result<PolicyNodeBatchProjection, String>> + Send + 'a>>;
}
