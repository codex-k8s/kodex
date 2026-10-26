-- name: workflow_exception_delete :exec
DELETE FROM control_plane.runs root USING control_plane.projects project
WHERE root.ref=@run_ref AND root.id=root.root_run_id AND root.project_id=project.id
    AND root.organization_id=project.organization_id AND project.ref=@project_ref;
