-- name: report__pending :many
SELECT tenant_id,mailbox_id,message_id,effect_key,input_digest,status,resource_digest,
actor_id,agent_id,grant_id,operation,configuration_revision,credential_generation,gate_approved,
report_version,report_source,report_source_digest
FROM email_bridge.receipts WHERE report_pending AND report_after<=clock_timestamp()
ORDER BY report_after,tenant_id,mailbox_id,message_id LIMIT @batch;
