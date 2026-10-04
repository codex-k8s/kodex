-- name: runtime_secret_lock_by_name :one
SELECT secret.id::text, secret.ref, secret.version, COALESCE(secret.project_id::text, ''),
       COALESCE(project.ref, ''), secret.name, secret.description, secret.value_type,
       secret.state, secret.current_revision, secret.namespace, secret.created_at, secret.updated_at,
       secret.scope_kind, organization.ref
FROM control_plane.runtime_secrets secret
LEFT JOIN control_plane.projects project ON project.id = secret.project_id
JOIN control_plane.organizations organization ON organization.id = secret.organization_id
WHERE secret.organization_id = @organization_id::uuid
  AND secret.project_id IS NOT DISTINCT FROM NULLIF(@project_id, '')::uuid
  AND secret.name = @name
FOR UPDATE OF secret;
