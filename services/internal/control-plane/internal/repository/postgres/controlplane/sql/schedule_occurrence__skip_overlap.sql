-- name: ScheduleOccurrenceSkipOverlap
WITH skipped AS (
    UPDATE control_plane.schedule_occurrences AS occurrence
    SET
        state = 'SKIPPED',
        outcome = 'overlap',
        updated_at = @now
    WHERE occurrence.organization_id = @organization_id::uuid
      AND occurrence.project_id = @project_id::uuid
      AND occurrence.state = 'QUEUED'
      AND occurrence.available_at <= @now
      AND occurrence.overlap_policy = 'SKIP'
      AND EXISTS (
          SELECT 1
          FROM control_plane.schedule_occurrences AS active
          WHERE active.schedule_id = occurrence.schedule_id
            AND active.state IN ('CLAIMED', 'WAITING_OWNER', 'CONTINUATION')
      )
    RETURNING occurrence.*
)
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
    coalesce(execution_session_id::text, ''),
    coalesce(execution_session_version, 0),
    coalesce(execution_turn_id::text, ''),
    coalesce(execution_turn_version, 0),
    coalesce(execution_process_run_id::text, ''),
    coalesce(execution_process_version, 0),
    coalesce(execution_runtime_revision_id::text, ''),
    coalesce(execution_runtime_revision_version, 0),
    claimed_at,
    updated_at
FROM skipped
ORDER BY scheduled_for, id
