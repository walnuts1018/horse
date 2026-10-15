CREATE TABLE oidc_sessions (
    token_hash bytea PRIMARY KEY,
    member_id uuid NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    expiry timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
