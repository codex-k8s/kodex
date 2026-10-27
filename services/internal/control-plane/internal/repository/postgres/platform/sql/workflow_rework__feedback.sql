-- name: workflow_rework__feedback :one
SELECT gate.ref,gate.version,gate.decision,gate.decision_comment,COALESCE(manager.progress_summary,'')
FROM control_plane.runs root
JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
CROSS JOIN LATERAL jsonb_array_elements(root.workflow_exception_history) historical(value)
JOIN control_plane.owner_gates gate ON gate.id=@gate_id::uuid AND gate.ref=@gate_ref
    AND gate.organization_id=root.organization_id AND gate.root_run_id=root.id AND gate.project_id=root.project_id
    AND gate.integration_invocation_id IS NULL AND gate.state='APPROVED' AND gate.decision='APPROVE'
JOIN control_plane.run_nodes gate_node ON gate_node.id=gate.node_id AND gate_node.organization_id=root.organization_id
    AND gate_node.root_run_id=root.id AND gate_node.parent_node_id::text=COALESCE(NULLIF(historical.value->>'managerNodeId',''),historical.value->>'finalNodeId')
LEFT JOIN control_plane.run_nodes manager ON manager.id::text=historical.value->>'managerNodeId'
    AND manager.organization_id=root.organization_id AND manager.root_run_id=root.id
    AND manager.ref=historical.value->>'managerNodeRef'
    AND manager.type='AGENT_EXECUTION' AND manager.state IN ('SUCCEEDED','FAILED')
    AND manager.workflow_business_attempt=0 AND manager.workflow_step_key='workflow.coordinator.exception.'||@episode::bigint
    AND EXISTS (SELECT 1 FROM control_plane.agents agent WHERE agent.id=manager.agent_id
        AND agent.organization_id=root.organization_id AND agent.project_id=root.project_id
        AND agent.ref=version.spec->>'CoordinatorAgentRef')
    AND EXISTS (SELECT 1 FROM control_plane.runtime_revisions revision
        JOIN control_plane.runtime_leases lease ON lease.runtime_revision_id=revision.id AND lease.organization_id=revision.organization_id
            AND lease.node_id=manager.id AND lease.run_id=manager.run_id AND lease.generation=revision.generation AND lease.state='COMPLETED'
        WHERE revision.organization_id=manager.organization_id AND revision.root_run_id=root.id
            AND revision.node_id=manager.id AND revision.run_id=manager.run_id AND revision.turn_id=manager.turn_id
            AND revision.agent_id=manager.agent_id AND revision.attempt=manager.attempt)
WHERE root.organization_id=@organization_id::uuid AND root.id=@root_run_id::uuid AND root.id=root.root_run_id
    AND root.state IN ('RUNNING','WAITING_HUMAN') AND version.spec->>'ExecutionMode'='SERVER_DAG_V1'
    AND historical.value->>'state'='RECOVERED'
    AND historical.value->>'kind'='UPSTREAM_REWORK' AND version.spec->>'ReworkPolicy'='OWNER_APPROVED_UPSTREAM_V1'
    AND historical.value->'episode'=to_jsonb(@episode::bigint)
    AND historical.value->'version'=to_jsonb(@episode_version::bigint)
    AND historical.value->>'gateId'=gate.id::text AND historical.value->>'gateRef'=gate.ref
    AND (COALESCE(historical.value->>'managerNodeId','')='' OR manager.id IS NOT NULL);
