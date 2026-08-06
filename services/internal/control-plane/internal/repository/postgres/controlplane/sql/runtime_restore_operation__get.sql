-- name: RuntimeRestoreOperationGet :one
SELECT operation.id::text, operation.organization_id::text,
       operation.project_id::text, operation.owner_actor_id::text,
       operation.backup_execution_id::text, operation.source_version, operation.source_fence,
       operation.archive_sha256, operation.provenance_sha256,
       operation.session_id::text, operation.target_turn_id::text,
       operation.target_attempt, operation.target_execution_id::text,
       coalesce(target.version, 0), coalesce(target.state, ''),
       coalesce(target.restore_assignment_state, ''),
       operation.created_at, coalesce(target.updated_at, operation.updated_at)
FROM control_plane.runtime_restore_operations AS operation
LEFT JOIN control_plane.runtime_executions AS target
  ON target.organization_id = operation.organization_id
 AND target.project_id = operation.project_id
 AND target.id = operation.target_execution_id
WHERE operation.id = @id::uuid;
