-- name: kubernetes_setup__attempt_lock :one
SELECT state,expires_at,clock_timestamp()
FROM control_plane.kubernetes_identity_upload_attempts
WHERE id=@attempt_id::uuid AND organization_id=@organization_id::uuid AND setup_id=@setup_id::uuid
 AND ref=@attempt_ref AND generation=@generation AND workload_instance=@instance AND fence_sha256=@fence_digest
FOR UPDATE;
