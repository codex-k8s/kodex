-- name: owner_receipt__claim :exec
UPDATE email_bridge.owner_receipts SET next_check_at=clock_timestamp()+@seconds::double precision*interval '1 second'
WHERE tenant_id=@tenant AND mailbox_id=@mailbox AND message_id=@id AND owner_ref=@owner
 AND owner_version=@version AND external_digest=@digest AND decision_ref IS NULL AND outcome='UNKNOWN_OUTCOME'
 AND reconcile_after<=clock_timestamp() AND next_check_at<=clock_timestamp();
