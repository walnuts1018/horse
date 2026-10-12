ALTER TABLE purchases
    ADD COLUMN origin text NOT NULL DEFAULT 'UNKNOWN' CHECK (origin IN ('AI', 'MANUAL', 'UNKNOWN'));

ALTER TABLE corrections
    ADD CONSTRAINT corrections_household_id_id_key UNIQUE (household_id, id);

CREATE INDEX purchases_household_origin_occurred_idx ON purchases(household_id, origin, occurred_at DESC, id DESC);

CREATE TABLE purchase_member_burdens (
    household_id uuid NOT NULL,
    purchase_id uuid NOT NULL,
    item_id uuid NOT NULL,
    pool_id uuid NOT NULL,
    member_id uuid NOT NULL,
    amount_yen bigint NOT NULL CHECK (amount_yen >= 0),
    funding_rule_version integer NOT NULL CHECK (funding_rule_version > 0),
    funding_weight bigint NOT NULL CHECK (funding_weight > 0),
    PRIMARY KEY (item_id, pool_id, member_id),
    FOREIGN KEY (household_id, purchase_id, item_id) REFERENCES purchase_items(household_id, purchase_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, pool_id, funding_rule_version) REFERENCES pool_funding_rules(household_id, pool_id, version),
    FOREIGN KEY (household_id, member_id) REFERENCES household_members(household_id, member_id)
);

CREATE INDEX purchase_member_burdens_household_purchase_idx ON purchase_member_burdens(household_id, purchase_id);

CREATE TABLE correction_member_burdens (
    household_id uuid NOT NULL,
    correction_id uuid NOT NULL,
    item_id uuid,
    pool_id uuid NOT NULL,
    member_id uuid NOT NULL,
    amount_delta_yen bigint NOT NULL CHECK (amount_delta_yen <> 0),
    funding_rule_version integer NOT NULL CHECK (funding_rule_version > 0),
    funding_weight bigint NOT NULL CHECK (funding_weight > 0),
    PRIMARY KEY (correction_id, pool_id, member_id),
    FOREIGN KEY (household_id, pool_id, funding_rule_version) REFERENCES pool_funding_rules(household_id, pool_id, version),
    FOREIGN KEY (household_id, member_id) REFERENCES household_members(household_id, member_id),
    FOREIGN KEY (household_id, correction_id) REFERENCES corrections(household_id, id) ON DELETE CASCADE
);

CREATE INDEX correction_member_burdens_household_correction_idx ON correction_member_burdens(household_id, correction_id);
