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

ALTER TABLE purchases
    ADD COLUMN payment_account_id uuid,
    ADD COLUMN allocation_rule_version bigint NOT NULL DEFAULT 0,
    ADD COLUMN field_revisions jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN date_precision text NOT NULL DEFAULT 'DATE_TIME'
        CHECK (date_precision IN ('DATE_TIME', 'DATE_ONLY')),
    ADD COLUMN origin text NOT NULL DEFAULT 'UNKNOWN'
        CHECK (origin IN ('AI', 'MANUAL', 'UNKNOWN')),
    ADD CONSTRAINT purchases_household_payment_account_fk
        FOREIGN KEY (household_id, payment_account_id) REFERENCES accounts(household_id, id);

ALTER TABLE purchases
    ALTER COLUMN allocation_rule_version DROP DEFAULT;

ALTER TABLE purchase_items
    ADD COLUMN category_id uuid,
    ADD COLUMN revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    ADD COLUMN field_revisions jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN allocation_override boolean NOT NULL DEFAULT false,
    ADD COLUMN quantity_numerator bigint,
    ADD COLUMN quantity_denominator bigint,
    ADD COLUMN quantity_unit text,
    ADD COLUMN printed_unit_price_yen bigint,
    ADD COLUMN printed_unit_price_quantity_numerator bigint,
    ADD COLUMN printed_unit_price_quantity_denominator bigint,
    ADD COLUMN printed_unit_price_unit text,
    ADD CONSTRAINT purchase_items_household_category_fk
        FOREIGN KEY (household_id, category_id) REFERENCES categories(household_id, id),
    ADD CONSTRAINT purchase_items_quantity_presence_check CHECK (
        (quantity_numerator IS NULL AND quantity_denominator IS NULL AND quantity_unit IS NULL)
        OR (quantity_numerator IS NOT NULL AND quantity_numerator > 0 AND quantity_numerator <= 1000000000000
            AND quantity_denominator IS NOT NULL AND quantity_denominator > 0 AND quantity_denominator <= 1000000
            AND quantity_unit IS NOT NULL AND length(quantity_unit) BETWEEN 1 AND 32)
    ),
    ADD CONSTRAINT purchase_items_printed_unit_price_presence_check CHECK (
        (printed_unit_price_yen IS NULL AND printed_unit_price_quantity_numerator IS NULL
            AND printed_unit_price_quantity_denominator IS NULL AND printed_unit_price_unit IS NULL)
        OR (printed_unit_price_yen IS NOT NULL AND printed_unit_price_yen >= 0 AND printed_unit_price_yen <= 1000000000000
            AND printed_unit_price_quantity_numerator IS NOT NULL AND printed_unit_price_quantity_numerator > 0 AND printed_unit_price_quantity_numerator <= 1000000000000
            AND printed_unit_price_quantity_denominator IS NOT NULL AND printed_unit_price_quantity_denominator > 0 AND printed_unit_price_quantity_denominator <= 1000000
            AND printed_unit_price_unit IS NOT NULL AND length(printed_unit_price_unit) BETWEEN 1 AND 32)
    );

ALTER TABLE purchase_allocations
    ADD COLUMN weight bigint NOT NULL DEFAULT 1 CHECK (weight > 0),
    ADD COLUMN rule_version bigint NOT NULL DEFAULT 1,
    ADD CONSTRAINT purchase_allocations_rule_version_check CHECK (rule_version > 0);

ALTER TABLE purchase_allocations
    ALTER COLUMN weight DROP DEFAULT,
    ALTER COLUMN rule_version DROP DEFAULT;

ALTER TABLE purchase_member_burdens
    ADD COLUMN snapshot_source text NOT NULL DEFAULT 'POSTED'
        CHECK (snapshot_source IN ('POSTED', 'LEGACY_CURRENT_RULES'));

ALTER TABLE purchase_payment_funding_snapshots
    ADD COLUMN snapshot_source text NOT NULL DEFAULT 'POSTED'
        CHECK (snapshot_source IN ('POSTED', 'LEGACY_CURRENT_RULES'));
