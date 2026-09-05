-- name: report__retire :exec
WITH selected AS (
 SELECT tenant_id,mailbox_id,message_id FROM email_bridge.receipts
 WHERE NOT report_pending AND report_source IS NOT NULL AND report_after<=clock_timestamp()
 ORDER BY report_after,tenant_id,mailbox_id,message_id LIMIT @batch FOR UPDATE SKIP LOCKED
)
UPDATE email_bridge.receipts r SET report_source=NULL FROM selected s
WHERE r.tenant_id=s.tenant_id AND r.mailbox_id=s.mailbox_id AND r.message_id=s.message_id;
