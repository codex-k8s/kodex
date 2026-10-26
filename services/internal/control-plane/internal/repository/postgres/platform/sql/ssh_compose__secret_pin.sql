-- name: ssh_compose__secret_pin :one
SELECT revision.revision
FROM control_plane.runtime_secrets secret
JOIN control_plane.runtime_secret_revisions revision ON revision.secret_id=secret.id AND revision.revision=secret.current_revision
WHERE secret.organization_id=@organization_id::uuid AND secret.project_id=@project_id::uuid
 AND secret.scope_kind='PROJECT' AND secret.ref=@secret_ref AND secret.state='ACTIVE'
 AND secret.purpose='SSH_COMPOSE' AND revision.state='ACTIVE'
FOR SHARE OF secret,revision NOWAIT;
