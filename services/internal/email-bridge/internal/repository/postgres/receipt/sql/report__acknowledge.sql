-- name: report__acknowledge :exec
UPDATE email_bridge.receipts SET report_pending=false,
report_source=CASE WHEN report_completed OR @after::timestamptz<=clock_timestamp() THEN NULL ELSE report_source END
WHERE tenant_id=@tenant AND mailbox_id=@mailbox AND message_id=@id AND input_digest=@input
AND report_version=@version AND report_source_digest=@source_digest AND status=@status;
