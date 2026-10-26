-- name: prompt_workflow_execution_context :one
SELECT root.ref, run.ref, node.ref, session.ref, turn.ref, agent.ref,
       root.target_ref, version.ref, version.version_number, version.digest, node.workflow_step_key, root.attempt, node.attempt,
       COALESCE(previous.ref, ''),
       EXISTS (
           SELECT 1 FROM control_plane.run_edges edge
           WHERE edge.organization_id = node.organization_id
             AND edge.target_node_id = node.id AND edge.type = 'CONTINUES'
       ),
       NOT EXISTS (
           SELECT 1 FROM control_plane.run_edges edge
           LEFT JOIN control_plane.run_nodes origin ON origin.id = edge.source_node_id
           WHERE edge.organization_id = node.organization_id
             AND edge.target_node_id = node.id AND edge.type = 'CONTINUES'
             AND (edge.root_run_id IS DISTINCT FROM root.id
                  OR origin.organization_id IS DISTINCT FROM node.organization_id
                  OR origin.root_run_id IS DISTINCT FROM root.id
                  OR origin.run_id IS DISTINCT FROM node.run_id
                  OR origin.agent_id IS DISTINCT FROM node.agent_id)
       ),
       COALESCE((
           SELECT jsonb_agg(jsonb_build_object(
               'stepKey', step.value->>'Key', 'nodeRef', current_step.ref,
               'state', current_step.state, 'materializationState', current_step.materialization_state,
               'resultGateRef', COALESCE(result_gate.ref, ''),
               'resultGateState', COALESCE(result_gate.state, ''),
               'resultGateDecision', COALESCE(result_gate.decision, ''),
               'ready', current_step.materialization_state = 'PLANNED'
                        AND current_step.state = 'PLANNED'
                        AND NOT EXISTS (
                            SELECT 1 FROM control_plane.run_edges edge
                            LEFT JOIN control_plane.run_nodes dependency ON dependency.id = edge.source_node_id
                            WHERE edge.target_node_id = current_step.id AND edge.type = 'WAITING_FOR'
                              AND (dependency.root_run_id IS DISTINCT FROM root.id
                                   OR dependency.state IS DISTINCT FROM 'SUCCEEDED')
                        )
           ) ORDER BY step.position)
           FROM jsonb_array_elements(version.spec->'Steps') WITH ORDINALITY step(value, position)
           LEFT JOIN control_plane.run_nodes current_step
             ON current_step.organization_id = node.organization_id
            AND current_step.root_run_id = root.id
            AND current_step.workflow_step_key = step.value->>'Key'
            AND (COALESCE(version.spec->>'ExecutionMode','')<>'SERVER_DAG_V1' OR NOT EXISTS (
                SELECT 1 FROM control_plane.run_nodes newer
                WHERE newer.organization_id=current_step.organization_id AND newer.root_run_id=current_step.root_run_id
                  AND COALESCE(newer.workflow_step_origin_node_id,newer.id)=COALESCE(current_step.workflow_step_origin_node_id,current_step.id)
                  AND (newer.workflow_business_attempt,newer.attempt)>(current_step.workflow_business_attempt,current_step.attempt)
            ))
           LEFT JOIN LATERAL (
               SELECT gate.ref, gate.state, gate.decision
               FROM control_plane.owner_gates gate
               JOIN control_plane.run_nodes gate_node
                 ON gate_node.id = gate.node_id AND gate_node.organization_id = gate.organization_id
                AND gate_node.root_run_id = root.id AND gate_node.type = 'HUMAN_GATE'
               WHERE gate.organization_id = node.organization_id
                 AND gate.root_run_id = root.id AND gate_node.parent_node_id = current_step.id
                 AND gate.integration_invocation_id IS NULL
               ORDER BY gate.created_at DESC, gate.ref DESC LIMIT 1
           ) result_gate ON true
       ), '[]'::jsonb),
       CASE WHEN version.spec->>'ExecutionMode'='SERVER_DAG_V1' THEN 'SERVER_DAG_V1' ELSE '' END,
       root.id::text
FROM control_plane.run_nodes node
JOIN control_plane.runs run ON run.id = node.run_id AND run.organization_id = node.organization_id
JOIN control_plane.runs root ON root.id = node.root_run_id AND run.root_run_id = root.id
  AND root.organization_id = node.organization_id
JOIN control_plane.sessions session ON session.id = run.session_id AND session.organization_id = node.organization_id
JOIN control_plane.session_turns turn ON turn.id = node.turn_id AND turn.organization_id = node.organization_id
  AND turn.run_id = run.id AND turn.session_id = session.id
JOIN control_plane.agents agent ON agent.id = node.agent_id AND agent.organization_id = node.organization_id
JOIN control_plane.workflow_versions version ON version.id = root.workflow_version_id
  AND version.organization_id = node.organization_id
LEFT JOIN control_plane.runs previous ON previous.id = root.retry_of_run_id
  AND previous.organization_id = node.organization_id
WHERE node.organization_id = @organization_id::uuid AND node.ref = @node_ref
  AND root.target_type = 'WORKFLOW'
  AND (root.retry_of_run_id IS NULL OR (previous.id IS NOT NULL
       AND previous.target_type = 'WORKFLOW' AND previous.target_ref = root.target_ref
       AND previous.project_id = root.project_id));
