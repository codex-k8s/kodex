-- name: RuntimeRestoreOperationInsert :exec
INSERT INTO control_plane.runtime_restore_operations (
    id, organization_id, project_id, owner_actor_id,
    backup_execution_id, source_version, source_fence, archive_sha256, provenance_sha256,
    session_id, target_turn_id, target_attempt, target_execution_id,
    created_at, updated_at
) VALUES (
    @id::uuid, @organization_id::uuid, @project_id::uuid, @owner_actor_id::uuid,
    @backup_execution_id::uuid, @source_version, @source_fence, @archive_sha256, @provenance_sha256,
    @session_id::uuid, @target_turn_id::uuid, @target_attempt, @target_execution_id::uuid,
    @created_at, @updated_at
);
