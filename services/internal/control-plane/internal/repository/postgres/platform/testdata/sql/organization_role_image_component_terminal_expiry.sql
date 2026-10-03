-- name: organization_role_image_component_terminal_expiry :exec
UPDATE control_plane.image_builds SET maximum_attempts=attempt,
    lease_expires_at=clock_timestamp()-interval '1 second'
WHERE ref=$1;
