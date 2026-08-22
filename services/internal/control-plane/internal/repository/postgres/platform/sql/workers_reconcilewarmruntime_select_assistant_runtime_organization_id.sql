-- name: platform__workers_reconcilewarmruntime_select_assistant_runtime_organization_id :one
SELECT a.ref,
       ar.stable_key,
       a.name,
       a.purpose,
       ar.core_prompt_revision,
       ar.owner_instructions,
       ar.runtime_state,
       ar.runtime_revision,
       ar.desired_runtime_revision,
       ar.system_session_ref,
       ar.resource_limits,
       ar.last_heartbeat_at,
       ar.version,
       ar.updated_at,
       instruction.ref,
       instruction.digest,
       instruction.content,
       COALESCE(ar.warm_instance_ref, ''),
       a.runtime_key,
       profile.runtime_revision,
       profile.provider,
       profile.model,
       role_definition.ref
FROM control_plane.assistant_runtime ar
JOIN control_plane.agents a ON a.id = ar.agent_id
JOIN control_plane.role_definitions role_definition ON role_definition.id = a.role_definition_id
JOIN control_plane.instruction_versions instruction ON instruction.ref = ar.core_prompt_ref
JOIN control_plane.runtime_profiles profile ON profile.stable_key = a.runtime_key
WHERE ar.organization_id = $1::uuid
FOR UPDATE
