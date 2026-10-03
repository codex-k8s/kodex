-- name: organization_role_image_component_expiry :exec
UPDATE control_plane.image_builds SET lease_expires_at=clock_timestamp()-interval '1 second'
WHERE ref=$1;
