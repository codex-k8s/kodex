-- name: kubernetes_setup__attempt_lookup :one
SELECT setup.permit_sha256,attempt.id::text
FROM control_plane.kubernetes_identity_upload_attempts attempt
JOIN control_plane.kubernetes_identity_setups setup ON setup.id=attempt.setup_id AND setup.organization_id=attempt.organization_id
WHERE setup.ref=@setup_ref AND attempt.ref=@attempt_ref AND attempt.generation=@generation
 AND attempt.workload_instance=@instance AND attempt.fence_sha256=@fence_digest;
