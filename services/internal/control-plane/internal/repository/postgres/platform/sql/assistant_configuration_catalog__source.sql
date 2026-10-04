-- name: assistant_configuration_catalog__source :one
SELECT conversation.assistant_scope, agent.ref
FROM control_plane.runtime_leases lease
JOIN control_plane.run_nodes node ON node.id=lease.node_id AND node.organization_id=lease.organization_id
JOIN control_plane.agents agent ON agent.id=node.agent_id AND agent.organization_id=lease.organization_id
JOIN control_plane.runs run ON run.id=lease.run_id AND run.organization_id=lease.organization_id
JOIN control_plane.assistant_conversations conversation
  ON conversation.session_id=run.session_id AND conversation.organization_id=run.organization_id
 AND conversation.assistant_agent_id=agent.id
WHERE lease.organization_id=@organization_id::uuid AND lease.ref=@lease_ref;
