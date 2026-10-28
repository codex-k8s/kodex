-- name: runtime_secret__lock_exact_admission :one
SELECT revision.revision
FROM control_plane.runtime_secrets secret
JOIN control_plane.runtime_secret_revisions revision ON revision.secret_id=secret.id AND revision.revision=@revision::bigint
WHERE secret.organization_id=@organization_id::uuid AND secret.ref=@secret_ref
  AND secret.scope_kind=@scope_kind AND secret.project_id IS NOT DISTINCT FROM NULLIF(@project_id,'')::uuid
FOR SHARE OF secret, revision NOWAIT;
