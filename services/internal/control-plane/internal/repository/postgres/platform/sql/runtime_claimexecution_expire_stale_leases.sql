-- name: runtime_claimexecution_expire_stale_leases :many
WITH expired AS (
    UPDATE control_plane.runtime_leases
    SET state='EXPIRED',updated_at=clock_timestamp()
    WHERE organization_id=$1::uuid AND state='CLAIMED' AND expires_at<=clock_timestamp()
    RETURNING node_id,run_id,ref
), server_nodes AS MATERIALIZED (
    SELECT n.id,n.run_id,root.id AS root_id
    FROM expired e
    JOIN control_plane.run_nodes n ON n.id=e.node_id AND n.organization_id=$1::uuid
    JOIN control_plane.runs physical ON physical.id=n.run_id AND physical.organization_id=n.organization_id
    JOIN control_plane.runs root ON root.id=n.root_run_id AND root.organization_id=n.organization_id AND root.id=physical.root_run_id
        AND root.project_id=physical.project_id AND physical.workflow_version_id=root.workflow_version_id
    JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
    JOIN control_plane.workflows workflow ON workflow.id=version.workflow_id AND workflow.organization_id=root.organization_id AND workflow.project_id=root.project_id
    JOIN control_plane.agents agent ON agent.id=n.agent_id AND agent.organization_id=root.organization_id AND agent.project_id=root.project_id
    WHERE version.spec->>'ExecutionMode'='SERVER_DAG_V1' AND root.state IN ('QUEUED','RUNNING','WAITING_HUMAN')
        AND n.materialization_state='MATERIALIZED' AND n.type='AGENT_EXECUTION' AND n.state IN ('RUNNING','QUEUED')
        AND ((n.workflow_business_attempt BETWEEN 1 AND 4 AND EXISTS (
            SELECT 1 FROM jsonb_array_elements(version.spec->'Steps') step(value)
            WHERE step.value->>'Key'=n.workflow_step_key AND step.value->>'AgentRef'=agent.ref
        )) OR (n.workflow_business_attempt=0 AND root.workflow_exception->>'state'='MANAGER_QUEUED'
            AND n.id::text=root.workflow_exception->>'managerNodeId' AND n.ref=root.workflow_exception->>'managerNodeRef'
            AND n.workflow_step_key='workflow.coordinator.exception.'||(root.workflow_exception->>'episode')
            AND agent.ref=version.spec->>'CoordinatorAgentRef'))
), requeued AS (
    UPDATE control_plane.run_nodes n
    SET state='QUEUED',started_at=NULL,progress_summary='',version=n.version+1
    FROM expired e,control_plane.runs physical,control_plane.runs root
    WHERE n.id=e.node_id AND physical.id=n.run_id AND root.id=physical.root_run_id
        AND ((physical.state IN ('QUEUED','RUNNING') AND root.state IN ('QUEUED','RUNNING') AND NOT EXISTS (
                SELECT 1 FROM control_plane.workflow_versions server_version
                WHERE server_version.id=root.workflow_version_id AND server_version.spec->>'ExecutionMode'='SERVER_DAG_V1'))
            OR (physical.state IN ('QUEUED','RUNNING','WAITING_HUMAN') AND EXISTS (SELECT 1 FROM server_nodes assigned WHERE assigned.id=n.id))
            OR control_plane.legacy_rework_execution_assigned(n.organization_id,n.id))
    RETURNING n.id,n.run_id
), resumed AS (
    UPDATE control_plane.runs physical
    SET state='RUNNING',version=physical.version+1,updated_at=clock_timestamp()
    FROM requeued queued JOIN server_nodes assigned ON assigned.id=queued.id
    WHERE physical.id=queued.run_id AND physical.id<>assigned.root_id AND physical.organization_id=$1::uuid
        AND physical.state='WAITING_HUMAN' AND physical.finished_at IS NULL
        AND NOT EXISTS (SELECT 1 FROM control_plane.owner_gates own_gate
            JOIN control_plane.run_nodes gate_node ON gate_node.id=own_gate.node_id AND gate_node.organization_id=own_gate.organization_id
            WHERE own_gate.organization_id=physical.organization_id AND own_gate.root_run_id=assigned.root_id AND own_gate.state='OPEN'
                AND gate_node.root_run_id=assigned.root_id AND gate_node.run_id=physical.id)
    RETURNING physical.id
)
SELECT expired.ref,COALESCE(run.project_id::text,''),run.ref
FROM expired JOIN control_plane.runs run ON run.id=expired.run_id
ORDER BY expired.ref;
