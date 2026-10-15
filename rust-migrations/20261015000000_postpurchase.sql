CREATE TABLE members (
    id uuid PRIMARY KEY,
    oidc_subject text NOT NULL UNIQUE,
    display_name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE households (
    id uuid PRIMARY KEY,
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE household_members (
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    member_id uuid NOT NULL REFERENCES members(id),
    role text NOT NULL DEFAULT 'MEMBER' CHECK (role IN ('OWNER', 'MEMBER')),
    joined_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (household_id, member_id)
);

CREATE TABLE pools (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('PERSONAL', 'SHARED')),
    current_funding_version integer CHECK (current_funding_version IS NULL OR current_funding_version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    UNIQUE (household_id, name)
);

CREATE TABLE accounts (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    pool_id uuid,
    name text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('BANK', 'CASH', 'CARD', 'OTHER')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    UNIQUE (household_id, id, pool_id),
    UNIQUE (household_id, name),
    FOREIGN KEY (household_id, pool_id) REFERENCES pools(household_id, id)
);

CREATE TABLE pool_funding_rules (
    household_id uuid NOT NULL,
    pool_id uuid NOT NULL,
    version integer NOT NULL CHECK (version > 0),
    owner_member_id uuid,
    settlement_policy text CHECK (settlement_policy IN ('BALANCE_TO_STANDARD', 'REIMBURSE_PERSONAL')),
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (household_id, pool_id, version),
    FOREIGN KEY (household_id, pool_id) REFERENCES pools(household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, owner_member_id) REFERENCES household_members(household_id, member_id),
    FOREIGN KEY (household_id, created_by) REFERENCES household_members(household_id, member_id),
    CHECK (owner_member_id IS NULL OR settlement_policy IS NULL)
);

CREATE TABLE pool_funding_shares (
    household_id uuid NOT NULL,
    pool_id uuid NOT NULL,
    version integer NOT NULL,
    member_id uuid NOT NULL,
    weight bigint NOT NULL CHECK (weight > 0),
    PRIMARY KEY (household_id, pool_id, version, member_id),
    FOREIGN KEY (household_id, pool_id, version) REFERENCES pool_funding_rules(household_id, pool_id, version) ON DELETE CASCADE,
    FOREIGN KEY (household_id, member_id) REFERENCES household_members(household_id, member_id)
);

ALTER TABLE pools
    ADD CONSTRAINT pools_current_funding_rule_fk
        FOREIGN KEY (household_id, id, current_funding_version)
        REFERENCES pool_funding_rules(household_id, pool_id, version)
        DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE purchases (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    occurred_at timestamptz NOT NULL,
    merchant text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'DRAFT' CHECK (status IN ('DRAFT', 'POSTED', 'CLOSED')),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, created_by) REFERENCES household_members(household_id, member_id)
);

CREATE INDEX purchases_household_occurred_idx ON purchases(household_id, occurred_at DESC, id DESC);

CREATE TABLE purchase_items (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL,
    purchase_id uuid NOT NULL,
    name text NOT NULL,
    amount_yen bigint NOT NULL CHECK (amount_yen BETWEEN 0 AND 1000000000000),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, purchase_id, id),
    FOREIGN KEY (household_id, purchase_id) REFERENCES purchases(household_id, id) ON DELETE CASCADE
);

CREATE TABLE purchase_allocations (
    household_id uuid NOT NULL,
    purchase_id uuid NOT NULL,
    item_id uuid NOT NULL,
    pool_id uuid NOT NULL,
    amount_yen bigint NOT NULL CHECK (amount_yen BETWEEN 0 AND 1000000000000),
    PRIMARY KEY (item_id, pool_id),
    FOREIGN KEY (household_id, purchase_id, item_id) REFERENCES purchase_items(household_id, purchase_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, pool_id) REFERENCES pools(household_id, id)
);

CREATE TABLE purchase_payments (
    household_id uuid NOT NULL,
    purchase_id uuid NOT NULL,
    line_no integer NOT NULL CHECK (line_no > 0),
    account_id uuid NOT NULL,
    amount_yen bigint NOT NULL CHECK (amount_yen BETWEEN 1 AND 1000000000000),
    PRIMARY KEY (purchase_id, line_no),
    UNIQUE (household_id, purchase_id, line_no),
    FOREIGN KEY (household_id, purchase_id) REFERENCES purchases(household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, account_id) REFERENCES accounts(household_id, id)
);

CREATE TABLE purchase_member_burdens (
    household_id uuid NOT NULL,
    purchase_id uuid NOT NULL,
    item_id uuid NOT NULL,
    pool_id uuid NOT NULL,
    member_id uuid NOT NULL,
    amount_yen bigint NOT NULL CHECK (amount_yen BETWEEN 0 AND 1000000000000),
    funding_rule_version integer NOT NULL CHECK (funding_rule_version > 0),
    funding_weight bigint NOT NULL CHECK (funding_weight > 0),
    settlement_policy text CHECK (settlement_policy IN ('BALANCE_TO_STANDARD', 'REIMBURSE_PERSONAL')),
    PRIMARY KEY (item_id, pool_id, member_id),
    FOREIGN KEY (household_id, purchase_id, item_id) REFERENCES purchase_items(household_id, purchase_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, pool_id, funding_rule_version) REFERENCES pool_funding_rules(household_id, pool_id, version),
    FOREIGN KEY (household_id, member_id) REFERENCES household_members(household_id, member_id)
);

CREATE INDEX purchase_member_burdens_household_purchase_idx ON purchase_member_burdens(household_id, purchase_id);

CREATE TABLE purchase_payment_funding_snapshots (
    household_id uuid NOT NULL,
    purchase_id uuid NOT NULL,
    payment_line_no integer NOT NULL CHECK (payment_line_no > 0),
    account_id uuid NOT NULL,
    pool_id uuid NOT NULL,
    member_id uuid NOT NULL,
    funding_rule_version integer NOT NULL CHECK (funding_rule_version > 0),
    funding_weight bigint NOT NULL CHECK (funding_weight > 0),
    settlement_policy text CHECK (settlement_policy IN ('BALANCE_TO_STANDARD', 'REIMBURSE_PERSONAL')),
    PRIMARY KEY (purchase_id, payment_line_no, member_id),
    FOREIGN KEY (household_id, purchase_id, payment_line_no) REFERENCES purchase_payments(household_id, purchase_id, line_no) ON DELETE CASCADE,
    FOREIGN KEY (household_id, account_id, pool_id) REFERENCES accounts(household_id, id, pool_id),
    FOREIGN KEY (household_id, pool_id, funding_rule_version) REFERENCES pool_funding_rules(household_id, pool_id, version),
    FOREIGN KEY (household_id, member_id) REFERENCES household_members(household_id, member_id)
);

CREATE INDEX purchase_payment_funding_snapshots_household_purchase_idx ON purchase_payment_funding_snapshots(household_id, purchase_id);

CREATE TABLE accounting_periods (
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    period_start date NOT NULL CHECK (EXTRACT(DAY FROM period_start) = 1),
    status text NOT NULL CHECK (status IN ('OPEN', 'CLOSED')),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    closed_by uuid,
    closed_at timestamptz,
    PRIMARY KEY (household_id, period_start),
    FOREIGN KEY (household_id, closed_by) REFERENCES household_members(household_id, member_id),
    CHECK (
        (status = 'CLOSED' AND closed_by IS NOT NULL AND closed_at IS NOT NULL)
        OR (status = 'OPEN' AND closed_by IS NULL AND closed_at IS NULL)
    )
);

CREATE TABLE journals (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    source_kind text NOT NULL CHECK (length(source_kind) BETWEEN 1 AND 64),
    source_id uuid NOT NULL,
    occurred_at timestamptz NOT NULL,
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    UNIQUE (household_id, source_kind, source_id),
    FOREIGN KEY (household_id, created_by) REFERENCES household_members(household_id, member_id)
);

CREATE TABLE journal_entries (
    household_id uuid NOT NULL,
    journal_id uuid NOT NULL,
    line_no integer NOT NULL CHECK (line_no > 0),
    account_id uuid,
    pool_id uuid,
    member_id uuid,
    debit_yen bigint NOT NULL DEFAULT 0 CHECK (debit_yen BETWEEN 0 AND 1000000000000),
    credit_yen bigint NOT NULL DEFAULT 0 CHECK (credit_yen BETWEEN 0 AND 1000000000000),
    memo text NOT NULL DEFAULT '',
    PRIMARY KEY (journal_id, line_no),
    FOREIGN KEY (household_id, journal_id) REFERENCES journals(household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, account_id) REFERENCES accounts(household_id, id),
    FOREIGN KEY (household_id, pool_id) REFERENCES pools(household_id, id),
    FOREIGN KEY (household_id, member_id) REFERENCES household_members(household_id, member_id),
    CHECK ((debit_yen > 0) <> (credit_yen > 0)),
    CHECK (num_nonnulls(account_id, pool_id, member_id) <= 1)
);

CREATE TABLE ledger_entries (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    journal_id uuid NOT NULL UNIQUE,
    source_kind text NOT NULL CHECK (length(source_kind) BETWEEN 1 AND 64),
    source_id uuid NOT NULL,
    occurred_at timestamptz NOT NULL,
    description text NOT NULL,
    amount_yen bigint NOT NULL CHECK (amount_yen BETWEEN 1 AND 1000000000000),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (household_id, journal_id) REFERENCES journals(household_id, id),
    UNIQUE (household_id, source_kind, source_id)
);

CREATE INDEX ledger_entries_household_occurred_idx ON ledger_entries(household_id, occurred_at DESC, id DESC);

CREATE TABLE idempotency_records (
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    operation text NOT NULL CHECK (length(operation) BETWEEN 1 AND 64),
    key text NOT NULL CHECK (length(key) BETWEEN 1 AND 200),
    request_fingerprint bytea NOT NULL CHECK (octet_length(request_fingerprint) = 32),
    resource_kind text NOT NULL CHECK (length(resource_kind) BETWEEN 1 AND 64),
    resource_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (household_id, operation, key)
);

CREATE TABLE household_events (
    cursor bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    event_type text NOT NULL,
    entity_kind text NOT NULL CHECK (length(entity_kind) BETWEEN 1 AND 64),
    entity_id uuid NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    changed_fields text[] NOT NULL DEFAULT '{}',
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    actor_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, entity_kind, entity_id, revision, event_type),
    FOREIGN KEY (household_id, actor_id) REFERENCES household_members(household_id, member_id)
);

CREATE INDEX household_events_household_cursor_idx ON household_events(household_id, cursor);
