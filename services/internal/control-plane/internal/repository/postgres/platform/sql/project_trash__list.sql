-- name: project_trash__list :many
SELECT project.ref, project.name, project.purpose, project.language,
       project.lifecycle, project.version, project.created_at, project.updated_at,
       project.deleted_at, project.purge_after,
       (SELECT count(*)::integer FROM control_plane.agents agent
        WHERE agent.project_id=project.id AND agent.state<>'ARCHIVED'),
       (SELECT count(*)::integer FROM control_plane.workflows workflow
        WHERE workflow.project_id=project.id AND workflow.state<>'ARCHIVED')
FROM control_plane.projects project
WHERE project.organization_id=@organization_id::uuid
  AND project.lifecycle IN ('TRASHED', 'PURGE_PENDING')
  AND (@cursor_at::timestamptz IS NULL OR
       (project.deleted_at, project.ref)<(@cursor_at::timestamptz, @cursor_ref))
ORDER BY project.deleted_at DESC, project.ref DESC
LIMIT @page_size
