-- name: runtime_retry_authority :one
SELECT run.target_type,run.target_ref,project.ref
FROM control_plane.runs run
JOIN control_plane.projects project ON project.id=run.project_id AND project.organization_id=run.organization_id
WHERE run.organization_id=@organization_id::uuid AND run.ref=@run_ref;
