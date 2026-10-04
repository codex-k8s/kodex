-- name: run_assistant_pin :one
SELECT conversation.assistant_scope, organization.ref, conversation.ref,
       agent.ref, COALESCE(project.ref, ''), COALESCE(profile.ref, '')
FROM control_plane.runs run
JOIN control_plane.organizations organization ON organization.id = run.organization_id
JOIN control_plane.sessions session
  ON session.id = run.session_id AND session.organization_id = run.organization_id
JOIN control_plane.assistant_conversations conversation
  ON conversation.session_id = session.id AND conversation.organization_id = run.organization_id
 AND conversation.project_id IS NOT DISTINCT FROM run.project_id
JOIN control_plane.agents agent
  ON agent.id = conversation.assistant_agent_id AND agent.organization_id = run.organization_id
 AND agent.ref = run.target_ref AND agent.ref = session.target_ref
LEFT JOIN control_plane.projects project
  ON project.id = run.project_id AND project.organization_id = run.organization_id
LEFT JOIN control_plane.project_assistant_profiles profile
  ON profile.id = conversation.assistant_profile_id AND profile.organization_id = run.organization_id
 AND profile.project_id = run.project_id AND profile.agent_id = agent.id
WHERE run.organization_id = @organization_id::uuid AND run.ref = @run_ref
  AND run.source = 'SYSTEM_ASSISTANT' AND run.target_type = 'SYSTEM_ASSISTANT'
  AND ((conversation.assistant_scope = 'SYSTEM' AND agent.project_id IS NULL
        AND agent.system_key = 'system-assistant' AND conversation.assistant_profile_id IS NULL)
    OR (conversation.assistant_scope = 'PROJECT' AND profile.id IS NOT NULL
        AND agent.project_id = run.project_id AND agent.system_key IS NULL));
