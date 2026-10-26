-- name: workflow_exception_mutate :exec
UPDATE control_plane.runs root SET workflow_exception_history=@history::jsonb,
    workflow_exception=NULLIF(@active::jsonb,'null'::jsonb)
FROM control_plane.projects project
WHERE root.ref=@run_ref AND root.id=root.root_run_id AND root.project_id=project.id
    AND root.organization_id=project.organization_id AND project.ref=@project_ref;
