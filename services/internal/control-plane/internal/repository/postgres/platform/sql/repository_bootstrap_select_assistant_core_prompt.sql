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
WHERE runtime.stable_key = 'system-assistant'
FOR UPDATE OF runtime;
