ALTER TABLE oidc_sessions DROP COLUMN IF EXISTS access_token;
ALTER TABLE oidc_sessions DROP COLUMN IF EXISTS refresh_token;
