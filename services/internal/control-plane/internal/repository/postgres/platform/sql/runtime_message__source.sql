-- name: runtime_message__source :one
SELECT CASE WHEN t.actor_kind = 'AGENT' AND EXISTS (
  SELECT 1
  FROM control_plane.run_nodes n
  JOIN control_plane.agents a ON a.id = n.agent_id AND a.organization_id = n.organization_id
  WHERE n.organization_id = t.organization_id AND n.run_id = t.run_id
    AND a.ref = t.actor_ref AND (@node_ref = '' OR n.ref = @node_ref)
    AND (
      n.turn_id = t.id OR EXISTS (
        SELECT 1 FROM control_plane.run_events event
        WHERE event.organization_id = t.organization_id AND event.root_run_id = n.root_run_id
          AND event.node_ref = n.ref AND event.type = 'TURN_QUEUED'
          AND event.actor_kind = 'AGENT' AND event.actor_ref = t.actor_ref
          AND event.safe_delta -> 'Execution' ->> 'TurnRef' = t.ref
          AND event.safe_delta -> 'Execution' ->> 'RunRef' = r.ref
          AND event.safe_delta -> 'Execution' ->> 'SessionRef' = s.ref
          AND event.safe_delta -> 'Execution' ->> 'NodeRef' = n.ref
          AND event.safe_delta -> 'Execution' ->> 'TurnNumber' = t.turn_number::text
      )
    )
    AND (
      EXISTS (
        SELECT 1 FROM control_plane.run_edges edge
        JOIN control_plane.run_nodes parent ON parent.id = edge.source_node_id
        WHERE edge.organization_id = n.organization_id AND edge.root_run_id = n.root_run_id
          AND edge.target_node_id = n.id AND edge.type = 'CONTINUES'
          AND n.parent_node_id = parent.id AND parent.run_id = n.run_id
      ) OR EXISTS (
        SELECT 1 FROM control_plane.required_workflow_launches launch
        JOIN control_plane.session_turns origin ON origin.id = launch.origin_turn_id
        JOIN control_plane.callback_receipts receipt ON receipt.callback_edge_id = launch.callback_edge_id
          AND receipt.child_run_id = launch.child_root_run_id
        WHERE launch.organization_id = n.organization_id AND launch.origin_node_id = n.id
          AND launch.origin_root_run_id = n.root_run_id AND launch.origin_run_id = n.run_id
          AND launch.state <> 'OPEN' AND t.turn_number > origin.turn_number
      )
    )
) THEN 'CALLBACK_CONTINUATION' ELSE 'ORDINARY' END
FROM control_plane.session_turns t
JOIN control_plane.runs r ON r.id = t.run_id AND r.organization_id = t.organization_id
JOIN control_plane.sessions s ON s.id = t.session_id AND s.organization_id = t.organization_id
  AND r.session_id = s.id
WHERE t.organization_id = @organization_id::uuid AND t.ref = @turn_ref
  AND r.ref = @run_ref
