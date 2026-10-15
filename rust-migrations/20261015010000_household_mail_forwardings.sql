CREATE TABLE household_mail_forwardings (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    created_by uuid NOT NULL REFERENCES members(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    UNIQUE (household_id, id)
);

CREATE UNIQUE INDEX household_mail_forwardings_active_household_idx
    ON household_mail_forwardings(household_id)
    WHERE revoked_at IS NULL;

