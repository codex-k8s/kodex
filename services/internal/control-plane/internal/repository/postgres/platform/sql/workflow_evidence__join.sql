-- name: workflow_evidence__join :one
-- Детерминированный seed относится к одному producer; receipts/outcomes выбирает
-- существующий owner selector целиком. Уже доставленный parent не создаёт новый turn.
SELECT COALESCE(min(invocation.id::text) FILTER (
        WHERE control_plane.workflow_evidence_delivery_operation(version.spec->>'ResultEvidenceProfile',invocation.operation)),''),
    count(*) FILTER (WHERE invocation.state NOT IN ('SUCCEEDED','FAILED','REJECTED','CANCELLED'))
FROM control_plane.run_nodes node
JOIN control_plane.runs physical ON physical.id=node.run_id AND physical.organization_id=node.organization_id
    AND physical.root_run_id=node.root_run_id AND physical.state='RUNNING'
JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=node.organization_id
    AND root.project_id=physical.project_id AND root.workflow_version_id=physical.workflow_version_id
    AND root.state IN ('RUNNING','WAITING_HUMAN')
JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
JOIN control_plane.integration_invocations invocation ON invocation.node_id=node.id AND invocation.organization_id=node.organization_id
    AND invocation.run_id=physical.id
WHERE node.organization_id=@organization_id::uuid AND node.id=@node_id::uuid
    AND node.type='AGENT_EXECUTION' AND node.materialization_state='MATERIALIZED' AND node.state='SUCCEEDED'
    AND node.owner_rework_gate_id IS NULL AND node.workflow_business_attempt BETWEEN 1 AND 4
    AND version.spec->>'ExecutionMode'='SERVER_DAG_V1' AND version.spec->>'ResultEvidenceProfile' IN ('GITHUB_REVIEW_QA_V1','GITHUB_PDLC_V1')
    AND NOT EXISTS (SELECT 1 FROM control_plane.run_edges delivered
        WHERE delivered.organization_id=node.organization_id AND delivered.root_run_id=root.id
            AND delivered.source_node_id=node.id AND delivered.type='CONTINUES');
