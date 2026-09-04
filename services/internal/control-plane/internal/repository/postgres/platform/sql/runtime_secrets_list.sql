-- name: runtime_secrets_list :many
SELECT secret.ref, secret.version, project.ref, secret.name, secret.description,
       secret.value_type, secret.state, secret.current_revision,
       secret.display_hint_prefix, secret.display_hint_suffix,
       secret.created_at, secret.updated_at, secret.namespace,
       COALESCE(revision.revision, 0), COALESCE(revision.namespace, ''),
       COALESCE(revision.secret_name, ''), COALESCE(revision.secret_key, ''),
       COALESCE(revision.secret_uid, ''), COALESCE(revision.secret_resource_version, ''),
       COALESCE(revision.content_sha256, '')
FROM control_plane.runtime_secrets secret
JOIN control_plane.projects project ON project.id = secret.project_id
LEFT JOIN control_plane.runtime_secret_revisions revision
  ON revision.secret_id = secret.id AND revision.revision = secret.current_revision
WHERE secret.organization_id = @organization_id::uuid
  AND (@project_ref = '' OR project.ref = @project_ref)
  AND secret.state <> 'PROVISIONING'
  AND (@query = '' OR secret.name ILIKE '%' || @query || '%' OR secret.description ILIKE '%' || @query || '%')
  AND (@cursor_ref = '' OR secret.ref > @cursor_ref)
  AND (@authority_project = '' OR secret.project_id = NULLIF(@authority_project,'')::uuid)
  AND control_plane.catalog_resource_visible(secret.organization_id, @actor_id::uuid, 'secret.view', 'SECRET',
      secret.id, secret.project_id, secret.created_by, jsonb_build_object('PROJECT',secret.project_id::text), statement_timestamp())
ORDER BY secret.ref
LIMIT @page_size;
