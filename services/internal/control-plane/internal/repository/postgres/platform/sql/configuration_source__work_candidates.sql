-- name: configuration_source__work_candidates :many
SELECT work.ref FROM control_plane.managed_configuration_source_work work
WHERE work.organization_id=$1::uuid AND (work.state='QUEUED' OR work.state='CLAIMED' AND work.lease_expires_at<=clock_timestamp())
ORDER BY work.created_at,work.id LIMIT $2;
