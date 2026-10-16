use horse_household_change::UiChangeDescriptor;

pub fn budget_created() -> UiChangeDescriptor {
    UiChangeDescriptor::new(
        "budget.updated",
        "budget",
        [
            "name",
            "amountYen",
            "categoryId",
            "startsOn",
            "endsOn",
            "effectiveFrom",
        ],
    )
}

pub fn budget_updated() -> UiChangeDescriptor {
    UiChangeDescriptor::new(
        "budget.updated",
        "budget",
        ["name", "amountYen", "categoryId", "startsOn", "endsOn"],
    )
}

pub fn budget_archived() -> UiChangeDescriptor {
    UiChangeDescriptor::new("budget.archived", "budget", ["archived"])
}

pub fn recurring_expense_changed(archived: bool) -> UiChangeDescriptor {
    if archived {
        UiChangeDescriptor::new(
            "recurring_expense.archived",
            "recurring_expense",
            ["archived"],
        )
    } else {
        UiChangeDescriptor::new(
            "recurring_expense.updated",
            "recurring_expense",
            [
                "name",
                "merchant",
                "amountYen",
                "categoryId",
                "paymentAccountId",
                "cadence",
                "startsOn",
                "nextDueOn",
                "endsOn",
            ],
        )
    }
}

pub fn recurring_expense_matched() -> UiChangeDescriptor {
    UiChangeDescriptor::new(
        "recurring_expense.matched",
        "recurring_expense",
        ["nextDueOn"],
    )
}

pub fn allocation_rule_updated(fields: &[String]) -> UiChangeDescriptor {
    UiChangeDescriptor::new(
        "allocation.rule.updated",
        "allocation_rule",
        fields.iter().map(String::as_str),
    )
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
