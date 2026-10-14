-- Old binaries do not enforce external identity provenance. Revoke these
-- sessions before dropping it, including refresh tokens, so a later upgrade
-- cannot mistake them for legacy password/OIDC sessions.
UPDATE auth_tokens SET is_revoked = TRUE
WHERE auth_method = 'wecom' OR external_identity_id <> '';

DROP INDEX IF EXISTS idx_auth_tokens_external_identity;
ALTER TABLE auth_tokens DROP COLUMN session_family_id;
ALTER TABLE auth_tokens DROP COLUMN external_identity_version;
ALTER TABLE auth_tokens DROP COLUMN external_identity_id;
ALTER TABLE auth_tokens DROP COLUMN auth_method;
DROP TABLE IF EXISTS we_com_identity_events;
DROP TABLE IF EXISTS we_com_flows;
DROP TABLE IF EXISTS we_com_identities;
