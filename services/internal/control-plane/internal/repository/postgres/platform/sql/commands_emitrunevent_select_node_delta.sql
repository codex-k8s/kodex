-- name: commands_emitrunevent_select_node_delta :one
SELECT node.ref,
       run.ref,
       COALESCE(parent.ref, ''),
       node.type,
       node.state,
       node.display_name,
       node.role,
       COALESCE(agent.ref, ''),
       COALESCE(turn.ref, ''),
       node.attempt,
       node.input_summary,
       node.progress_summary,
       node.integration_names,
       node.callback_summary,
       node.safe_error_code,
       node.safe_error_message,
       node.next_actions,
       node.created_at,
       node.started_at,
       node.finished_at,
       COALESCE((
           SELECT array_agg(artifact.ref ORDER BY artifact.created_at)
           FROM control_plane.artifacts artifact
           WHERE artifact.node_id = node.id
       ), '{}'::text[]),
       COALESCE((
           SELECT array_agg(child.ref ORDER BY child.created_at)
           FROM control_plane.runs child
           WHERE child.parent_run_id = node.run_id
       ), '{}'::text[])
FROM control_plane.run_nodes node
JOIN control_plane.runs run ON run.id = node.run_id
LEFT JOIN control_plane.run_nodes parent ON parent.id = node.parent_node_id
LEFT JOIN control_plane.agents agent ON agent.id = node.agent_id
LEFT JOIN control_plane.session_turns turn ON turn.id = node.turn_id
WHERE node.organization_id = $1::uuid
  AND node.ref = $2
