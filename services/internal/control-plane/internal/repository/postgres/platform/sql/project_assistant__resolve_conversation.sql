-- name: project_assistant__resolve_conversation :one
SELECT agent.id::text, agent.ref, conversation.assistant_scope,
       COALESCE(profile.ref, ''), COALESCE(project.ref, '')
FROM control_plane.assistant_conversations conversation
JOIN control_plane.agents agent
  ON agent.id = conversation.assistant_agent_id AND agent.organization_id = conversation.organization_id
LEFT JOIN control_plane.project_assistant_profiles profile
  ON profile.id = conversation.assistant_profile_id AND profile.organization_id = conversation.organization_id
 AND profile.project_id = conversation.project_id AND profile.agent_id = agent.id
LEFT JOIN control_plane.projects project
  ON project.id = conversation.project_id AND project.organization_id = conversation.organization_id
WHERE conversation.organization_id = @organization_id::uuid
  AND conversation.ref = @conversation_ref AND conversation.created_by = @actor_id::uuid
  AND (@authority_project = '' OR conversation.project_id = NULLIF(@authority_project, '')::uuid)
  AND ((conversation.assistant_scope = 'SYSTEM' AND agent.project_id IS NULL AND agent.system_key = 'system-assistant')
    OR (conversation.assistant_scope = 'PROJECT' AND profile.id IS NOT NULL
        AND agent.project_id = profile.project_id AND agent.system_key IS NULL))
  AND (conversation.project_id IS NULL OR project.lifecycle = 'ACTIVE');
