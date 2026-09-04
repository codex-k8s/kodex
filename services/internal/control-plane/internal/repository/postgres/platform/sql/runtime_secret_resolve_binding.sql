-- name: runtime_secret_resolve_binding :one
SELECT secret.ref,
       revision.namespace,
       revision.revision,
       revision.secret_name,
       revision.secret_key,
       revision.secret_uid,
       revision.secret_resource_version,
       revision.content_sha256
FROM control_plane.runtime_secrets secret
JOIN control_plane.runtime_secret_revisions revision
  ON revision.secret_id = secret.id
 AND revision.revision = CASE WHEN @revision::bigint=0 THEN secret.current_revision ELSE @revision::bigint END
WHERE secret.organization_id = @organization_id::uuid
  AND secret.project_id = @project_id::uuid
  AND secret.ref = @secret_ref
  AND secret.state = 'ACTIVE'
  AND revision.state='ACTIVE'
FOR SHARE OF secret, revision;
