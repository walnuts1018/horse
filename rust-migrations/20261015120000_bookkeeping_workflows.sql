CREATE TABLE purchase_corrections (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL,
    purchase_id uuid NOT NULL,
    item_id uuid,
    amount_delta_yen bigint NOT NULL CHECK (amount_delta_yen BETWEEN -1000000000000 AND 1000000000000 AND amount_delta_yen <> 0),
    reason text NOT NULL CHECK (length(btrim(reason)) > 0),
    effective_period_start date NOT NULL,
    journal_id uuid NOT NULL,
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    UNIQUE (household_id, journal_id),
    FOREIGN KEY (household_id, purchase_id) REFERENCES purchases(household_id, id),
    FOREIGN KEY (household_id, purchase_id, item_id) REFERENCES purchase_items(household_id, purchase_id, id),
    FOREIGN KEY (household_id, effective_period_start) REFERENCES accounting_periods(household_id, period_start),
    FOREIGN KEY (household_id, journal_id) REFERENCES journals(household_id, id),
    FOREIGN KEY (household_id, created_by) REFERENCES household_members(household_id, member_id)
);

CREATE INDEX purchase_corrections_household_purchase_idx ON purchase_corrections(household_id, purchase_id, created_at DESC);

CREATE TABLE purchase_correction_allocations (
    household_id uuid NOT NULL,
    correction_id uuid NOT NULL,
    line_no integer NOT NULL CHECK (line_no > 0),
    pool_id uuid NOT NULL,
    amount_yen bigint NOT NULL CHECK (amount_yen BETWEEN 0 AND 1000000000000),
    PRIMARY KEY (correction_id, line_no),
    UNIQUE (household_id, correction_id, pool_id),
    FOREIGN KEY (household_id, correction_id) REFERENCES purchase_corrections(household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, pool_id) REFERENCES pools(household_id, id)
);

CREATE TABLE purchase_correction_payments (
    household_id uuid NOT NULL,
    correction_id uuid NOT NULL,
    line_no integer NOT NULL CHECK (line_no > 0),
    account_id uuid NOT NULL,
    amount_yen bigint NOT NULL CHECK (amount_yen BETWEEN 1 AND 1000000000000),
    PRIMARY KEY (correction_id, line_no),
    FOREIGN KEY (household_id, correction_id) REFERENCES purchase_corrections(household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, account_id) REFERENCES accounts(household_id, id)
);

CREATE TABLE correction_member_burdens (
    household_id uuid NOT NULL,
    correction_id uuid NOT NULL,
    item_id uuid,
    pool_id uuid NOT NULL,
    member_id uuid NOT NULL,
    amount_delta_yen bigint NOT NULL CHECK (amount_delta_yen BETWEEN -1000000000000 AND 1000000000000 AND amount_delta_yen <> 0),
    funding_rule_version integer NOT NULL CHECK (funding_rule_version > 0),
    funding_weight bigint NOT NULL CHECK (funding_weight > 0),
    PRIMARY KEY (correction_id, pool_id, member_id),
    FOREIGN KEY (household_id, correction_id) REFERENCES purchase_corrections(household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, pool_id, funding_rule_version) REFERENCES pool_funding_rules(household_id, pool_id, version),
    FOREIGN KEY (household_id, member_id) REFERENCES household_members(household_id, member_id)
);

CREATE INDEX correction_member_burdens_household_correction_idx ON correction_member_burdens(household_id, correction_id);

CREATE TABLE transfers (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL,
    journal_id uuid NOT NULL,
    from_member_id uuid NOT NULL,
    to_member_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('ADVANCE', 'LOAN', 'REPAYMENT', 'GIFT')),
    amount_yen bigint NOT NULL CHECK (amount_yen BETWEEN 1 AND 1000000000000),
    description text NOT NULL,
    payment_account_id uuid,
    loan_id uuid,
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    UNIQUE (household_id, journal_id),
    FOREIGN KEY (household_id, journal_id) REFERENCES journals(household_id, id),
    FOREIGN KEY (household_id, from_member_id) REFERENCES household_members(household_id, member_id),
    FOREIGN KEY (household_id, to_member_id) REFERENCES household_members(household_id, member_id),
    FOREIGN KEY (household_id, payment_account_id) REFERENCES accounts(household_id, id),
    FOREIGN KEY (household_id, loan_id) REFERENCES transfers(household_id, id),
    FOREIGN KEY (household_id, created_by) REFERENCES household_members(household_id, member_id),
    CHECK (from_member_id <> to_member_id),
    CHECK ((kind = 'REPAYMENT') = (loan_id IS NOT NULL)),
    CHECK (loan_id IS NULL OR loan_id <> id)
);

CREATE TABLE settlement_runs (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL,
    period_start date NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    status text NOT NULL CHECK (status IN ('OPEN', 'CLOSED')),
    funding_snapshot jsonb NOT NULL,
    actor_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    UNIQUE (household_id, period_start, revision),
    FOREIGN KEY (household_id, period_start) REFERENCES accounting_periods(household_id, period_start),
    FOREIGN KEY (household_id, actor_id) REFERENCES household_members(household_id, member_id)
);

CREATE TABLE settlement_run_lines (
    household_id uuid NOT NULL,
    settlement_run_id uuid NOT NULL,
    member_id uuid NOT NULL,
    member_name text NOT NULL CHECK (length(member_name) > 0),
    scheduled_yen bigint NOT NULL CHECK (scheduled_yen BETWEEN 0 AND 1000000000000),
    paid_yen bigint NOT NULL CHECK (paid_yen BETWEEN 0 AND 1000000000000),
    carry_yen bigint NOT NULL CHECK (carry_yen BETWEEN -1000000000000 AND 1000000000000),
    monthly_adjustment_yen bigint NOT NULL CHECK (monthly_adjustment_yen BETWEEN -1000000000000 AND 1000000000000),
    advance_credit_yen bigint NOT NULL CHECK (advance_credit_yen BETWEEN 0 AND 1000000000000),
    amount_due_yen bigint NOT NULL CHECK (amount_due_yen BETWEEN -3000000000000 AND 3000000000000),
    PRIMARY KEY (settlement_run_id, member_id),
    UNIQUE (household_id, settlement_run_id, member_id),
    FOREIGN KEY (household_id, settlement_run_id) REFERENCES settlement_runs(household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, member_id) REFERENCES household_members(household_id, member_id)
);

CREATE TABLE settlement_run_adjustments (
    household_id uuid NOT NULL,
    settlement_run_id uuid NOT NULL,
    member_id uuid NOT NULL,
    adjustment_kind text NOT NULL CHECK (adjustment_kind IN ('MONTHLY', 'ADVANCE_CREDIT')),
    amount_yen bigint NOT NULL CHECK (amount_yen BETWEEN -1000000000000 AND 1000000000000 AND amount_yen <> 0),
    PRIMARY KEY (settlement_run_id, member_id, adjustment_kind),
    FOREIGN KEY (household_id, settlement_run_id, member_id) REFERENCES settlement_run_lines(household_id, settlement_run_id, member_id) ON DELETE CASCADE,
    CHECK ((adjustment_kind = 'ADVANCE_CREDIT' AND amount_yen > 0) OR adjustment_kind = 'MONTHLY')
);

CREATE TABLE settlement_run_purchase_snapshots (
    household_id uuid NOT NULL,
    settlement_run_id uuid NOT NULL,
    purchase_id uuid NOT NULL,
    expected_revision bigint NOT NULL CHECK (expected_revision > 0),
    revision bigint NOT NULL CHECK (revision = expected_revision + 1),
    PRIMARY KEY (settlement_run_id, purchase_id),
    FOREIGN KEY (household_id, settlement_run_id) REFERENCES settlement_runs(household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, purchase_id) REFERENCES purchases(household_id, id)
);

CREATE TABLE settlement_adjustments (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL,
    period_start date NOT NULL,
    member_id uuid NOT NULL,
    source_kind text NOT NULL CHECK (source_kind IN ('CORRECTION', 'ADVANCE', 'LOAN', 'REPAYMENT')),
    source_id uuid NOT NULL,
    amount_yen bigint NOT NULL CHECK (amount_yen BETWEEN -1000000000000 AND 1000000000000 AND amount_yen <> 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (household_id, period_start) REFERENCES accounting_periods(household_id, period_start),
    FOREIGN KEY (household_id, member_id) REFERENCES household_members(household_id, member_id),
    UNIQUE (household_id, member_id, source_kind, source_id)
);

CREATE INDEX settlement_adjustments_household_period_idx ON settlement_adjustments(household_id, period_start, member_id);
