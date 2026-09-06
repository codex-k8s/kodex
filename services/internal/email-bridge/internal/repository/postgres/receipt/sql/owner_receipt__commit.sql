-- name: owner_receipt__commit :one
WITH locked AS MATERIALIZED (
 SELECT r.tenant_id,r.mailbox_id,r.message_id,o.decision_ref
 FROM email_bridge.receipts r JOIN email_bridge.owner_receipts o USING (tenant_id,mailbox_id,message_id)
 WHERE r.tenant_id=@tenant AND r.mailbox_id=@mailbox AND r.message_id=@id AND r.input_digest=@input
 AND r.status='unknown' AND o.owner_ref=@owner AND o.owner_version=@owner_version
 AND o.invocation_ref=@invocation AND o.connection_ref=@connection AND o.external_digest=@digest
 AND o.outcome='UNKNOWN_OUTCOME' AND o.reconcile_after<=clock_timestamp()
 FOR UPDATE OF r,o
), audit AS (
 UPDATE email_bridge.owner_receipts o SET decision_ref=@decision,decision_version=@decision_version,
 decision_outcome=@outcome,decision_actor=@actor,decision_grant=@grant,decision_expires_at=@expires,reconciled_at=clock_timestamp()
 FROM locked l WHERE o.tenant_id=l.tenant_id AND o.mailbox_id=l.mailbox_id AND o.message_id=l.message_id
 AND l.decision_ref IS NULL AND @expires::timestamptz>clock_timestamp()+interval '100 milliseconds'
 RETURNING o.tenant_id,o.mailbox_id,o.message_id
), unlocked AS (
 UPDATE email_bridge.receipts r SET source_unlocked=true FROM audit a
 WHERE r.tenant_id=a.tenant_id AND r.mailbox_id=a.mailbox_id AND r.message_id=a.message_id
 RETURNING r.message_id
)
SELECT EXISTS(SELECT 1 FROM unlocked) AS changed,
 EXISTS(SELECT 1 FROM locked l JOIN email_bridge.owner_receipts o USING (tenant_id,mailbox_id,message_id)
 WHERE o.decision_ref=@decision AND o.decision_version=@decision_version AND o.decision_outcome=@outcome
 AND o.decision_actor=@actor AND o.decision_grant=@grant AND o.decision_expires_at=@expires) AS replay;
