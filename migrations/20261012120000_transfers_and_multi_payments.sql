CREATE TABLE purchase_payments (
    household_id uuid NOT NULL,
    purchase_id uuid NOT NULL,
    line_no integer NOT NULL CHECK (line_no > 0),
    account_id uuid NOT NULL,
    amount_yen bigint NOT NULL CHECK (amount_yen > 0),
    PRIMARY KEY (purchase_id, line_no),
    FOREIGN KEY (household_id, purchase_id) REFERENCES purchases(household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, account_id) REFERENCES accounts(household_id, id)
);

CREATE TABLE settlement_adjustments (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    period text NOT NULL,
    member_id uuid NOT NULL REFERENCES members(id),
    source_kind text NOT NULL CHECK (source_kind IN ('CORRECTION', 'ADVANCE', 'LOAN', 'REPAYMENT')),
    source_id uuid NOT NULL,
    amount_yen bigint NOT NULL CHECK (amount_yen <> 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (household_id, period) REFERENCES accounting_periods(household_id, period),
    UNIQUE (household_id, member_id, source_kind, source_id)
);

CREATE INDEX settlement_adjustments_period_idx ON settlement_adjustments(household_id, period, member_id);

ALTER TABLE corrections ADD COLUMN effective_period text;
ALTER TABLE corrections ADD CONSTRAINT corrections_effective_period_fk FOREIGN KEY (household_id, effective_period) REFERENCES accounting_periods(household_id, period);

CREATE TABLE correction_payments (
    correction_id uuid NOT NULL REFERENCES corrections(id) ON DELETE CASCADE,
    line_no integer NOT NULL CHECK (line_no > 0),
    account_id uuid NOT NULL REFERENCES accounts(id),
    amount_yen bigint NOT NULL CHECK (amount_yen > 0),
    PRIMARY KEY (correction_id, line_no)
);

ALTER TABLE transfers ADD COLUMN payment_account_id uuid;
ALTER TABLE transfers ADD CONSTRAINT transfers_payment_account_fk FOREIGN KEY (household_id, payment_account_id) REFERENCES accounts(household_id, id);
