-- name: project_trash__lock :one
SELECT id::text, ref, name, purpose, language, lifecycle, version,
       created_at, updated_at, deleted_at, purge_after
FROM control_plane.projects
WHERE organization_id=@organization_id::uuid AND ref=@project_ref
FOR UPDATE
