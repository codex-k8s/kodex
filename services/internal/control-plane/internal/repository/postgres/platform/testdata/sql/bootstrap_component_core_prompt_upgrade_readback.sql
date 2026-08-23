-- name: bootstrap_component_core_prompt_upgrade_readback :one
SELECT runtime.core_prompt_revision,
       runtime.runtime_state,
       runtime.desired_runtime_revision,
       instruction.content,
       instruction.version_number,
       (SELECT count(*)
        FROM control_plane.instruction_versions
        WHERE agent_id = runtime.agent_id
          AND core
          AND state = 'PUBLISHED') AS prompt_count,
       (SELECT count(*)
        FROM control_plane.audit_events
        WHERE organization_id = runtime.organization_id
          AND action = 'system_assistant.core_prompt_upgraded') AS audit_count
FROM control_plane.assistant_runtime runtime
JOIN control_plane.instruction_versions instruction
  ON instruction.ref = runtime.core_prompt_ref
WHERE runtime.stable_key = 'system-assistant';
