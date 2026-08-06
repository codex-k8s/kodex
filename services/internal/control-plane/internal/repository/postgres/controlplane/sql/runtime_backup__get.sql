-- name: RuntimeBackupGet :one
WITH read_clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now
), eligible AS (
    SELECT candidate.id,
           row_number() OVER (
               PARTITION BY candidate.session_id
               ORDER BY candidate.cleanup_consumed_at DESC, candidate.id DESC
           ) AS position
    FROM control_plane.runtime_executions AS candidate
    CROSS JOIN read_clock
    WHERE candidate.organization_id = @organization_id::uuid
      AND candidate.project_id = @project_id::uuid
      AND candidate.state IN ('FAILED', 'EXPIRED')
      AND candidate.cleanup_authorization_state = 'CONSUMED'
      AND candidate.archive_reference IS NOT NULL
      AND candidate.archive_sha256 IS NOT NULL
      AND candidate.archive_object_key IS NOT NULL
      AND candidate.archive_version_id IS NOT NULL
      AND candidate.archive_kms_key_arn IS NOT NULL
      AND candidate.archive_object_lock_mode = 'COMPLIANCE'
      AND candidate.archive_provenance_sha256 IS NOT NULL
      AND candidate.restore_proof_sha256 IS NOT NULL
      AND candidate.cleanup_deletion_proof_sha256 IS NOT NULL
      AND candidate.archive_retain_until > read_clock.now
      AND NOT EXISTS (
          SELECT 1
          FROM control_plane.runtime_executions AS target
          WHERE target.organization_id = candidate.organization_id
            AND target.project_id = candidate.project_id
            AND target.restore_source_execution_id = candidate.id
            AND (target.restore_assignment_state = 'CONSUMED'
                 OR target.state NOT IN ('RETRIED', 'CANCELLED', 'EXPIRED'))
      )
),
session_freshness AS (
    SELECT candidate.session_id,
           max(greatest(
               candidate.updated_at + candidate.version * interval '1 microsecond',
               coalesce(restore.updated_at + restore.generation * interval '1 microsecond', candidate.updated_at),
               coalesce(target.updated_at + target.version * interval '1 microsecond', candidate.updated_at),
               coalesce(target_turn.updated_at + target_turn.version * interval '1 microsecond', candidate.updated_at)
           )) AS base_updated_at,
           coalesce(max(candidate.archive_retain_until) FILTER (
               WHERE candidate.archive_retain_until <= read_clock.now
           ), 'epoch'::timestamptz) AS latest_expired_at,
           count(*) AS archive_count,
           count(*) FILTER (
               WHERE candidate.archive_retain_until <= read_clock.now
           ) AS expired_count
    FROM control_plane.runtime_executions AS candidate
    CROSS JOIN read_clock
    LEFT JOIN control_plane.runtime_restore_operations AS restore
      ON restore.organization_id = candidate.organization_id
     AND restore.project_id = candidate.project_id
     AND restore.backup_execution_id = candidate.id
    LEFT JOIN control_plane.runtime_executions AS target
      ON target.organization_id = restore.organization_id
     AND target.project_id = restore.project_id
     AND target.id = restore.target_execution_id
    LEFT JOIN control_plane.resources AS target_turn
      ON target_turn.organization_id = restore.organization_id
     AND target_turn.project_id = restore.project_id
     AND target_turn.id = restore.target_turn_id
    WHERE candidate.organization_id = @organization_id::uuid
      AND candidate.project_id = @project_id::uuid
      AND candidate.archive_reference IS NOT NULL
      AND candidate.archive_sha256 IS NOT NULL
    GROUP BY candidate.session_id
)
SELECT execution.id::text, execution.organization_id::text,
       execution.project_id::text, execution.session_id::text,
       coalesce(operation.source_version, execution.version),
       coalesce(operation.source_fence, execution.fence),
       session_freshness.base_updated_at,
       session_freshness.latest_expired_at,
       session_freshness.archive_count,
       session_freshness.expired_count,
       execution.runtime_revision_sha256, execution.immutable_input_sha256,
       execution.archive_sha256, coalesce(execution.archive_provenance_sha256, ''),
       execution.state,
       CASE
         WHEN operation.id IS NOT NULL AND target.state = 'SUCCEEDED' THEN 'RESTORED'
         WHEN operation.id IS NOT NULL
              AND coalesce(target.state, target_turn.state) = 'FAILED' THEN 'RESTORE_FAILED'
         WHEN operation.id IS NOT NULL
              AND coalesce(target.state, target_turn.state) = 'CANCELLED' THEN 'RESTORE_CANCELLED'
         WHEN operation.id IS NOT NULL
              AND coalesce(target.state, target_turn.state) = 'EXPIRED' THEN 'RESTORE_EXPIRED'
         WHEN operation.id IS NOT NULL THEN 'RESTORING'
         WHEN execution.archive_retain_until <= read_clock.now THEN 'EXPIRED'
         WHEN execution.restore_proof_sha256 IS NULL THEN 'VERIFYING'
         WHEN execution.cleanup_authorization_state <> 'CONSUMED' THEN 'RETENTION_PENDING'
         WHEN eligible.id IS NOT NULL AND eligible.position = 1 THEN 'AVAILABLE'
         ELSE 'UNAVAILABLE'
       END,
       (eligible.id IS NOT NULL AND eligible.position = 1 AND operation.id IS NULL),
       coalesce(operation.id::text, ''),
       execution.created_at,
       coalesce(execution.cleanup_consumed_at, 'epoch'::timestamptz),
       coalesce(execution.archive_retain_until, 'epoch'::timestamptz)
FROM control_plane.runtime_executions AS execution
CROSS JOIN read_clock
JOIN control_plane.resources AS turn
  ON turn.organization_id = execution.organization_id
 AND turn.project_id = execution.project_id
 AND turn.id = execution.turn_id
 AND turn.kind = 'TURN'
 AND turn.owner_actor_id = @actor_id::uuid
LEFT JOIN eligible
  ON eligible.id = execution.id
JOIN session_freshness
  ON session_freshness.session_id = execution.session_id
LEFT JOIN control_plane.runtime_restore_operations AS operation
  ON operation.organization_id = execution.organization_id
 AND operation.project_id = execution.project_id
 AND operation.backup_execution_id = execution.id
LEFT JOIN control_plane.runtime_executions AS target
  ON target.organization_id = operation.organization_id
 AND target.project_id = operation.project_id
 AND target.id = operation.target_execution_id
LEFT JOIN control_plane.resources AS target_turn
  ON target_turn.organization_id = operation.organization_id
 AND target_turn.project_id = operation.project_id
 AND target_turn.id = operation.target_turn_id
WHERE execution.organization_id = @organization_id::uuid
  AND execution.project_id = @project_id::uuid
  AND execution.archive_reference IS NOT NULL
  AND execution.archive_sha256 IS NOT NULL
  AND execution.id = @backup_id::uuid;
