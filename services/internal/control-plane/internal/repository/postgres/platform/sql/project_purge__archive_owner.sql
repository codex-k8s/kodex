-- name: project_purge__archive_owner :one
SELECT EXISTS (
    SELECT 1
    FROM control_plane.session_archives archive
    JOIN control_plane.sessions session ON session.id=archive.session_id AND session.organization_id=archive.organization_id
    JOIN control_plane.organizations organization ON organization.id=archive.organization_id
    JOIN control_plane.projects project ON project.id=archive.project_id AND project.organization_id=archive.organization_id
    WHERE archive.organization_id=@organization_id::uuid AND archive.project_id=@project_id::uuid
      AND session.project_id=@project_id::uuid AND archive.object_key=@object_key
      AND project.ref=@project_ref AND project.lifecycle='PURGE_PENDING'
      AND (@object_version='' OR archive.object_version=@object_version)
      AND (starts_with(archive.object_key,'session-archive/v1/' || organization.ref || '/' || @project_ref || '/' || session.ref || '/')
        OR starts_with(archive.object_key,'session-archive/v1/' || organization.ref || '/_system/' || session.ref || '/'))
    UNION ALL
    SELECT 1
    FROM control_plane.session_archive_tasks task
    JOIN control_plane.sessions session ON session.id=task.session_id AND session.organization_id=task.organization_id
    JOIN control_plane.organizations organization ON organization.id=task.organization_id
    JOIN control_plane.projects project ON project.id=task.project_id AND project.organization_id=task.organization_id
    WHERE task.organization_id=@organization_id::uuid AND task.project_id=@project_id::uuid
      AND session.project_id=@project_id::uuid AND task.object_key=@object_key
      AND project.ref=@project_ref AND project.lifecycle='PURGE_PENDING'
      AND task.state NOT IN ('READY','CLAIMED')
      AND (@object_version='' OR task.object_version=@object_version)
      AND (starts_with(task.object_key,'session-archive/v1/' || organization.ref || '/' || @project_ref || '/' || session.ref || '/')
        OR starts_with(task.object_key,'session-archive/v1/' || organization.ref || '/_system/' || session.ref || '/'))
);
