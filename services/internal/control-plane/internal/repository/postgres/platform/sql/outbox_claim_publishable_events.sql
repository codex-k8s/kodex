-- name: platform__outbox_claim_publishable_events :many
WITH candidates AS (
		SELECT e.id FROM control_plane.outbox_events e
		WHERE ((e.state='PENDING' AND e.available_at<=clock_timestamp()) OR (e.state='CLAIMED' AND e.lease_expires_at<clock_timestamp()))
		AND NOT EXISTS(SELECT 1 FROM control_plane.outbox_events predecessor WHERE predecessor.ordering_key=e.ordering_key AND predecessor.sequence<e.sequence AND predecessor.state<>'PUBLISHED')
		ORDER BY e.created_at FOR UPDATE SKIP LOCKED LIMIT $1
	), claimed AS (
		UPDATE control_plane.outbox_events e SET state='CLAIMED',lease_owner=$2,lease_expires_at=clock_timestamp()+$3::interval,attempts=attempts+1
		FROM candidates c WHERE e.id=c.id RETURNING e.event_id::text,e.subject,e.payload,e.attempts
	) SELECT event_id,subject,payload,attempts FROM claimed
