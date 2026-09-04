-- name: receipt__reserve :one
INSERT INTO email_bridge.receipts (tenant_id, mailbox_id, effect_key, input_digest, message_id, status)
VALUES (@tenant, @mailbox, @key, @digest, @id, 'unknown')
ON CONFLICT (tenant_id, mailbox_id, effect_key) DO NOTHING
RETURNING message_id, effect_key, input_digest, status;
