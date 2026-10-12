CREATE TABLE account_incomes (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    journal_id uuid NOT NULL UNIQUE REFERENCES journals(id),
    account_id uuid NOT NULL,
    occurred_at timestamptz NOT NULL,
    amount_yen bigint NOT NULL CHECK (amount_yen > 0),
    description text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, account_id) REFERENCES accounts(household_id, id)
);

CREATE INDEX account_incomes_household_occurred_idx ON account_incomes(household_id, occurred_at DESC, id DESC);

ALTER TABLE idempotency_keys ADD COLUMN request_fingerprint bytea;
