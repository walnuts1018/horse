use horse_household_change::UiChangeDescriptor;
use serde_json::json;

pub fn account_created() -> UiChangeDescriptor {
    UiChangeDescriptor::new(
        "account.created",
        "account",
        ["name", "kind", "cardLast4", "poolId"],
    )
}

pub fn account_card_last4_updated() -> UiChangeDescriptor {
    UiChangeDescriptor::new("account.card_last4_updated", "account", ["cardLast4"])
}

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

pub fn purchase_created() -> UiChangeDescriptor {
    UiChangeDescriptor::new("purchase.created", "purchase", std::iter::empty::<String>())
}

pub fn account_income_recorded(
    changed_fields: impl IntoIterator<Item = impl Into<String>>,
) -> UiChangeDescriptor {
    UiChangeDescriptor::new("account.income.recorded", "account_income", changed_fields)
}

pub fn account_movement_recorded(
    changed_fields: impl IntoIterator<Item = impl Into<String>>,
) -> UiChangeDescriptor {
    UiChangeDescriptor::new(
        "account.movement_recorded",
        "account_movement",
        changed_fields,
    )
}

pub fn transfer_recorded(
    changed_fields: impl IntoIterator<Item = impl Into<String>>,
) -> UiChangeDescriptor {
    UiChangeDescriptor::new("transfer.recorded", "ledger", changed_fields)
}

pub fn repayment_recorded(
    changed_fields: impl IntoIterator<Item = impl Into<String>>,
) -> UiChangeDescriptor {
    UiChangeDescriptor::new("transfer.recorded", "ledger", changed_fields)
}

pub fn purchase_posted(
    event_type: &'static str,
    changed_fields: impl IntoIterator<Item = impl Into<String>>,
) -> UiChangeDescriptor {
    UiChangeDescriptor::new(event_type, "purchase", changed_fields)
}

pub fn purchase_corrected(
    event_type: &'static str,
    changed_fields: impl IntoIterator<Item = impl Into<String>>,
) -> UiChangeDescriptor {
    UiChangeDescriptor::new(event_type, "purchase", changed_fields)
}

pub fn purchase_updated(
    changed_fields: impl IntoIterator<Item = impl Into<String>>,
) -> UiChangeDescriptor {
    UiChangeDescriptor::new("purchase.updated", "purchase", changed_fields)
}

pub fn purchase_item_updated(
    changed_fields: impl IntoIterator<Item = impl Into<String>>,
) -> UiChangeDescriptor {
    UiChangeDescriptor::new("purchase.item.updated", "purchase_item", changed_fields)
}

pub fn purchase_item_deleted(purchase_id: String) -> UiChangeDescriptor {
    UiChangeDescriptor::new("purchase.item.deleted", "purchase_item", ["deleted"])
        .with_payload(json!({ "purchaseId": purchase_id }))
}

pub fn product_variant_renamed() -> UiChangeDescriptor {
    UiChangeDescriptor::new("product.variant.renamed", "product_variant", ["name"])
}

pub fn product_alias_learned() -> UiChangeDescriptor {
    UiChangeDescriptor::new(
        "product.alias.learned",
        "product_alias",
        ["alias", "variantId", "merchant"],
    )
}

pub fn period_closed(
    changed_fields: impl IntoIterator<Item = impl Into<String>>,
) -> UiChangeDescriptor {
    UiChangeDescriptor::new("period.closed", "settlement_run", changed_fields)
}

pub fn funding_recorded(
    changed_fields: impl IntoIterator<Item = impl Into<String>>,
) -> UiChangeDescriptor {
    UiChangeDescriptor::new("funding.recorded", "ledger", changed_fields)
}
