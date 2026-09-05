-- name: receipt__complete :exec
UPDATE email_bridge.receipts SET status=@status, completed_at=CASE WHEN @status='unknown' THEN NULL ELSE clock_timestamp() END,
provider_uid=@uid, uid_validity=@validity, folder=@folder, content_digest=@content,
report_source=CASE WHEN @source::jsonb IS NULL THEN report_source ELSE @source::jsonb END,
report_version=report_version+CASE WHEN @source::jsonb IS NULL THEN 0 ELSE 1 END,
report_pending=report_pending OR @source::jsonb IS NOT NULL,
report_completed=true,
report_after=COALESCE(@after::timestamptz,report_after)
WHERE tenant_id=@tenant AND mailbox_id=@mailbox AND message_id=@id AND input_digest=@digest AND status='unknown' AND NOT source_unlocked
AND (@source::jsonb IS NULL OR (report_version=@version AND report_source_digest=@source_digest AND @after::timestamptz>clock_timestamp()));
