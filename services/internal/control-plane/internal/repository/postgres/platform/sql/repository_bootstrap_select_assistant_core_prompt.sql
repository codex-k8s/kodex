-- name: repository_bootstrap_select_assistant_core_prompt :one
SELECT runtime.organization_id::text,
       runtime.agent_id::text,
       runtime.core_prompt_revision,
       instruction.digest,
       instruction.content
FROM control_plane.assistant_runtime runtime
JOIN control_plane.instruction_versions instruction
  ON instruction.ref = runtime.core_prompt_ref
 AND instruction.agent_id = runtime.agent_id
 AND instruction.core
 AND instruction.state = 'PUBLISHED'
JOIN control_plane.agent_instruction_bindings active_instruction
 ON active_instruction.agent_id=runtime.agent_id AND active_instruction.organization_id=runtime.organization_id
 AND active_instruction.instruction_id=instruction.id
WHERE runtime.stable_key = 'system-assistant'
FOR UPDATE OF runtime;
