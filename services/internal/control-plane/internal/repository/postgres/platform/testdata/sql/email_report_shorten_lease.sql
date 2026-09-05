-- name: email_report_shorten_lease :one
UPDATE control_plane.integration_invocations
SET lease_expires_at=clock_timestamp()+interval '3 seconds'
WHERE ref=$1 AND state='RUNNING'
RETURNING lease_expires_at;
