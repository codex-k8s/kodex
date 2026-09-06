-- name: report__claim :exec
UPDATE email_bridge.receipts SET report_after=clock_timestamp()+(@seconds*interval '1 second')
WHERE tenant_id=@tenant AND mailbox_id=@mailbox AND message_id=@id AND input_digest=@input
AND report_version=@version AND report_source_digest=@source_digest AND status=@status
AND report_pending AND report_after<=clock_timestamp();
