-- name: receipt__complete :exec
UPDATE email_bridge.receipts SET status=@status, completed_at=clock_timestamp()
WHERE tenant_id=@tenant AND mailbox_id=@mailbox AND message_id=@id AND input_digest=@digest AND status='unknown';
