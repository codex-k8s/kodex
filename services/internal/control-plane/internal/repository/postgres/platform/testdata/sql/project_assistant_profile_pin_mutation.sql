UPDATE control_plane.assistant_conversations conversation
SET assistant_agent_id = agent.id
FROM control_plane.agents agent
WHERE conversation.ref = $1 AND agent.ref = $2;
