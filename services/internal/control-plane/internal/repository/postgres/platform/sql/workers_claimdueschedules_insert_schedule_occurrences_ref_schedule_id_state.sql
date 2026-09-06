-- name: workers_claimdueschedules_insert_schedule_occurrences_ref_schedule_id_state :one
INSERT INTO control_plane.schedule_occurrences(
    ref, organization_id, schedule_id, scheduled_for, schedule_version,
    target_type, target_ref, run_name, input, input_digest, state, lease_ref,
    fence_digest, generation, workload_instance, lease_expires_at,
    schedule_revision_id, target_version, target_digest, automation_text,
    automation_text_digest, prompt_inputs, prompt_inputs_digest, initiated_by, completed_at, prompt_input_format
) VALUES (
    $1, $2::uuid, $3::uuid, $4, $5, $6, $7, $8, $9, $10,
    CASE WHEN $23::boolean THEN 'SKIPPED' ELSE 'CLAIMED' END,
    CASE WHEN $23::boolean THEN NULL ELSE $11 END,
    CASE WHEN $23::boolean THEN NULL ELSE $12 END, 1,
    CASE WHEN $23::boolean THEN NULL ELSE $13 END,
    CASE WHEN $23::boolean THEN NULL ELSE $14::timestamptz END,
    $15::uuid, $16, $17, $18, $19, $20, $21, $22::uuid,
    CASE WHEN $23::boolean THEN clock_timestamp() END, 1
)
RETURNING id::text
