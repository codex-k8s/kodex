-- name: workers_claimdueschedules_insert_attempt :exec
INSERT INTO control_plane.schedule_occurrence_attempts(
    ref, organization_id, occurrence_id, attempt, generation, lease_ref,
    fence_digest, workload_instance, state, input_digest,
    schedule_revision_digest, expires_at, credential_generation
) VALUES ($1, $2::uuid, $3::uuid, $4, $5, $6, $7, $8, 'CLAIMED', $9, $10, $11, $12)
