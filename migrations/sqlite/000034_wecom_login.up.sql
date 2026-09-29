CREATE TABLE we_com_identities (
 id VARCHAR(36) PRIMARY KEY,
 corp_id VARCHAR(128) NOT NULL,
 subject VARCHAR(64) NOT NULL,
 user_id VARCHAR(36) NOT NULL REFERENCES users(id),
 display_name VARCHAR(255) NOT NULL,
 status VARCHAR(16) NOT NULL CHECK (status IN ('active', 'suspended', 'revoked')),
 version BIGINT NOT NULL DEFAULT 1,
 created_at DATETIME NOT NULL,
 updated_at DATETIME NOT NULL,
 UNIQUE (corp_id, subject)
);
CREATE UNIQUE INDEX wecom_user ON we_com_identities(corp_id, user_id) WHERE status <> 'revoked';
CREATE TABLE we_com_flows (
 corp_id VARCHAR(128) NOT NULL DEFAULT '',
 id VARCHAR(36) PRIMARY KEY,
 purpose VARCHAR(16) NOT NULL,
 secret_hash VARCHAR(64) NOT NULL UNIQUE,
 browser_hash VARCHAR(64) NOT NULL DEFAULT '',
 user_id VARCHAR(36) NOT NULL DEFAULT '',
 identity_id VARCHAR(36) NOT NULL DEFAULT '',
 identity_version BIGINT NOT NULL DEFAULT 0,
 subject VARCHAR(64) NOT NULL DEFAULT '',
 display_name VARCHAR(255) NOT NULL DEFAULT '',
 actor_id VARCHAR(36) NOT NULL DEFAULT '',
 expires_at DATETIME NOT NULL
);
CREATE INDEX idx_we_com_flows_expires_at ON we_com_flows(expires_at);
CREATE TABLE we_com_identity_events (
 id VARCHAR(36) PRIMARY KEY,
 identity_id VARCHAR(36) NOT NULL,
 actor_id VARCHAR(36) NOT NULL,
 action VARCHAR(32) NOT NULL,
 user_id VARCHAR(36) NOT NULL,
 version BIGINT NOT NULL,
 created_at DATETIME NOT NULL
);
CREATE INDEX idx_we_com_identity_events_identity_id ON we_com_identity_events(identity_id);
ALTER TABLE auth_tokens ADD COLUMN auth_method VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE auth_tokens ADD COLUMN external_identity_id VARCHAR(36) NOT NULL DEFAULT '';
ALTER TABLE auth_tokens ADD COLUMN external_identity_version BIGINT NOT NULL DEFAULT 0;
ALTER TABLE auth_tokens ADD COLUMN session_family_id VARCHAR(36) NOT NULL DEFAULT '';
CREATE INDEX idx_auth_tokens_external_identity ON auth_tokens(external_identity_id);
