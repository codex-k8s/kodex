-- name: workflow_gate__resume_physical :exec
UPDATE control_plane.runs physical
SET state='RUNNING',version=physical.version+1,updated_at=clock_timestamp()
FROM control_plane.runs root
JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
JOIN control_plane.workflows workflow ON workflow.id=version.workflow_id AND workflow.organization_id=root.organization_id AND workflow.project_id=root.project_id
WHERE root.id=@root_run_id::uuid AND root.organization_id=@organization_id::uuid AND root.root_run_id=root.id
    AND version.spec->>'ExecutionMode'='SERVER_DAG_V1' AND root.state IN ('RUNNING','WAITING_HUMAN')
    AND physical.id<>root.id AND physical.root_run_id=root.id AND physical.organization_id=root.organization_id
    AND physical.project_id=root.project_id AND physical.workflow_version_id=root.workflow_version_id
    AND physical.state='WAITING_HUMAN' AND physical.finished_at IS NULL
    AND EXISTS (SELECT 1 FROM control_plane.run_nodes node
        JOIN control_plane.agents agent ON agent.id=node.agent_id AND agent.organization_id=root.organization_id AND agent.project_id=root.project_id
        WHERE node.organization_id=root.organization_id AND node.root_run_id=root.id AND node.run_id=physical.id
            AND node.type='AGENT_EXECUTION' AND node.materialization_state='MATERIALIZED' AND node.state IN ('QUEUED','RUNNING')
            AND ((node.workflow_business_attempt BETWEEN 1 AND 4 AND EXISTS (
                SELECT 1 FROM jsonb_array_elements(version.spec->'Steps') step(value)
                WHERE step.value->>'Key'=node.workflow_step_key AND step.value->>'AgentRef'=agent.ref
            )) OR (node.workflow_business_attempt=0 AND root.workflow_exception->>'state'='MANAGER_QUEUED'
                AND node.id::text=root.workflow_exception->>'managerNodeId' AND node.ref=root.workflow_exception->>'managerNodeRef'
                AND node.workflow_step_key='workflow.coordinator.exception.'||(root.workflow_exception->>'episode')
                AND agent.ref=version.spec->>'CoordinatorAgentRef')))
    AND NOT EXISTS (SELECT 1 FROM control_plane.owner_gates own_gate
        JOIN control_plane.run_nodes gate_node ON gate_node.id=own_gate.node_id AND gate_node.organization_id=own_gate.organization_id
        WHERE own_gate.organization_id=root.organization_id AND own_gate.root_run_id=root.id AND own_gate.state='OPEN'
            AND gate_node.root_run_id=root.id AND gate_node.run_id=physical.id);
