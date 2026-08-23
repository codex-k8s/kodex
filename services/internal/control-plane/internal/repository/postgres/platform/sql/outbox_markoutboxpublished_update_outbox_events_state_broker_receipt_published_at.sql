-- name: outbox_markoutboxpublished_update_outbox_events_state_broker_receipt_published_at :exec
UPDATE control_plane.outbox_events SET state='PUBLISHED',broker_receipt=$3,published_at=clock_timestamp(),lease_owner=NULL,lease_expires_at=NULL WHERE event_id=$1::uuid AND state='CLAIMED' AND lease_owner=$2
