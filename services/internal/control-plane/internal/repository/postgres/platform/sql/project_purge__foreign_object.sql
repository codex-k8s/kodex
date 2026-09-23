-- name: project_purge__foreign_object :one
SELECT EXISTS (
    SELECT 1 FROM control_plane.artifact_content content
    JOIN control_plane.artifacts artifact ON artifact.id=content.artifact_id
    WHERE content.object_key=@object_key
      AND (artifact.organization_id<>@organization_id::uuid OR artifact.project_id IS DISTINCT FROM @project_id::uuid)
    UNION ALL
    SELECT 1 FROM control_plane.agent_avatar_upload_reservations reservation
    WHERE reservation.object_key=@object_key
      AND (reservation.organization_id<>@organization_id::uuid OR reservation.project_id<>@project_id::uuid)
    UNION ALL
    SELECT 1 FROM control_plane.session_archives archive
    WHERE archive.object_key=@object_key
      AND (archive.organization_id<>@organization_id::uuid OR archive.project_id IS DISTINCT FROM @project_id::uuid)
    UNION ALL
    SELECT 1 FROM control_plane.session_archive_tasks task
    WHERE task.object_key=@object_key
      AND (task.organization_id<>@organization_id::uuid OR task.project_id IS DISTINCT FROM @project_id::uuid)
)
