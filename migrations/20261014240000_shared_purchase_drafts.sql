CREATE TABLE shared_purchase_drafts (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (pg_column_size(payload) <= 262144)
);

CREATE INDEX shared_purchase_drafts_household_updated_at_idx
    ON shared_purchase_drafts (household_id, updated_at DESC);
