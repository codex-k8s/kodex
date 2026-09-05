-- name: receipt__get :one
SELECT message_id, effect_key, input_digest, status, resource_digest, provider_uid, uid_validity, folder, content_digest,
actor_id,agent_id,grant_id,operation,configuration_revision,credential_generation,gate_approved,report_version FROM email_bridge.receipts
WHERE tenant_id=@tenant AND mailbox_id=@mailbox
AND ((@id::text <> '' AND message_id=@id) OR (@key::text <> '' AND effect_key=@key));
