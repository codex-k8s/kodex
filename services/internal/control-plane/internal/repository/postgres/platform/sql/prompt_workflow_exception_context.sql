-- name: prompt_workflow_exception_context :one
SELECT EXISTS (
    SELECT 1 FROM control_plane.run_nodes node
    JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=node.organization_id
    JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
    JOIN control_plane.agents agent ON agent.id=node.agent_id AND agent.organization_id=root.organization_id
        AND agent.project_id=root.project_id AND agent.ref=version.spec->>'CoordinatorAgentRef'
    WHERE node.organization_id=@organization_id::uuid AND node.ref=@node_ref
        AND root.id=root.root_run_id AND root.state IN ('RUNNING','WAITING_HUMAN')
        AND version.spec->>'ExecutionMode'='SERVER_DAG_V1'
        AND node.type='AGENT_EXECUTION' AND node.state IN ('QUEUED','RUNNING')
        AND node.workflow_business_attempt=0 AND node.workflow_step_key='workflow.coordinator.exception.'||@episode::bigint
        AND root.workflow_exception_history->-1=root.workflow_exception
        AND root.workflow_exception->>'state'='MANAGER_QUEUED'
        AND root.workflow_exception->'episode'=to_jsonb(@episode::bigint)
        AND root.workflow_exception->>'managerNodeId'=node.id::text
        AND root.workflow_exception->>'managerNodeRef'=node.ref
);
