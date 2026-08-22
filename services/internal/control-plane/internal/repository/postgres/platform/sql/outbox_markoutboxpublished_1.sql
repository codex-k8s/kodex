-- name: platform__outbox_markoutboxpublished_1 :exec
UPDATE control_plane.outbox_events SET state='PUBLISHED',broker_receipt=$3,published_at=clock_timestamp(),lease_owner=NULL,lease_expires_at=NULL WHERE event_id=$1::uuid AND state='CLAIMED' AND lease_owner=$2
