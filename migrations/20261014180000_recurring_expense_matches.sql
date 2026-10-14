CREATE TABLE recurring_expense_matches (
    household_id uuid NOT NULL,
    recurring_expense_id uuid NOT NULL,
    due_on date NOT NULL,
    purchase_id uuid NOT NULL,
    matched_by uuid NOT NULL REFERENCES members(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (household_id, recurring_expense_id, due_on),
    UNIQUE (household_id, purchase_id),
    FOREIGN KEY (household_id, recurring_expense_id) REFERENCES recurring_expenses(household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, purchase_id) REFERENCES purchases(household_id, id) ON DELETE CASCADE
);

CREATE INDEX recurring_expense_matches_purchase_idx ON recurring_expense_matches(household_id, purchase_id);
