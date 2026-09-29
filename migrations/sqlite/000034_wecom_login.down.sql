DROP INDEX IF EXISTS idx_auth_tokens_external_identity;
ALTER TABLE auth_tokens DROP COLUMN session_family_id;
ALTER TABLE auth_tokens DROP COLUMN external_identity_version;
ALTER TABLE auth_tokens DROP COLUMN external_identity_id;
ALTER TABLE auth_tokens DROP COLUMN auth_method;
DROP TABLE IF EXISTS we_com_identity_events;
DROP TABLE IF EXISTS we_com_flows;
DROP TABLE IF EXISTS we_com_identities;
