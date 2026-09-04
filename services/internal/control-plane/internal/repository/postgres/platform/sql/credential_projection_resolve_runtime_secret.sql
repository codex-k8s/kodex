-- name: credential_projection_resolve_runtime_secret :one
SELECT secret.ref, revision.revision, revision.namespace, revision.secret_name,
       revision.secret_key, revision.secret_uid, revision.secret_resource_version,
       revision.content_sha256
FROM control_plane.runtime_secrets secret
JOIN control_plane.runtime_secret_revisions revision
  ON revision.secret_id = secret.id AND revision.revision = secret.current_revision
WHERE secret.organization_id = @organization_id::uuid
  AND secret.project_id = @project_id::uuid
  AND secret.state = 'ACTIVE'
  AND revision.secret_name = @secret_name
  AND revision.secret_key = @secret_key
  AND revision.secret_uid = @secret_uid
  AND revision.secret_resource_version = @secret_resource_version
  AND revision.content_sha256 = @content_sha256;
