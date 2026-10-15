CREATE TABLE household_invitations (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    role text NOT NULL DEFAULT 'MEMBER' CHECK (role = 'MEMBER'),
    expires_at timestamptz NOT NULL,
    created_by uuid NOT NULL REFERENCES members(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    accepted_by uuid REFERENCES members(id),
    accepted_at timestamptz,
    CHECK ((accepted_by IS NULL) = (accepted_at IS NULL)),
    CHECK (accepted_at IS NULL OR revoked_at IS NULL),
    UNIQUE (household_id, id)
);

CREATE INDEX household_invitations_household_created_idx ON household_invitations(household_id, created_at DESC, id);
