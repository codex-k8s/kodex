-- name: runtime_delegateexecution_select_run_nodes_id :one
SELECT 'platform.run.delegate' = ANY(parent_agent.capabilities) AS capability_allowed,
       CASE
           WHEN root.workflow_version_id IS NULL THEN @workflow_step_key = ''
           ELSE workflow_version.spec ->> 'CoordinatorAgentRef' = parent_agent.ref
                AND matching_step.value IS NOT NULL
                AND NOT EXISTS (
                    SELECT 1
                    FROM control_plane.run_nodes delegated
                    WHERE delegated.root_run_id = root.id
                      AND delegated.workflow_step_key = @workflow_step_key
                )
       END AS relationship_allowed,
       COALESCE(matching_step.value ->> 'Instructions', ''),
       COALESCE(matching_step.value ->> 'Name', '')
FROM control_plane.run_nodes parent_node
JOIN control_plane.agents parent_agent ON parent_agent.id = parent_node.agent_id
JOIN control_plane.runs root ON root.id = parent_node.root_run_id
LEFT JOIN control_plane.workflow_versions workflow_version
  ON workflow_version.id = root.workflow_version_id
LEFT JOIN LATERAL (
    SELECT step.value
    FROM jsonb_array_elements(COALESCE(workflow_version.spec -> 'Steps', '[]'::jsonb)) step(value)
    WHERE step.value ->> 'Key' = @workflow_step_key
      AND step.value ->> 'AgentRef' = @target_agent_ref
    LIMIT 1
) matching_step ON true
WHERE parent_node.id = @parent_node_id::uuid
FOR UPDATE OF parent_node
