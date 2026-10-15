CREATE TABLE shared_purchase_drafts (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    payload jsonb NOT NULL CHECK (pg_column_size(payload) <= 524288),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX shared_purchase_drafts_household_updated_at_idx
    ON shared_purchase_drafts (household_id, updated_at DESC);
