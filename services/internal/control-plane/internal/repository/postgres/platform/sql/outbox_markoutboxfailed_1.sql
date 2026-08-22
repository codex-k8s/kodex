-- name: platform__outbox_markoutboxfailed_1 :exec
UPDATE control_plane.outbox_events SET state=$3,available_at=clock_timestamp()+$4::interval,lease_owner=NULL,lease_expires_at=NULL WHERE event_id=$1::uuid AND state='CLAIMED' AND lease_owner=$2
