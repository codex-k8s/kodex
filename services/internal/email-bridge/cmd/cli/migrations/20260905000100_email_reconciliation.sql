-- +goose Up
ALTER TABLE email_bridge.receipts ADD COLUMN source_unlocked boolean NOT NULL DEFAULT false;
DROP INDEX email_bridge.unknown_resource;
CREATE UNIQUE INDEX unknown_resource ON email_bridge.receipts(tenant_id,mailbox_id,resource_digest)
 WHERE status='unknown' AND resource_digest<>'' AND NOT source_unlocked;

CREATE TABLE email_bridge.owner_receipts (
 tenant_id text NOT NULL,
 mailbox_id text NOT NULL,
 message_id text NOT NULL,
 owner_ref text NOT NULL,
 owner_version bigint NOT NULL CHECK (owner_version>0),
 invocation_ref text NOT NULL CHECK (invocation_ref<>''),
 connection_ref text NOT NULL CHECK (connection_ref<>''),
 external_digest text NOT NULL CHECK (external_digest ~ '^[0-9a-f]{64}$'),
 outcome text NOT NULL CHECK (outcome IN ('UNKNOWN_OUTCOME','EFFECT_CONFIRMED','NO_EFFECT_CONFIRMED')),
 reconcile_after timestamptz NOT NULL,
 next_check_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 decision_ref text,
 decision_version bigint,
 decision_outcome text CHECK (decision_outcome IN ('EFFECT_CONFIRMED','NO_EFFECT_CONFIRMED')),
 decision_actor text,
 decision_grant text,
 decision_expires_at timestamptz,
 reconciled_at timestamptz,
 PRIMARY KEY(tenant_id,mailbox_id,message_id),
 UNIQUE(owner_ref),
 FOREIGN KEY(tenant_id,mailbox_id,message_id) REFERENCES email_bridge.receipts(tenant_id,mailbox_id,message_id),
 CHECK ((decision_ref IS NULL AND decision_version IS NULL AND decision_outcome IS NULL AND decision_actor IS NULL AND decision_grant IS NULL AND decision_expires_at IS NULL AND reconciled_at IS NULL)
 OR (decision_ref IS NOT NULL AND decision_ref<>'' AND decision_version IS NOT NULL AND decision_version>0 AND decision_outcome IS NOT NULL AND decision_actor IS NOT NULL AND decision_actor<>'' AND decision_grant IS NOT NULL AND decision_grant<>'' AND decision_expires_at IS NOT NULL AND reconciled_at IS NOT NULL))
);
CREATE INDEX owner_receipts_pending ON email_bridge.owner_receipts(next_check_at,tenant_id,mailbox_id,message_id)
 WHERE decision_ref IS NULL AND outcome='UNKNOWN_OUTCOME';
ALTER TABLE email_bridge.owner_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE email_bridge.owner_receipts FORCE ROW LEVEL SECURITY;
CREATE POLICY runtime_owner_receipts ON email_bridge.owner_receipts
 TO email_bridge_runtime USING (session_user='email_bridge_runtime')
 WITH CHECK (session_user='email_bridge_runtime');
REVOKE ALL ON email_bridge.owner_receipts FROM PUBLIC;
GRANT SELECT,INSERT,UPDATE ON email_bridge.owner_receipts TO email_bridge_runtime;
