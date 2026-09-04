-- name: commands_changeagentbinding_select_agent_readback :one
SELECT a.ref, p.ref, role.ref, role.name, a.name, a.purpose,
       a.role_description, a.avatar_url, COALESCE(avatar.ref, ''),
       COALESCE(a.avatar_artifact_revision, 0), a.state, a.enabled, a.version,
       a.runtime_key, runtime.name, runtime.provider, runtime.model,
       runtime.runtime_revision, a.capabilities,
       COALESCE((
           SELECT array_agg(artifact.ref ORDER BY binding.created_at)
           FROM control_plane.artifact_bindings binding
           JOIN control_plane.artifacts artifact ON artifact.id = binding.artifact_id
           WHERE binding.target_kind = 'KNOWLEDGE'
             AND binding.target_ref = a.ref
             AND artifact.scan_state = 'CLEAN'
             AND artifact.lifecycle_state = 'ACTIVE'
       ), '{}'),
       a.created_at, a.updated_at
FROM control_plane.agents a
JOIN control_plane.projects p ON p.id = a.project_id
JOIN control_plane.role_definitions role ON role.id = a.role_definition_id
JOIN control_plane.runtime_profiles runtime ON runtime.stable_key = a.runtime_key
LEFT JOIN control_plane.artifacts avatar ON avatar.id = a.avatar_artifact_id
WHERE a.organization_id = $1::uuid
  AND a.ref = $2
