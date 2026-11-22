-- name: ssh_setup__private_lock :one
SELECT id::text FROM control_plane.ssh_identity_setups
WHERE id=@setup_id::uuid AND organization_id=@organization_id::uuid FOR UPDATE;
