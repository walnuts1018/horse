CREATE TABLE tags (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),
    archived boolean NOT NULL DEFAULT false,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    UNIQUE (household_id, name)
);

CREATE TABLE purchase_item_tags (
    household_id uuid NOT NULL,
    purchase_id uuid NOT NULL,
    item_id uuid NOT NULL,
    tag_id uuid NOT NULL,
    PRIMARY KEY (item_id, tag_id),
    FOREIGN KEY (household_id, purchase_id, item_id) REFERENCES purchase_items(household_id, purchase_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, tag_id) REFERENCES tags(household_id, id)
);

CREATE INDEX purchase_item_tags_tag_idx ON purchase_item_tags(household_id, tag_id, item_id);

CREATE TABLE budgets (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    category_id uuid,
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    amount_yen bigint NOT NULL CHECK (amount_yen > 0),
    starts_on date NOT NULL,
    ends_on date,
    archived boolean NOT NULL DEFAULT false,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, category_id) REFERENCES categories(household_id, id),
    CHECK (ends_on IS NULL OR ends_on >= starts_on)
);

CREATE INDEX budgets_household_active_idx ON budgets(household_id, starts_on) WHERE archived = false;

CREATE TABLE recurring_expenses (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 160),
    merchant text NOT NULL DEFAULT '',
    amount_yen bigint NOT NULL CHECK (amount_yen > 0),
    currency text NOT NULL DEFAULT 'JPY' CHECK (currency = 'JPY'),
    category_id uuid,
    payment_account_id uuid,
    cadence text NOT NULL CHECK (cadence IN ('WEEKLY', 'MONTHLY', 'YEARLY')),
    starts_on date NOT NULL,
    next_due_on date NOT NULL,
    ends_on date,
    archived boolean NOT NULL DEFAULT false,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, category_id) REFERENCES categories(household_id, id),
    FOREIGN KEY (household_id, payment_account_id) REFERENCES accounts(household_id, id),
    CHECK (ends_on IS NULL OR ends_on >= starts_on),
    CHECK (next_due_on >= starts_on)
);

CREATE INDEX recurring_expenses_household_active_idx ON recurring_expenses(household_id, next_due_on) WHERE archived = false;
