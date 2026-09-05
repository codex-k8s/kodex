-- name: owner_receipt__pending :many
SELECT r.tenant_id,r.mailbox_id,r.message_id,r.effect_key,r.input_digest,r.status,r.resource_digest,
 r.actor_id,r.agent_id,r.grant_id,r.operation,r.configuration_revision,r.credential_generation,r.gate_approved,
 o.owner_ref,o.owner_version,o.invocation_ref,o.connection_ref,o.external_digest
FROM email_bridge.owner_receipts o JOIN email_bridge.receipts r USING (tenant_id,mailbox_id,message_id)
WHERE r.status='unknown' AND NOT r.source_unlocked AND o.outcome='UNKNOWN_OUTCOME' AND o.decision_ref IS NULL
 AND o.reconcile_after<=clock_timestamp() AND o.next_check_at<=clock_timestamp()
ORDER BY o.next_check_at,o.tenant_id,o.mailbox_id,o.message_id LIMIT @batch;
