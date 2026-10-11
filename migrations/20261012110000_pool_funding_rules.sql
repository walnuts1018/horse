ALTER TABLE pools
    ADD COLUMN funding_rule_version integer NOT NULL DEFAULT 1 CHECK (funding_rule_version > 0);

CREATE TABLE pool_funding_rules (
    household_id uuid NOT NULL,
    pool_id uuid NOT NULL,
    version integer NOT NULL CHECK (version > 0),
    owner_member_id uuid,
    settlement_policy text CHECK (settlement_policy IN ('BALANCE_TO_STANDARD', 'REIMBURSE_PERSONAL')),
    created_by uuid NOT NULL REFERENCES members(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (household_id, pool_id, version),
    FOREIGN KEY (household_id, pool_id) REFERENCES pools(household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, owner_member_id) REFERENCES household_members(household_id, member_id),
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

ALTER TABLE settlement_runs
    ADD COLUMN funding_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE settlement_lines
    ADD COLUMN monthly_adjustment_yen bigint NOT NULL DEFAULT 0;

INSERT INTO pool_funding_rules(household_id, pool_id, version, created_by)
SELECT p.household_id, p.id, p.funding_rule_version, hm.member_id
FROM pools p
JOIN household_members hm ON hm.household_id = p.household_id AND hm.role = 'OWNER';
