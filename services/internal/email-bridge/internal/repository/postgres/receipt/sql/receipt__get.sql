-- name: receipt__get :one
SELECT message_id, effect_key, input_digest, status FROM email_bridge.receipts
WHERE tenant_id=@tenant AND mailbox_id=@mailbox
AND ((@id::text <> '' AND message_id=@id) OR (@key::text <> '' AND effect_key=@key));
