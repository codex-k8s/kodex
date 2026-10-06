-- name: runtime_callback_resolve_continuation :one
SELECT parent_node.run_id::text,
       parent_node.root_run_id::text,
       parent_node.agent_id::text,
       parent_node.attempt,
       parent_node.display_name,
       parent_node.role,
       parent_run.session_id::text,
       parent_agent.ref,
       COALESCE(root.workflow_version_id::text, ''),
       parent_node.workflow_step_key,
       parent_node.human_gate_after,
       EXISTS(SELECT 1 FROM control_plane.required_workflow_launches launch WHERE launch.origin_node_id=parent_node.id),
       parent_node.ref
FROM control_plane.run_nodes parent_node
JOIN control_plane.runs parent_run ON parent_run.id = parent_node.run_id
JOIN control_plane.runs root ON root.id = parent_node.root_run_id
JOIN control_plane.agents parent_agent ON parent_agent.id = parent_node.agent_id
LEFT JOIN control_plane.workflow_versions workflow_version
  ON workflow_version.id = root.workflow_version_id
LEFT JOIN LATERAL (
    SELECT count(*) FILTER (
        WHERE step.value ->> 'AgentRef' <> workflow_version.spec ->> 'CoordinatorAgentRef'
          AND NOT EXISTS (
            SELECT 1
            FROM control_plane.run_nodes delegated
            WHERE delegated.root_run_id = root.id
              AND delegated.workflow_step_key = step.value ->> 'Key'
              AND delegated.materialization_state = 'MATERIALIZED'
        )
    ) AS missing_steps
    FROM jsonb_array_elements(COALESCE(workflow_version.spec -> 'Steps', '[]'::jsonb)) step(value)
) workflow_progress ON true
WHERE parent_node.organization_id = @organization_id::uuid
  AND parent_node.id = @parent_node_id::uuid
  AND parent_node.state = 'SUCCEEDED'
  AND NOT EXISTS (
      SELECT 1 FROM control_plane.required_workflow_launches launch
      WHERE launch.origin_node_id=parent_node.id AND launch.state='OPEN'
  )
  AND root.state IN ('RUNNING', 'WAITING_HUMAN')
  AND (
      NOT EXISTS(SELECT 1 FROM control_plane.required_workflow_launches launch WHERE launch.origin_node_id=parent_node.id)
      OR EXISTS(
          SELECT 1 FROM control_plane.required_workflow_launches launch
          JOIN control_plane.callback_receipts receipt ON receipt.callback_edge_id=launch.callback_edge_id AND receipt.child_run_id=launch.child_root_run_id
          JOIN control_plane.runs child ON child.id=launch.child_root_run_id
          JOIN control_plane.session_turns delivered ON delivered.run_id=parent_run.id AND delivered.session_id=parent_run.session_id
             AND delivered.actor_kind='AGENT' AND delivered.actor_ref=child.ref AND delivered.state='COMPLETED'
          JOIN control_plane.session_turns current_turn ON current_turn.id=parent_node.turn_id
          WHERE launch.origin_node_id=parent_node.id AND launch.organization_id=parent_node.organization_id
            AND launch.state<>'OPEN' AND delivered.turn_number>current_turn.turn_number
      )
  )
  AND (
      EXISTS (
      SELECT 1
      FROM control_plane.run_edges callback_edge
      WHERE callback_edge.root_run_id = root.id
        AND callback_edge.target_node_id = parent_node.id
        AND callback_edge.type = 'CALLBACK_TO'
      )
      OR (root.workflow_version_id IS NOT NULL AND workflow_progress.missing_steps > 0)
  )
  AND NOT EXISTS (
      SELECT 1
      FROM control_plane.run_edges callback_edge
      WHERE callback_edge.root_run_id = root.id
        AND callback_edge.target_node_id = parent_node.id
        AND callback_edge.type = 'CALLBACK_TO'
        AND NOT EXISTS (
            SELECT 1
            FROM control_plane.callback_receipts receipt
            WHERE receipt.callback_edge_id = callback_edge.id
        )
  )
  AND (
      root.workflow_version_id IS NULL
      OR workflow_version.spec ->> 'CoordinatorAgentRef' = parent_agent.ref
      OR EXISTS(SELECT 1 FROM control_plane.required_workflow_launches launch
                WHERE launch.origin_node_id=parent_node.id AND launch.origin_root_run_id=root.id
                  AND launch.origin_run_id=parent_run.id AND launch.organization_id=parent_node.organization_id)
  )
  AND NOT EXISTS (
      SELECT 1
      FROM control_plane.run_edges continuation
      WHERE continuation.root_run_id = root.id
        AND continuation.source_node_id = parent_node.id
        AND continuation.type = 'CONTINUES'
  )
FOR UPDATE OF parent_node
