-- name: prompt_assistant_core :one
SELECT CASE
           WHEN agent.system_key = 'system-assistant' AND agent.project_id IS NULL THEN 'SYSTEM'
           WHEN profile.id IS NOT NULL THEN 'PROJECT'
           ELSE ''
       END,
       COALESCE(runtime.core_prompt_ref, ''), COALESCE(runtime.core_prompt_revision, ''),
       COALESCE(instruction.digest, ''), COALESCE(instruction.content, ''),
       active_instruction.instruction_id IS NOT NULL
FROM control_plane.agents agent
LEFT JOIN control_plane.projects project
  ON project.id = agent.project_id AND project.organization_id = agent.organization_id
LEFT JOIN control_plane.project_assistant_profiles profile
  ON profile.agent_id = agent.id AND profile.organization_id = agent.organization_id
 AND profile.project_id = agent.project_id
LEFT JOIN control_plane.assistant_runtime runtime
  ON runtime.organization_id = agent.organization_id AND runtime.stable_key = 'system-assistant'
 AND ((agent.system_key = 'system-assistant' AND agent.project_id IS NULL) OR profile.id IS NOT NULL)
LEFT JOIN control_plane.agents system_agent
  ON system_agent.id = runtime.agent_id AND system_agent.organization_id = runtime.organization_id
 AND system_agent.system_key = 'system-assistant' AND system_agent.project_id IS NULL
LEFT JOIN control_plane.instruction_versions instruction
  ON instruction.ref = runtime.core_prompt_ref AND instruction.agent_id = system_agent.id
 AND instruction.organization_id = runtime.organization_id
 AND instruction.core AND instruction.state = 'PUBLISHED'
LEFT JOIN control_plane.agent_instruction_bindings active_instruction
  ON active_instruction.agent_id = system_agent.id
 AND active_instruction.organization_id = runtime.organization_id
 AND active_instruction.instruction_id = instruction.id
WHERE agent.organization_id = @organization_id::uuid AND agent.ref = @agent_ref
  AND COALESCE(project.ref, '') = @project_ref
  AND agent.enabled AND agent.state <> 'ARCHIVED';
