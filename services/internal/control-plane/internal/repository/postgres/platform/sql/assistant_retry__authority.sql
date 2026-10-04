-- name: assistant_retry__authority :one
SELECT run.target_type, COALESCE(conversation.ref, '')
FROM control_plane.runs run
LEFT JOIN control_plane.assistant_conversations conversation
  ON conversation.session_id = run.session_id AND conversation.organization_id = run.organization_id
 AND conversation.created_by = @actor_id::uuid AND run.initiated_by = @actor_id::uuid
 AND run.id = run.root_run_id AND conversation.state = 'ACTIVE'
 AND (@authority_project = '' OR conversation.project_id = NULLIF(@authority_project, '')::uuid)
LEFT JOIN control_plane.agents agent
  ON agent.id = conversation.assistant_agent_id AND agent.organization_id = conversation.organization_id
WHERE run.organization_id = @organization_id::uuid AND run.ref = @run_ref
  AND (run.target_type <> 'SYSTEM_ASSISTANT' OR run.target_ref = agent.ref);
