-- name: assistant_retry__source :one
SELECT conversation.ref, source.content, COALESCE(attachment.ref, ''),
       run.assistant_context_route, run.assistant_context_entity_kind, run.assistant_context_entity_ref
FROM control_plane.runs run
JOIN control_plane.assistant_conversations conversation
  ON conversation.session_id = run.session_id AND conversation.organization_id = run.organization_id
 AND conversation.created_by = @actor_id::uuid AND conversation.state = 'ACTIVE'
JOIN control_plane.agents agent ON agent.id = conversation.assistant_agent_id AND agent.organization_id = run.organization_id
JOIN control_plane.session_turns source
  ON source.session_id = run.session_id AND source.run_id = run.id AND source.actor_kind = 'USER'
LEFT JOIN control_plane.attachment_sets attachment ON attachment.id = source.attachment_set_id
WHERE run.organization_id = @organization_id::uuid AND run.id = @run_id::uuid
  AND run.id = run.root_run_id AND run.initiated_by = @actor_id::uuid
  AND run.target_type = 'SYSTEM_ASSISTANT' AND run.target_ref = agent.ref
  AND run.state IN ('FAILED', 'CANCELLED')
ORDER BY source.turn_number LIMIT 1;
