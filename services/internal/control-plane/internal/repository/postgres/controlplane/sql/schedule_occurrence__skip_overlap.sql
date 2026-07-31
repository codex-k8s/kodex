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
      AND occurrence.overlap_policy IN ('FORBID', 'SKIP')
      AND EXISTS (
          SELECT 1
          FROM control_plane.schedule_occurrences AS active
          WHERE active.schedule_id = occurrence.schedule_id
            AND active.state = 'CLAIMED'
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
FROM skipped
ORDER BY scheduled_for, id
