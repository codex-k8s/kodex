-- name: RuntimeRestoreOperationGetByBackup :one
SELECT operation.id::text, operation.organization_id::text,
       operation.project_id::text, operation.owner_actor_id::text,
       operation.backup_execution_id::text, operation.source_version, operation.source_fence,
       operation.archive_sha256, operation.provenance_sha256, operation.source_authority_sha256,
       operation.session_id::text, operation.generation, operation.consumed_generation,
       operation.revoked_generation, operation.target_turn_id::text,
       operation.target_attempt, operation.target_execution_id::text,
       coalesce(target.version, 0), target_turn.version, coalesce(target.state, ''),
       coalesce(target.restore_assignment_state, ''), coalesce(target_turn.state, ''),
       operation.created_at,
       greatest(operation.updated_at,
                coalesce(target.updated_at, operation.updated_at),
                target_turn.updated_at)
FROM control_plane.runtime_restore_operations AS operation
LEFT JOIN control_plane.runtime_executions AS target
  ON target.organization_id = operation.organization_id
 AND target.project_id = operation.project_id
 AND target.id = operation.target_execution_id
JOIN control_plane.resources AS target_turn
  ON target_turn.organization_id = operation.organization_id
 AND target_turn.project_id = operation.project_id
 AND target_turn.id = operation.target_turn_id
WHERE operation.backup_execution_id = @backup_execution_id::uuid;
