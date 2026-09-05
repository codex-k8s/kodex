-- name: receipt__complete :exec
UPDATE email_bridge.receipts SET status=@status, completed_at=CASE WHEN @status='unknown' THEN NULL ELSE clock_timestamp() END,
provider_uid=@uid, uid_validity=@validity, folder=@folder, content_digest=@content
WHERE tenant_id=@tenant AND mailbox_id=@mailbox AND message_id=@id AND input_digest=@digest AND status='unknown' AND NOT source_unlocked;
