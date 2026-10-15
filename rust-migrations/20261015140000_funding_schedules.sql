CREATE TABLE funding_schedules (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL,
    member_id uuid NOT NULL,
    pool_id uuid NOT NULL,
    amount_yen bigint NOT NULL CHECK (amount_yen BETWEEN 0 AND 1000000000000),
    cadence text NOT NULL CHECK (cadence IN ('MONTHLY', 'WEEKLY', 'CUSTOM')),
    starts_on date NOT NULL,
    ends_on date,
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    UNIQUE (household_id, id, member_id, pool_id),
    FOREIGN KEY (household_id, member_id) REFERENCES household_members(household_id, member_id),
    FOREIGN KEY (household_id, pool_id) REFERENCES pools(household_id, id),
    CHECK (ends_on IS NULL OR ends_on >= starts_on)
);

CREATE UNIQUE INDEX funding_schedules_one_active_per_member_pool_idx
    ON funding_schedules(household_id, member_id, pool_id)
    WHERE active;

CREATE INDEX funding_schedules_household_dates_idx
    ON funding_schedules(household_id, starts_on, ends_on);

CREATE TABLE funding_obligations (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL,
    schedule_id uuid,
    member_id uuid NOT NULL,
    pool_id uuid NOT NULL,
    period_start date NOT NULL CHECK (EXTRACT(DAY FROM period_start) = 1),
    amount_yen bigint NOT NULL CHECK (amount_yen >= 0),
    paid_yen bigint NOT NULL DEFAULT 0 CHECK (paid_yen >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    UNIQUE (household_id, member_id, pool_id, period_start),
    FOREIGN KEY (household_id, schedule_id, member_id, pool_id)
        REFERENCES funding_schedules(household_id, id, member_id, pool_id),
    FOREIGN KEY (household_id, member_id) REFERENCES household_members(household_id, member_id),
    FOREIGN KEY (household_id, pool_id) REFERENCES pools(household_id, id),
    FOREIGN KEY (household_id, period_start) REFERENCES accounting_periods(household_id, period_start)
);

CREATE INDEX funding_obligations_household_period_idx
    ON funding_obligations(household_id, period_start, member_id, pool_id);
