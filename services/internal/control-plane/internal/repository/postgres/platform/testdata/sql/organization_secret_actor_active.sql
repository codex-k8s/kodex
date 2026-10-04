-- name: organization_secret_actor_active_fixture :exec
UPDATE control_plane.subjects
SET active = $2
WHERE id::text = $1;
