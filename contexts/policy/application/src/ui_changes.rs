use horse_household_change::UiChangeDescriptor;

pub fn pool_created() -> UiChangeDescriptor {
    UiChangeDescriptor::new("pool.created", "pool", ["name", "kind"])
}

pub fn pool_funding_rule_updated() -> UiChangeDescriptor {
    UiChangeDescriptor::new(
        "pool.funding_rule.updated",
        "pool",
        ["ownerMemberId", "fundingShares", "settlementPolicy"],
    )
}
