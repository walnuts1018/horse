CREATE TABLE allocation_rules (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    category_id uuid,
    merchant_key text,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((category_id IS NOT NULL) <> (merchant_key IS NOT NULL)),
    CHECK (merchant_key IS NULL OR (length(merchant_key) BETWEEN 1 AND 240 AND merchant_key = lower(btrim(merchant_key)))),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, category_id) REFERENCES categories(household_id, id)
);

CREATE UNIQUE INDEX allocation_rules_category_key ON allocation_rules(household_id, category_id) WHERE category_id IS NOT NULL;
CREATE UNIQUE INDEX allocation_rules_merchant_key ON allocation_rules(household_id, merchant_key) WHERE merchant_key IS NOT NULL;

CREATE TABLE allocation_rule_weights (
    household_id uuid NOT NULL,
    rule_id uuid NOT NULL,
    pool_id uuid NOT NULL,
    weight bigint NOT NULL CHECK (weight > 0),
    PRIMARY KEY (rule_id, pool_id),
    FOREIGN KEY (household_id, rule_id) REFERENCES allocation_rules(household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, pool_id) REFERENCES pools(household_id, id)
);

ALTER TABLE purchase_items ADD COLUMN allocation_override boolean NOT NULL DEFAULT false;
ALTER TABLE item_allocations ADD COLUMN rule_id uuid;
ALTER TABLE item_allocations ADD CONSTRAINT item_allocations_rule_fk FOREIGN KEY (household_id, rule_id) REFERENCES allocation_rules(household_id, id);
