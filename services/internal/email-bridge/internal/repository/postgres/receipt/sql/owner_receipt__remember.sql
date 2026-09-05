-- name: owner_receipt__remember :exec
INSERT INTO email_bridge.owner_receipts AS current
 (tenant_id,mailbox_id,message_id,owner_ref,owner_version,invocation_ref,connection_ref,external_digest,outcome,reconcile_after)
SELECT tenant_id,mailbox_id,message_id,@owner_ref,@version,@invocation,@connection,@external_digest,@outcome,@after
FROM email_bridge.receipts WHERE tenant_id=@tenant AND mailbox_id=@mailbox AND message_id=@id AND input_digest=@input
ON CONFLICT (tenant_id,mailbox_id,message_id) DO UPDATE
SET owner_version=GREATEST(current.owner_version,EXCLUDED.owner_version),
 outcome=CASE WHEN EXCLUDED.owner_version>current.owner_version THEN EXCLUDED.outcome ELSE current.outcome END
WHERE current.owner_ref=EXCLUDED.owner_ref AND current.invocation_ref=EXCLUDED.invocation_ref
 AND current.connection_ref=EXCLUDED.connection_ref AND current.external_digest=EXCLUDED.external_digest
 AND ((current.owner_version=EXCLUDED.owner_version AND current.outcome=EXCLUDED.outcome)
 OR (current.owner_version<EXCLUDED.owner_version AND (current.outcome='UNKNOWN_OUTCOME' OR current.outcome=EXCLUDED.outcome) AND current.decision_ref IS NULL)
 OR (current.owner_version>EXCLUDED.owner_version AND EXCLUDED.outcome='UNKNOWN_OUTCOME'));
