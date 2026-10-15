ALTER TABLE journals
    ADD COLUMN idempotency_key text;

UPDATE journals
SET idempotency_key = id::text
WHERE idempotency_key IS NULL;

ALTER TABLE journals
    ALTER COLUMN idempotency_key SET NOT NULL,
    ADD CONSTRAINT journals_idempotency_key_length CHECK (length(idempotency_key) BETWEEN 1 AND 200),
    ADD CONSTRAINT journals_household_idempotency_key_key UNIQUE (household_id, idempotency_key);

ALTER TABLE ledger_entries
    ADD COLUMN from_member_id uuid,
    ADD COLUMN pool_id uuid,
    ADD CONSTRAINT ledger_entries_from_member_fk FOREIGN KEY (household_id, from_member_id) REFERENCES household_members(household_id, member_id),
    ADD CONSTRAINT ledger_entries_pool_fk FOREIGN KEY (household_id, pool_id) REFERENCES pools(household_id, id);

CREATE TABLE recurring_expense_matches (
    household_id uuid NOT NULL,
    recurring_expense_id uuid NOT NULL,
    due_on date NOT NULL,
    purchase_id uuid NOT NULL,
    matched_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (household_id, recurring_expense_id, due_on),
    UNIQUE (household_id, purchase_id),
    FOREIGN KEY (household_id, recurring_expense_id) REFERENCES recurring_expenses(household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, purchase_id) REFERENCES purchases(household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, matched_by) REFERENCES household_members(household_id, member_id)
);

CREATE INDEX recurring_expense_matches_purchase_idx ON recurring_expense_matches(household_id, purchase_id);
