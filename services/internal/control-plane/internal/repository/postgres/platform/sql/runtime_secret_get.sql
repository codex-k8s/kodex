-- name: runtime_secret_get :one
SELECT secret.ref, secret.version, COALESCE(project.ref, ''), secret.name, secret.description,
       secret.value_type, secret.state, secret.current_revision,
       secret.display_hint_prefix, secret.display_hint_suffix,
       secret.created_at, secret.updated_at, secret.namespace,
       COALESCE(revision.revision, 0), COALESCE(revision.namespace, ''),
       COALESCE(revision.secret_name, ''), COALESCE(revision.secret_key, ''),
       COALESCE(revision.secret_uid, ''), COALESCE(revision.secret_resource_version, ''),
       COALESCE(revision.content_sha256, ''), secret.scope_kind, organization.ref
FROM control_plane.runtime_secrets secret
LEFT JOIN control_plane.projects project ON project.id = secret.project_id
JOIN control_plane.organizations organization ON organization.id = secret.organization_id
LEFT JOIN control_plane.runtime_secret_revisions revision
  ON revision.secret_id = secret.id AND revision.revision = secret.current_revision
WHERE secret.organization_id = @organization_id::uuid
  AND secret.ref = @secret_ref
  AND secret.state <> 'PROVISIONING';
