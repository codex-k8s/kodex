-- name: organization_runtime_secrets_list :many
SELECT secret.ref, secret.version, '', secret.name, secret.description,
       secret.value_type, secret.state, secret.current_revision,
       secret.display_hint_prefix, secret.display_hint_suffix,
       secret.created_at, secret.updated_at, secret.namespace,
       COALESCE(revision.revision, 0), COALESCE(revision.namespace, ''),
       COALESCE(revision.secret_name, ''), COALESCE(revision.secret_key, ''),
       COALESCE(revision.secret_uid, ''), COALESCE(revision.secret_resource_version, ''),
       COALESCE(revision.content_sha256, ''), secret.scope_kind, organization.ref
FROM control_plane.runtime_secrets secret
JOIN control_plane.organizations organization ON organization.id = secret.organization_id
LEFT JOIN control_plane.runtime_secret_revisions revision
  ON revision.secret_id = secret.id AND revision.revision = secret.current_revision
WHERE secret.organization_id = @organization_id::uuid
  AND secret.scope_kind = 'ORGANIZATION' AND secret.project_id IS NULL
  AND secret.state <> 'PROVISIONING'
  AND (@query = '' OR secret.name ILIKE '%' || @query || '%' OR secret.description ILIKE '%' || @query || '%')
  AND (@cursor_ref = '' OR secret.ref > @cursor_ref)
ORDER BY secret.ref
LIMIT @page_size;
