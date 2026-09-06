-- +goose Up
CREATE SCHEMA email_bridge;
REVOKE ALL ON SCHEMA email_bridge FROM PUBLIC;
CREATE TABLE email_bridge.receipts (
 tenant_id text NOT NULL,
 mailbox_id text NOT NULL,
 effect_key text NOT NULL CHECK (length(effect_key) BETWEEN 1 AND 128),
 input_digest text NOT NULL CHECK (length(input_digest)=64),
 message_id text NOT NULL,
 actor_id text NOT NULL CHECK (actor_id<>''),
 agent_id text NOT NULL CHECK (agent_id<>''),
 grant_id text NOT NULL CHECK (grant_id<>''),
 operation text NOT NULL CHECK (operation<>''),
 configuration_revision bigint NOT NULL CHECK (configuration_revision>0),
 credential_generation bigint NOT NULL CHECK (credential_generation>0),
 gate_approved boolean NOT NULL,
 resource_digest text NOT NULL DEFAULT '',
 provider_uid text NOT NULL DEFAULT '',
 uid_validity bigint NOT NULL DEFAULT 0 CHECK (uid_validity BETWEEN 0 AND 4294967295),
 folder text NOT NULL DEFAULT '',
 content_digest text NOT NULL DEFAULT '',
 status text NOT NULL CHECK (status IN ('unknown','accepted','failed','deleted')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 completed_at timestamptz,
 PRIMARY KEY(tenant_id,mailbox_id,effect_key),
 UNIQUE(tenant_id,mailbox_id,message_id)
);
CREATE UNIQUE INDEX unknown_resource ON email_bridge.receipts(tenant_id,mailbox_id,resource_digest)
 WHERE status='unknown' AND resource_digest<>'';
CREATE TABLE email_bridge.configuration_watermark (
 singleton boolean PRIMARY KEY CHECK (singleton),
 revision bigint NOT NULL CHECK (revision>0),
 digest text NOT NULL CHECK (length(digest)=64)
);
ALTER TABLE email_bridge.receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE email_bridge.receipts FORCE ROW LEVEL SECURITY;
CREATE POLICY runtime_receipts ON email_bridge.receipts
 TO email_bridge_runtime USING (session_user='email_bridge_runtime')
 WITH CHECK (session_user='email_bridge_runtime');
ALTER TABLE email_bridge.configuration_watermark ENABLE ROW LEVEL SECURITY;
ALTER TABLE email_bridge.configuration_watermark FORCE ROW LEVEL SECURITY;
CREATE POLICY runtime_configuration ON email_bridge.configuration_watermark
 TO email_bridge_runtime USING (session_user='email_bridge_runtime')
 WITH CHECK (session_user='email_bridge_runtime');
GRANT USAGE ON SCHEMA email_bridge TO email_bridge_runtime;
GRANT SELECT,INSERT,UPDATE ON email_bridge.receipts,email_bridge.configuration_watermark TO email_bridge_runtime;
REVOKE ALL ON ALL TABLES IN SCHEMA email_bridge FROM PUBLIC;
