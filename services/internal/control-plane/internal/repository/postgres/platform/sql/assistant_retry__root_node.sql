-- name: assistant_retry__root_node :one
SELECT node.id::text
FROM control_plane.run_nodes node
JOIN control_plane.runs run ON run.id=node.run_id AND run.organization_id=node.organization_id
JOIN control_plane.assistant_conversations conversation
  ON conversation.session_id=run.session_id AND conversation.organization_id=run.organization_id
 AND conversation.assistant_agent_id=node.agent_id
WHERE node.organization_id=$1::uuid AND node.root_run_id=$2::uuid
  AND node.run_id=node.root_run_id AND run.target_type='SYSTEM_ASSISTANT'
  AND node.type='AGENT_EXECUTION'
ORDER BY node.created_at, node.id LIMIT 1;
