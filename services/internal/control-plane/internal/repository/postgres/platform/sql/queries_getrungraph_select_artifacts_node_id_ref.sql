-- name: queries_getrungraph_select_artifacts_node_id_ref :many
SELECT n.ref,run.ref,COALESCE(parent.ref,''),n.type,n.state,n.display_name,n.role,COALESCE(a.ref,''),COALESCE(t.ref,''),n.attempt,n.input_summary,n.progress_summary,n.integration_names,n.callback_summary,n.safe_error_code,n.safe_error_message,n.next_actions,n.materialization_state,n.created_at,n.started_at,n.finished_at,
		COALESCE((SELECT array_agg(ar.ref ORDER BY ar.created_at) FROM control_plane.artifacts ar WHERE ar.node_id=n.id),'{}'),
        COALESCE((
           SELECT array_agg(child.ref ORDER BY child.created_at, child.ref)
           FROM control_plane.runs child
           WHERE n.materialization_state = 'MATERIALIZED'
             AND child.organization_id = n.organization_id
             AND child.parent_run_id = n.run_id
             AND (
                 (n.type = 'AGENT_EXECUTION'
                  AND child.root_run_id = n.root_run_id
                  AND EXISTS (
                      SELECT 1
                      FROM control_plane.run_edges delegation
                      JOIN control_plane.run_nodes target
                        ON target.id = delegation.target_node_id
                       AND target.organization_id = n.organization_id
                       AND target.root_run_id = n.root_run_id
                       AND target.run_id = child.id
                       AND target.materialization_state = 'MATERIALIZED'
                      JOIN control_plane.run_edges callback
                        ON callback.organization_id = n.organization_id
                       AND callback.root_run_id = n.root_run_id
                       AND callback.source_node_id = target.id
                       AND callback.target_node_id = n.id
                       AND callback.type = 'CALLBACK_TO'
                      WHERE delegation.organization_id = n.organization_id
                        AND delegation.root_run_id = n.root_run_id
                        AND delegation.source_node_id = n.id
                        AND delegation.type = 'DELEGATED_TO'
                  ))
                 OR
                 (n.type IN ('AGENT_EXECUTION', 'EXTERNAL_ACTION')
                  AND child.root_run_id = child.id
                  AND EXISTS (
                      SELECT 1
                      FROM control_plane.required_workflow_launches launch
                      JOIN control_plane.run_nodes origin
                        ON origin.id = launch.origin_node_id
                       AND origin.organization_id = n.organization_id
                       AND origin.root_run_id = n.root_run_id
                       AND origin.run_id = launch.origin_run_id
                      JOIN control_plane.run_nodes proxy
                        ON proxy.id = launch.proxy_node_id
                       AND proxy.organization_id = n.organization_id
                       AND proxy.root_run_id = n.root_run_id
                       AND proxy.run_id = launch.origin_run_id
                       AND proxy.parent_node_id = origin.id
                       AND proxy.type = 'EXTERNAL_ACTION'
                      JOIN control_plane.run_edges callback
                        ON callback.id = launch.callback_edge_id
                       AND callback.organization_id = n.organization_id
                       AND callback.root_run_id = n.root_run_id
                       AND callback.source_node_id = proxy.id
                       AND callback.target_node_id = origin.id
                       AND callback.type = 'CALLBACK_TO'
                      JOIN control_plane.run_edges delegation
                        ON delegation.organization_id = n.organization_id
                       AND delegation.root_run_id = n.root_run_id
                       AND delegation.source_node_id = origin.id
                       AND delegation.target_node_id = proxy.id
                       AND delegation.type = 'DELEGATED_TO'
                      WHERE launch.organization_id = n.organization_id
                        AND launch.origin_root_run_id = n.root_run_id
                        AND launch.origin_run_id = child.parent_run_id
                        AND launch.child_root_run_id = child.id
                        AND (launch.origin_node_id = n.id OR launch.proxy_node_id = n.id)
                  ))
             )
        ), '{}'::text[])
		FROM control_plane.run_nodes n JOIN control_plane.runs root ON root.id=n.root_run_id JOIN control_plane.runs run ON run.id=n.run_id LEFT JOIN control_plane.run_nodes parent ON parent.id=n.parent_node_id LEFT JOIN control_plane.agents a ON a.id=n.agent_id LEFT JOIN control_plane.session_turns t ON t.id=n.turn_id WHERE n.organization_id=$1::uuid AND (root.ref=$2 OR EXISTS(SELECT 1 FROM control_plane.run_edges e JOIN control_plane.runs eroot ON eroot.id=e.root_run_id WHERE eroot.ref=$2 AND (e.source_node_id=n.id OR e.target_node_id=n.id))) ORDER BY n.created_at
