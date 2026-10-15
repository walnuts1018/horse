CREATE TABLE product_concepts (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 240),
    normalized_name text NOT NULL CHECK (length(normalized_name) BETWEEN 1 AND 240),
    category_id uuid,
    archived boolean NOT NULL DEFAULT false,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    UNIQUE (household_id, normalized_name),
    FOREIGN KEY (household_id, category_id) REFERENCES categories(household_id, id)
);

CREATE TABLE product_variants (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL,
    concept_id uuid NOT NULL,
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 240),
    normalized_name text NOT NULL CHECK (length(normalized_name) BETWEEN 1 AND 240),
    brand text NOT NULL DEFAULT '',
    package_size text NOT NULL DEFAULT '',
    barcode text,
    archived boolean NOT NULL DEFAULT false,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    UNIQUE (household_id, concept_id, normalized_name),
    FOREIGN KEY (household_id, concept_id) REFERENCES product_concepts(household_id, id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX product_variants_barcode_key
    ON product_variants(household_id, barcode)
    WHERE barcode IS NOT NULL;

CREATE TABLE product_aliases (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    merchant_key text NOT NULL CHECK (length(merchant_key) BETWEEN 1 AND 240),
    alias_text text NOT NULL CHECK (length(btrim(alias_text)) BETWEEN 1 AND 240),
    normalized_alias text NOT NULL CHECK (length(normalized_alias) BETWEEN 1 AND 240),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    UNIQUE (household_id, merchant_key, normalized_alias),
    FOREIGN KEY (household_id, variant_id) REFERENCES product_variants(household_id, id) ON DELETE CASCADE
);

CREATE INDEX product_aliases_lookup_idx
    ON product_aliases(household_id, merchant_key, normalized_alias);
CREATE INDEX product_concepts_search_idx
    ON product_concepts(household_id, normalized_name);
CREATE INDEX product_variants_search_idx
    ON product_variants(household_id, normalized_name);

ALTER TABLE purchase_items
    ADD COLUMN product_variant_id uuid,
    ADD CONSTRAINT purchase_items_household_product_variant_fk
        FOREIGN KEY (household_id, product_variant_id) REFERENCES product_variants(household_id, id);

CREATE INDEX purchase_items_product_variant_idx
    ON purchase_items(household_id, product_variant_id)
    WHERE product_variant_id IS NOT NULL;

CREATE TABLE allocation_rules (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    category_id uuid,
    merchant_key text,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    product_concept_id uuid,
    product_variant_id uuid,
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, category_id) REFERENCES categories(household_id, id),
    FOREIGN KEY (household_id, product_concept_id) REFERENCES product_concepts(household_id, id),
    FOREIGN KEY (household_id, product_variant_id) REFERENCES product_variants(household_id, id),
    CHECK (num_nonnulls(category_id, merchant_key, product_concept_id, product_variant_id) = 1),
    CHECK (merchant_key IS NULL OR (length(merchant_key) BETWEEN 1 AND 240 AND merchant_key = lower(btrim(merchant_key))))
);

CREATE UNIQUE INDEX allocation_rules_category_key
    ON allocation_rules(household_id, category_id)
    WHERE category_id IS NOT NULL;
CREATE UNIQUE INDEX allocation_rules_merchant_key
    ON allocation_rules(household_id, merchant_key)
    WHERE merchant_key IS NOT NULL;
CREATE UNIQUE INDEX allocation_rules_product_concept_key
    ON allocation_rules(household_id, product_concept_id)
    WHERE product_concept_id IS NOT NULL;
CREATE UNIQUE INDEX allocation_rules_product_variant_key
    ON allocation_rules(household_id, product_variant_id)
    WHERE product_variant_id IS NOT NULL;

CREATE TABLE allocation_defaults (
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    version bigint NOT NULL,
    pool_id uuid NOT NULL,
    weight bigint NOT NULL CHECK (weight > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (household_id, version, pool_id),
    FOREIGN KEY (household_id, pool_id) REFERENCES pools(household_id, id)
);

CREATE TABLE allocation_rule_weights (
    household_id uuid NOT NULL,
    rule_id uuid NOT NULL,
    pool_id uuid NOT NULL,
    weight bigint NOT NULL CHECK (weight > 0),
    PRIMARY KEY (rule_id, pool_id),
    FOREIGN KEY (household_id, rule_id) REFERENCES allocation_rules(household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, pool_id) REFERENCES pools(household_id, id)
);

ALTER TABLE purchase_allocations
    ADD COLUMN rule_id uuid,
    ADD CONSTRAINT purchase_allocations_household_rule_fk
        FOREIGN KEY (household_id, rule_id) REFERENCES allocation_rules(household_id, id);

CREATE TABLE product_corrections (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL,
    purchase_id uuid NOT NULL,
    item_id uuid NOT NULL,
    raw_name text NOT NULL,
    product_variant_id uuid NOT NULL,
    learning_scope text NOT NULL CHECK (learning_scope IN ('ITEM_ONLY', 'VARIANT_NAME', 'MERCHANT_ALIAS')),
    merchant_key text NOT NULL DEFAULT '',
    actor_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    previous_product_variant_id uuid,
    FOREIGN KEY (household_id, purchase_id, item_id) REFERENCES purchase_items(household_id, purchase_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, product_variant_id) REFERENCES product_variants(household_id, id),
    FOREIGN KEY (household_id, previous_product_variant_id) REFERENCES product_variants(household_id, id),
    FOREIGN KEY (household_id, actor_id) REFERENCES household_members(household_id, member_id)
);

CREATE INDEX product_corrections_household_item_idx
    ON product_corrections(household_id, item_id, created_at DESC);

ALTER TABLE household_members
    ADD COLUMN left_at timestamptz;
