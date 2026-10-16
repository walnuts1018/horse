use uuid::Uuid;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct AllocationRuleTargetQuery {
    pub product_variant_id: Option<Uuid>,
    pub product_concept_id: Option<Uuid>,
    pub category_id: Option<Uuid>,
    pub merchant_key: Option<String>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct AllocationRuleWeight {
    pub pool_id: Uuid,
    pub weight: i64,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct PurchaseAllocationRule {
    pub id: Uuid,
    pub revision: i64,
    pub product_variant_id: Option<Uuid>,
    pub product_concept_id: Option<Uuid>,
    pub category_id: Option<Uuid>,
    pub merchant_key: Option<String>,
    pub allocations: Vec<AllocationRuleWeight>,
}

#[derive(Debug)]
pub enum PurchaseAllocationRulesFailure {
    Infrastructure(String),
}

#[allow(async_fn_in_trait)]
pub trait PurchaseAllocationRulesRead<C>: Send + Sync {
    async fn read(
        &self,
        conn: &mut C,
        household_id: Uuid,
        targets: &[AllocationRuleTargetQuery],
    ) -> Result<Vec<PurchaseAllocationRule>, PurchaseAllocationRulesFailure>;
}
