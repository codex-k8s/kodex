-- name: ScheduleOccurrenceNext
WITH candidate AS (
    SELECT occurrence.id
    FROM control_plane.schedule_occurrences AS occurrence
    WHERE occurrence.organization_id = @organization_id::uuid
      AND occurrence.project_id = @project_id::uuid
      AND occurrence.state = 'QUEUED'
      AND occurrence.available_at <= @now
      AND NOT EXISTS (
          SELECT 1
          FROM control_plane.schedule_occurrences AS active
          WHERE active.schedule_id = occurrence.schedule_id
            AND active.state = 'CLAIMED'
      )
      AND NOT EXISTS (
          SELECT 1
          FROM control_plane.schedule_occurrences AS predecessor
          WHERE predecessor.schedule_id = occurrence.schedule_id
            AND (
                predecessor.scheduled_for < occurrence.scheduled_for
                OR (
                    predecessor.scheduled_for = occurrence.scheduled_for
                    AND predecessor.id < occurrence.id
                )
            )
            AND predecessor.state IN ('QUEUED', 'CLAIMED')
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
    occurrence.overlap_policy,
    occurrence.maximum_attempts,
    occurrence.initial_backoff_ms,
    occurrence.maximum_backoff_ms,
    occurrence.dead_letter_at,
    occurrence.state,
    occurrence.attempt,
    coalesce(occurrence.claimant_workload_id, ''),
    coalesce(occurrence.authority_generation, 0),
    coalesce(occurrence.token_hash, ''),
    coalesce(occurrence.lease_expires_at, 'epoch'::timestamptz),
    occurrence.available_at,
    coalesce(occurrence.outcome, ''),
    coalesce(occurrence.result_artifact_id::text, ''),
    occurrence.claimed_at,
    occurrence.updated_at
FROM control_plane.schedule_occurrences AS occurrence
JOIN candidate ON candidate.id = occurrence.id
FOR UPDATE OF occurrence
