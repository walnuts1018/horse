CREATE TABLE account_movements (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    journal_id uuid NOT NULL UNIQUE,
    kind text NOT NULL CHECK (kind IN ('TRANSFER', 'CARD_BILL_PAYMENT')),
    occurred_at timestamptz NOT NULL,
    from_account_id uuid NOT NULL,
    to_account_id uuid NOT NULL,
    amount_yen bigint NOT NULL CHECK (amount_yen BETWEEN 1 AND 1000000000000),
    description text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, journal_id) REFERENCES journals(household_id, id),
    FOREIGN KEY (household_id, from_account_id) REFERENCES accounts(household_id, id),
    FOREIGN KEY (household_id, to_account_id) REFERENCES accounts(household_id, id),
    CHECK (from_account_id <> to_account_id)
);

CREATE INDEX account_movements_household_occurred_idx ON account_movements(household_id, occurred_at DESC, id DESC);

CREATE TABLE account_incomes (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    journal_id uuid NOT NULL UNIQUE,
    account_id uuid NOT NULL,
    occurred_at timestamptz NOT NULL,
    amount_yen bigint NOT NULL CHECK (amount_yen BETWEEN 1 AND 1000000000000),
    description text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, journal_id) REFERENCES journals(household_id, id),
    FOREIGN KEY (household_id, account_id) REFERENCES accounts(household_id, id)
);

CREATE INDEX account_incomes_household_occurred_idx ON account_incomes(household_id, occurred_at DESC, id DESC);
