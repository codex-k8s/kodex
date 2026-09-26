-- name: commands_changeagent_select_agents_ref :one
SELECT p.ref, role.ref, role.name, a.runtime_key, r.name, config.provider, config.model,
       r.runtime_revision, a.capabilities,
       COALESCE((SELECT array_agg(ar.ref ORDER BY b.created_at) FROM control_plane.artifact_bindings b JOIN control_plane.artifacts ar ON ar.id=b.artifact_id WHERE b.target_kind='KNOWLEDGE' AND b.target_ref=a.ref AND ar.scan_state='CLEAN' AND ar.lifecycle_state='ACTIVE'),'{}'),
       COALESCE(avatar.ref, ''), COALESCE(a.avatar_artifact_revision, 0)
FROM control_plane.agents a
JOIN control_plane.projects p ON p.id = a.project_id
JOIN control_plane.role_definitions role ON role.id = a.role_definition_id
JOIN control_plane.runtime_profiles r ON r.stable_key = a.runtime_key
JOIN control_plane.agent_runtime_config_versions config ON config.id = a.current_runtime_config_id
LEFT JOIN control_plane.artifacts avatar ON avatar.id = a.avatar_artifact_id
WHERE a.ref = $1
