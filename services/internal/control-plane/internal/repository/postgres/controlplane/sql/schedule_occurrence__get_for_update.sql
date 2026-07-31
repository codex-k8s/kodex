-- name: ScheduleOccurrenceGetForUpdate
SELECT
    id::text,
    schedule_id::text,
    organization_id::text,
    project_id::text,
    scheduled_for,
    target_resource_id::text,
    target_kind,
    target_version,
    effective_input_sha256,
    prompt_profile_id::text,
    prompt_revision,
    runtime_revision_id::text,
    session_policy,
    coalesce(room_id::text, ''),
    notification_policy,
    maximum_execution_duration_ms,
    coalesce,
    overlap_policy,
    maximum_attempts,
    initial_backoff_ms,
    maximum_backoff_ms,
    dead_letter_at,
    state,
    attempt,
    coalesce(claimant_workload_id, ''),
    coalesce(authority_generation, 0),
    coalesce(token_hash, ''),
    coalesce(lease_expires_at, 'epoch'::timestamptz),
    available_at,
    coalesce(outcome, ''),
    coalesce(result_artifact_id::text, ''),
    claimed_at,
    updated_at
FROM control_plane.schedule_occurrences
WHERE id = @id::uuid
  AND organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
FOR UPDATE
