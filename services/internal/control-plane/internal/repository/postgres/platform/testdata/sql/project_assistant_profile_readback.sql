SELECT session.target_ref, agent.ref, conversation.assistant_scope
FROM control_plane.assistant_conversations conversation
JOIN control_plane.sessions session ON session.id = conversation.session_id
JOIN control_plane.agents agent ON agent.id = conversation.assistant_agent_id
WHERE conversation.ref = $1;
