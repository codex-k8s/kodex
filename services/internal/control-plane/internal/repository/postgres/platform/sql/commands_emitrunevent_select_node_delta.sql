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
       node.materialization_state,
       node.created_at,
       node.started_at,
       node.finished_at,
       COALESCE((
           SELECT array_agg(artifact.ref ORDER BY artifact.created_at)
           FROM control_plane.artifacts artifact
           WHERE artifact.node_id = node.id
       ), '{}'::text[]),
       COALESCE((
           SELECT array_agg(child.ref ORDER BY child.created_at, child.ref)
           FROM control_plane.runs child
           WHERE node.materialization_state = 'MATERIALIZED'
             AND child.organization_id = node.organization_id
             AND child.parent_run_id = node.run_id
             AND (
                 (node.type = 'AGENT_EXECUTION'
                  AND child.root_run_id = node.root_run_id
                  AND EXISTS (
                      SELECT 1
                      FROM control_plane.run_edges delegation
                      JOIN control_plane.run_nodes target
                        ON target.id = delegation.target_node_id
                       AND target.organization_id = node.organization_id
                       AND target.root_run_id = node.root_run_id
                       AND target.run_id = child.id
                       AND target.materialization_state = 'MATERIALIZED'
                      JOIN control_plane.run_edges callback
                        ON callback.organization_id = node.organization_id
                       AND callback.root_run_id = node.root_run_id
                       AND callback.source_node_id = target.id
                       AND callback.target_node_id = node.id
                       AND callback.type = 'CALLBACK_TO'
                      WHERE delegation.organization_id = node.organization_id
                        AND delegation.root_run_id = node.root_run_id
                        AND delegation.source_node_id = node.id
                        AND delegation.type = 'DELEGATED_TO'
                  ))
                 OR
                 (node.type IN ('AGENT_EXECUTION', 'EXTERNAL_ACTION')
                  AND child.root_run_id = child.id
                  AND EXISTS (
                      SELECT 1
                      FROM control_plane.required_workflow_launches launch
                      JOIN control_plane.run_nodes origin
                        ON origin.id = launch.origin_node_id
                       AND origin.organization_id = node.organization_id
                       AND origin.root_run_id = node.root_run_id
                       AND origin.run_id = launch.origin_run_id
                      JOIN control_plane.run_nodes proxy
                        ON proxy.id = launch.proxy_node_id
                       AND proxy.organization_id = node.organization_id
                       AND proxy.root_run_id = node.root_run_id
                       AND proxy.run_id = launch.origin_run_id
                       AND proxy.parent_node_id = origin.id
                       AND proxy.type = 'EXTERNAL_ACTION'
                      JOIN control_plane.run_edges callback
                        ON callback.id = launch.callback_edge_id
                       AND callback.organization_id = node.organization_id
                       AND callback.root_run_id = node.root_run_id
                       AND callback.source_node_id = proxy.id
                       AND callback.target_node_id = origin.id
                       AND callback.type = 'CALLBACK_TO'
                      JOIN control_plane.run_edges delegation
                        ON delegation.organization_id = node.organization_id
                       AND delegation.root_run_id = node.root_run_id
                       AND delegation.source_node_id = origin.id
                       AND delegation.target_node_id = proxy.id
                       AND delegation.type = 'DELEGATED_TO'
                      WHERE launch.organization_id = node.organization_id
                        AND launch.origin_root_run_id = node.root_run_id
                        AND launch.origin_run_id = child.parent_run_id
                        AND launch.child_root_run_id = child.id
                        AND (launch.origin_node_id = node.id OR launch.proxy_node_id = node.id)
                  ))
             )
       ), '{}'::text[])
FROM control_plane.run_nodes node
JOIN control_plane.runs run ON run.id = node.run_id
LEFT JOIN control_plane.run_nodes parent ON parent.id = node.parent_node_id
LEFT JOIN control_plane.agents agent ON agent.id = node.agent_id
LEFT JOIN control_plane.session_turns turn ON turn.id = node.turn_id
WHERE node.organization_id = $1::uuid
  AND node.ref = $2
