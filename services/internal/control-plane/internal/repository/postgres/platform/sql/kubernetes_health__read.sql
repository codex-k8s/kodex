-- name: kubernetes_health__read :one
SELECT attempt.id::text,attempt.snapshot_digest,attempt.input_digest,attempt.lease_ref,attempt.fence_digest,
 attempt.generation,attempt.workload_instance,attempt.expires_at,attempt.registry_revision,attempt.target_ref,
 attempt.target_revision,COALESCE(completion.outcome,''),COALESCE(completion.subject_digest,''),
 COALESCE(completion.state,''),COALESCE(completion.applied,false)
FROM control_plane.kubernetes_identity_health_attempts attempt
LEFT JOIN control_plane.kubernetes_identity_health_completions completion ON completion.attempt_id=attempt.id
WHERE attempt.organization_id=@organization_id::uuid AND ((@shared::boolean AND attempt.shared_test_id=@test_id::uuid) OR (NOT @shared::boolean AND attempt.personal_test_id=@test_id::uuid)) AND attempt.ref=@attempt_ref;
