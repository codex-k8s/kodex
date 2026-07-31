-- name: ScheduleOccurrenceRecoverExpired
WITH recovered AS (
    UPDATE control_plane.schedule_occurrences AS occurrence
    SET
        state = CASE
            WHEN occurrence.attempt >= occurrence.maximum_attempts
              OR @now >= occurrence.dead_letter_at
            THEN 'DEAD_LETTER'
            ELSE 'QUEUED'
        END,
        attempt = CASE
            WHEN occurrence.attempt >= occurrence.maximum_attempts
              OR @now >= occurrence.dead_letter_at
            THEN occurrence.attempt
            ELSE occurrence.attempt + 1
        END,
        claimant_workload_id = NULL,
        authority_generation = NULL,
        token_hash = NULL,
        lease_expires_at = NULL,
        available_at = CASE
            WHEN occurrence.attempt >= occurrence.maximum_attempts
              OR @now >= occurrence.dead_letter_at
            THEN occurrence.available_at
            ELSE @now + make_interval(
                secs => least(
                    occurrence.maximum_backoff_ms,
                    occurrence.initial_backoff_ms *
                        power(2::numeric, occurrence.attempt - 1)
                )::double precision / 1000
            )
        END,
        outcome = 'lease_expired',
        updated_at = @now
    WHERE occurrence.organization_id = @organization_id::uuid
      AND occurrence.project_id = @project_id::uuid
      AND occurrence.state = 'CLAIMED'
      AND occurrence.lease_expires_at <= @now
    RETURNING occurrence.*
), finished_runs AS (
    UPDATE control_plane.scheduled_runs AS run
    SET state = 'FAILED',
        outcome = 'lease_expired',
        finished_at = @now
    FROM recovered
    WHERE run.occurrence_id = recovered.id
      AND run.attempt = CASE
          WHEN recovered.state = 'QUEUED' THEN recovered.attempt - 1
          ELSE recovered.attempt
      END
      AND run.state = 'CLAIMED'
    RETURNING run.occurrence_id
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
FROM recovered
ORDER BY scheduled_for, id
