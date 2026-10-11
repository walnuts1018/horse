CREATE TABLE members (
    id uuid PRIMARY KEY,
    oidc_subject text NOT NULL UNIQUE,
    display_name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE households (
    id uuid PRIMARY KEY,
    name text NOT NULL,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    default_allocation_version bigint NOT NULL DEFAULT 1,
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
    FOREIGN KEY (household_id, pool_id) REFERENCES pools(household_id, id),
    UNIQUE (household_id, name)
);

CREATE TABLE categories (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    parent_id uuid,
    name text NOT NULL,
    archived boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, parent_id) REFERENCES categories(household_id, id),
    UNIQUE (household_id, parent_id, name)
);

CREATE TABLE allocation_defaults (
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    version bigint NOT NULL,
    pool_id uuid NOT NULL,
    weight bigint NOT NULL CHECK (weight > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (household_id, version, pool_id),
    FOREIGN KEY (household_id, pool_id) REFERENCES pools(household_id, id)
);

CREATE TABLE purchases (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    occurred_at timestamptz NOT NULL,
    merchant text NOT NULL DEFAULT '',
    payment_account_id uuid,
    status text NOT NULL DEFAULT 'DRAFT' CHECK (status IN ('DRAFT', 'POSTED', 'CLOSED')),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    field_revisions jsonb NOT NULL DEFAULT '{}'::jsonb,
    allocation_rule_version bigint NOT NULL,
    created_by uuid NOT NULL REFERENCES members(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, payment_account_id) REFERENCES accounts(household_id, id)
);

CREATE INDEX purchases_household_occurred_idx ON purchases(household_id, occurred_at DESC, id DESC);

CREATE TABLE purchase_items (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL,
    purchase_id uuid NOT NULL,
    name text NOT NULL,
    amount_yen bigint NOT NULL CHECK (amount_yen >= 0),
    category_id uuid,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    field_revisions jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (purchase_id, id),
    UNIQUE (household_id, purchase_id, id),
    FOREIGN KEY (household_id, purchase_id) REFERENCES purchases(household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, category_id) REFERENCES categories(household_id, id)
);

CREATE TABLE item_allocations (
    household_id uuid NOT NULL,
    purchase_id uuid NOT NULL,
    item_id uuid NOT NULL,
    pool_id uuid NOT NULL,
    weight bigint NOT NULL CHECK (weight > 0),
    amount_yen bigint NOT NULL CHECK (amount_yen >= 0),
    rule_version bigint NOT NULL,
    PRIMARY KEY (item_id, pool_id),
    FOREIGN KEY (household_id, purchase_id, item_id) REFERENCES purchase_items(household_id, purchase_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, pool_id) REFERENCES pools(household_id, id)
);

CREATE TABLE journals (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    source_kind text NOT NULL,
    source_id uuid NOT NULL,
    idempotency_key text NOT NULL,
    occurred_at timestamptz NOT NULL,
    created_by uuid NOT NULL REFERENCES members(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, idempotency_key),
    UNIQUE (household_id, source_kind, source_id)
);

CREATE TABLE journal_entries (
    journal_id uuid NOT NULL REFERENCES journals(id) ON DELETE CASCADE,
    line_no integer NOT NULL,
    account_id uuid,
    pool_id uuid,
    member_id uuid,
    debit_yen bigint NOT NULL DEFAULT 0 CHECK (debit_yen >= 0),
    credit_yen bigint NOT NULL DEFAULT 0 CHECK (credit_yen >= 0),
    memo text NOT NULL DEFAULT '',
    PRIMARY KEY (journal_id, line_no),
    CHECK ((debit_yen = 0) <> (credit_yen = 0))
);

CREATE TABLE ledger_entries (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    journal_id uuid NOT NULL REFERENCES journals(id),
    kind text NOT NULL CHECK (kind IN ('PURCHASE', 'FUNDING', 'TRANSFER', 'CORRECTION')),
    occurred_at timestamptz NOT NULL,
    description text NOT NULL,
    amount_yen bigint NOT NULL CHECK (amount_yen >= 0),
    from_member_id uuid REFERENCES members(id),
    to_member_id uuid REFERENCES members(id),
    pool_id uuid,
    source_purchase_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (household_id, pool_id) REFERENCES pools(household_id, id),
    FOREIGN KEY (household_id, source_purchase_id) REFERENCES purchases(household_id, id),
    UNIQUE (journal_id)
);

CREATE INDEX ledger_household_occurred_idx ON ledger_entries(household_id, occurred_at DESC, id DESC);

CREATE TABLE transfers (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    ledger_entry_id uuid NOT NULL UNIQUE REFERENCES ledger_entries(id),
    from_member_id uuid NOT NULL REFERENCES members(id),
    to_member_id uuid NOT NULL REFERENCES members(id),
    kind text NOT NULL CHECK (kind IN ('ADVANCE', 'LOAN', 'REPAYMENT', 'GIFT')),
    amount_yen bigint NOT NULL CHECK (amount_yen > 0),
    description text NOT NULL DEFAULT '',
    loan_id uuid REFERENCES transfers(id),
    CHECK (from_member_id <> to_member_id),
    CHECK ((kind = 'REPAYMENT') = (loan_id IS NOT NULL))
);

CREATE TABLE funding_schedules (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    member_id uuid NOT NULL REFERENCES members(id),
    pool_id uuid NOT NULL,
    amount_yen bigint NOT NULL CHECK (amount_yen >= 0),
    cadence text NOT NULL CHECK (cadence IN ('MONTHLY', 'WEEKLY', 'CUSTOM')),
    starts_on date NOT NULL,
    ends_on date,
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (household_id, pool_id) REFERENCES pools(household_id, id),
    CHECK (ends_on IS NULL OR ends_on >= starts_on)
);

CREATE TABLE funding_obligations (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    schedule_id uuid REFERENCES funding_schedules(id),
    member_id uuid NOT NULL REFERENCES members(id),
    pool_id uuid NOT NULL,
    period text NOT NULL,
    amount_yen bigint NOT NULL CHECK (amount_yen >= 0),
    paid_yen bigint NOT NULL DEFAULT 0 CHECK (paid_yen >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (household_id, pool_id) REFERENCES pools(household_id, id),
    UNIQUE (household_id, member_id, pool_id, period)
);

CREATE TABLE accounting_periods (
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    period text NOT NULL,
    status text NOT NULL CHECK (status IN ('OPEN', 'CLOSED')),
    revision bigint NOT NULL DEFAULT 1,
    closed_by uuid REFERENCES members(id),
    closed_at timestamptz,
    PRIMARY KEY (household_id, period)
);

CREATE TABLE settlement_runs (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    period text NOT NULL,
    revision bigint NOT NULL,
    status text NOT NULL CHECK (status IN ('OPEN', 'CLOSED')),
    created_by uuid NOT NULL REFERENCES members(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, period, revision)
);

CREATE TABLE settlement_lines (
    settlement_run_id uuid NOT NULL REFERENCES settlement_runs(id) ON DELETE CASCADE,
    member_id uuid NOT NULL REFERENCES members(id),
    scheduled_yen bigint NOT NULL,
    paid_yen bigint NOT NULL,
    carry_yen bigint NOT NULL,
    advance_credit_yen bigint NOT NULL,
    amount_due_yen bigint NOT NULL,
    PRIMARY KEY (settlement_run_id, member_id)
);

CREATE TABLE corrections (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    purchase_id uuid NOT NULL,
    item_id uuid,
    amount_delta_yen bigint NOT NULL,
    reason text NOT NULL,
    journal_id uuid NOT NULL REFERENCES journals(id),
    created_by uuid NOT NULL REFERENCES members(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (household_id, purchase_id) REFERENCES purchases(household_id, id),
    FOREIGN KEY (purchase_id, item_id) REFERENCES purchase_items(purchase_id, id)
);

CREATE TABLE household_events (
    cursor bigserial PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    type text NOT NULL,
    entity_id uuid NOT NULL,
    revision bigint NOT NULL,
    changed_fields jsonb NOT NULL DEFAULT '[]'::jsonb,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    actor_id uuid REFERENCES members(id),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX household_events_cursor_idx ON household_events(household_id, cursor);

CREATE TABLE idempotency_keys (
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    key text NOT NULL,
    operation text NOT NULL,
    resource_id uuid,
    response jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (household_id, key)
);

CREATE TABLE oidc_sessions (
    token_hash bytea PRIMARY KEY,
    member_id uuid NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    expiry timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE oidc_states (
    state_hash bytea PRIMARY KEY,
    nonce text NOT NULL,
    verifier text NOT NULL,
    redirect_to text NOT NULL DEFAULT '/',
    expires_at timestamptz NOT NULL
);

CREATE TABLE source_documents (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    sha256 text NOT NULL,
    object_key text NOT NULL,
    mime_type text NOT NULL,
    source_kind text NOT NULL CHECK (source_kind IN ('receipt', 'order', 'email', 'card', 'bank')),
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_by uuid NOT NULL REFERENCES members(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, sha256),
    UNIQUE (object_key)
);

CREATE TABLE extraction_jobs (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    source_document_id uuid NOT NULL REFERENCES source_documents(id) ON DELETE CASCADE,
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    status text NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    request jsonb NOT NULL DEFAULT '{}'::jsonb,
    result jsonb,
    error_code text,
    attempt integer NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    available_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source_document_id, version)
);

CREATE INDEX extraction_jobs_ready_idx ON extraction_jobs(available_at) WHERE status = 'queued';

CREATE TABLE evidence_links (
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    source_document_id uuid NOT NULL REFERENCES source_documents(id) ON DELETE CASCADE,
    purchase_id uuid NOT NULL,
    item_id uuid,
    evidence_region jsonb,
    observation jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (source_document_id, purchase_id, item_id),
    FOREIGN KEY (household_id, purchase_id) REFERENCES purchases(household_id, id),
    FOREIGN KEY (purchase_id, item_id) REFERENCES purchase_items(purchase_id, id)
);

CREATE FUNCTION ensure_journal_balanced() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    target_id uuid;
    debit_total bigint;
    credit_total bigint;
BEGIN
    target_id := COALESCE(NEW.journal_id, OLD.journal_id);
    SELECT COALESCE(sum(debit_yen), 0), COALESCE(sum(credit_yen), 0)
      INTO debit_total, credit_total
      FROM journal_entries WHERE journal_id = target_id;
    IF debit_total <> credit_total THEN
        RAISE EXCEPTION 'journal % is not balanced', target_id USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER journal_entries_balanced
AFTER INSERT OR UPDATE OR DELETE ON journal_entries
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION ensure_journal_balanced();
