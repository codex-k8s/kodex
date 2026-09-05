-- name: receipt__reserve :one
INSERT INTO email_bridge.receipts (tenant_id, mailbox_id, effect_key, input_digest, message_id, status, resource_digest,
actor_id,agent_id,grant_id,operation,configuration_revision,credential_generation,gate_approved,
report_source,report_source_digest,report_version,report_pending,report_after)
VALUES (@tenant, @mailbox, @key, @digest, @id, 'unknown', @resource,
@actor,@agent,@grant,@operation,@configuration,@generation,@gate,
@source::jsonb,@source_digest,CASE WHEN @source::jsonb IS NULL THEN 0 ELSE 1 END,@source::jsonb IS NOT NULL,@after::timestamptz)
ON CONFLICT (tenant_id, mailbox_id, effect_key) DO NOTHING
RETURNING message_id, effect_key, input_digest, status, resource_digest, provider_uid, uid_validity, folder, content_digest,
actor_id,agent_id,grant_id,operation,configuration_revision,credential_generation,gate_approved,report_version;
