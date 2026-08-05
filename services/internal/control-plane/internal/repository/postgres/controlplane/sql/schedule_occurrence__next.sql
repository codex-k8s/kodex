-- name: ScheduleOccurrenceNext
WITH candidate AS (
    SELECT occurrence.id
    FROM control_plane.schedule_occurrences AS occurrence
    JOIN control_plane.resources AS schedule
      ON schedule.organization_id = occurrence.organization_id
     AND schedule.project_id = occurrence.project_id
     AND schedule.id = occurrence.schedule_id
     AND schedule.kind = 'SCHEDULE'
     AND schedule.state = 'ACTIVE'
    WHERE occurrence.organization_id = @organization_id::uuid
      AND occurrence.project_id = @project_id::uuid
      AND occurrence.state = 'QUEUED'
      AND occurrence.available_at <= @now
      AND NOT EXISTS (
          SELECT 1
          FROM control_plane.schedule_occurrences AS active
          WHERE active.organization_id = occurrence.organization_id
            AND active.project_id = occurrence.project_id
            AND active.schedule_id = occurrence.schedule_id
            AND active.state = ANY(@open_execution_states::text[])
      )
      AND NOT EXISTS (
          SELECT 1
          FROM control_plane.schedule_occurrences AS run_owner
          JOIN control_plane.scheduled_runs AS open_run
            ON open_run.occurrence_id = run_owner.id
           AND open_run.state = ANY(@open_execution_states::text[])
          WHERE run_owner.organization_id = occurrence.organization_id
            AND run_owner.project_id = occurrence.project_id
            AND run_owner.schedule_id = occurrence.schedule_id
      )
      AND NOT EXISTS (
          SELECT 1
          FROM control_plane.schedule_occurrences AS predecessor
          WHERE predecessor.organization_id = occurrence.organization_id
            AND predecessor.project_id = occurrence.project_id
            AND predecessor.schedule_id = occurrence.schedule_id
            AND (
                predecessor.scheduled_for < occurrence.scheduled_for
                OR (
                    predecessor.scheduled_for = occurrence.scheduled_for
                    AND predecessor.id < occurrence.id
                )
            )
            AND (
                predecessor.state = 'QUEUED'
                OR predecessor.state = ANY(@open_execution_states::text[])
            )
      )
    ORDER BY occurrence.available_at, occurrence.scheduled_for, occurrence.id
    FOR UPDATE OF occurrence SKIP LOCKED
    LIMIT 1
)
SELECT
    occurrence.id::text,
    occurrence.schedule_id::text,
    occurrence.organization_id::text,
    occurrence.project_id::text,
    occurrence.scheduled_for,
    occurrence.target_resource_id::text,
    occurrence.target_kind,
    occurrence.target_version,
    occurrence.effective_input_sha256,
    occurrence.prompt_profile_id::text,
    occurrence.prompt_revision,
    occurrence.runtime_revision_id::text,
    occurrence.session_policy,
    coalesce(occurrence.room_id::text, ''),
    occurrence.notification_policy,
    occurrence.maximum_execution_duration_ms,
    occurrence.coalesce,
    occurrence.overlap_policy,
    occurrence.maximum_attempts,
    occurrence.initial_backoff_ms,
    occurrence.maximum_backoff_ms,
    occurrence.dead_letter_at,
    occurrence.state,
    occurrence.version,
    occurrence.attempt,
    coalesce(occurrence.claimant_workload_id, ''),
    coalesce(occurrence.authority_generation, 0),
    coalesce(occurrence.token_hash, ''),
    coalesce(occurrence.claim_key_sha256, ''),
    coalesce(occurrence.lease_expires_at, 'epoch'::timestamptz),
    occurrence.available_at,
    coalesce(occurrence.outcome, ''),
    coalesce(occurrence.result_artifact_id::text, ''),
    coalesce(occurrence.recovery_evidence_sha256, ''),
    coalesce(occurrence.recovery_blocked_at, 'epoch'::timestamptz),
    coalesce(occurrence.execution_session_id::text, ''),
    coalesce(occurrence.execution_session_version, 0),
    coalesce(occurrence.execution_turn_id::text, ''),
    coalesce(occurrence.execution_turn_version, 0),
    coalesce(occurrence.execution_process_run_id::text, ''),
    coalesce(occurrence.execution_process_version, 0),
    coalesce(occurrence.execution_runtime_revision_id::text, ''),
    coalesce(occurrence.execution_runtime_revision_version, 0),
    occurrence.claimed_at,
    occurrence.updated_at
FROM control_plane.schedule_occurrences AS occurrence
JOIN candidate ON candidate.id = occurrence.id
FOR UPDATE OF occurrence
