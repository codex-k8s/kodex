-- name: workers_claimdueschedules_insert_schedule_occurrences_ref_schedule_id_state :exec
INSERT INTO control_plane.schedule_occurrences(
    ref, organization_id, schedule_id, scheduled_for, schedule_version,
    target_type, target_ref, run_name, input, input_digest, state, lease_ref,
    fence_digest, generation, workload_instance, lease_expires_at,
    schedule_revision_id
) VALUES (
    $1, $2::uuid, $3::uuid, $4, $5, $6, $7, $8, $9, $10,
    'CLAIMED', $11, $12, 1, $13, $14, $15::uuid
)
