-- name: kubernetes_setup__private_lock :one
SELECT version,state FROM control_plane.kubernetes_identity_setups
WHERE organization_id=@organization_id::uuid AND id=@setup_id::uuid FOR UPDATE;
