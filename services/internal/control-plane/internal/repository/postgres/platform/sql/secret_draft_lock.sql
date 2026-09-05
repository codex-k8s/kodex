-- name: secret_draft_lock :one
SELECT d.id::text, d.ref, d.version, d.generation, p.ref, s.ref, s.name, s.description, s.value_type,
d.state, d.published_revision, d.created_at, d.updated_at, d.expires_at,
d.owner_actor_id::text, d.expected_content_sha256, d.encrypted_descriptor,
s.id::text, s.project_id::text, s.namespace, d.staged_namespace, s.state
FROM control_plane.runtime_secret_drafts d
JOIN control_plane.runtime_secrets s ON s.id=d.secret_id
JOIN control_plane.projects p ON p.id=s.project_id
WHERE d.organization_id=@organization_id::uuid AND d.ref=@draft_ref
FOR UPDATE OF s,d;
