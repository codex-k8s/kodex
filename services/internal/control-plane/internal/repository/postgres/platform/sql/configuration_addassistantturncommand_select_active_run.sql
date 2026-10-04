-- name: configuration_addassistantturncommand_select_active_run :one
SELECT run.ref, run.version
FROM control_plane.assistant_conversations conversation
JOIN control_plane.runs run
  ON run.organization_id = conversation.organization_id
 AND run.session_id = conversation.session_id
JOIN control_plane.run_nodes node
  ON node.root_run_id = run.root_run_id
 AND node.run_id = run.id
 AND node.type = 'AGENT_EXECUTION'
WHERE conversation.organization_id = $1::uuid
  AND conversation.ref = $2
  AND conversation.state = 'ACTIVE'
  AND run.state IN ('QUEUED','RUNNING','WAITING_HUMAN','CANCELLING')
  AND node.state IN ('QUEUED','RUNNING','WAITING')
ORDER BY node.started_at NULLS LAST, node.created_at
LIMIT 1
