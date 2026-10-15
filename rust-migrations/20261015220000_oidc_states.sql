CREATE TABLE oidc_states (
    state_hash bytea PRIMARY KEY,
    nonce text NOT NULL,
    verifier text NOT NULL,
    redirect_to text NOT NULL DEFAULT '/',
    expires_at timestamptz NOT NULL
);
