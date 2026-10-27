-- name: workflow_exception__outstanding :one
SELECT anchor.workflow_step_key,(receipt.value->>'attempt')::integer,
       CASE WHEN receipt.value->>'accepted'='false' THEN receipt.value->>'reason' ELSE 'OWNER_CHANGES_REQUESTED' END,
       anchor.id::text,physical.id::text
FROM control_plane.runs root
JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
JOIN control_plane.workflows workflow ON workflow.id=version.workflow_id AND workflow.organization_id=root.organization_id
    AND workflow.project_id=root.project_id
CROSS JOIN LATERAL jsonb_array_elements(version.spec->'Steps') WITH ORDINALITY published(value,position)
JOIN control_plane.run_nodes anchor ON anchor.organization_id=root.organization_id AND anchor.root_run_id=root.id
    AND anchor.workflow_step_key=published.value->>'Key' AND anchor.workflow_step_origin_node_id IS NULL
    AND anchor.type='AGENT_EXECUTION' AND anchor.workflow_business_attempt=1
CROSS JOIN LATERAL (SELECT anchor.workflow_attempts->-1 AS value) receipt
JOIN control_plane.run_nodes physical ON physical.id::text=receipt.value->'provenance'->>'nodeId'
    AND physical.organization_id=anchor.organization_id AND physical.root_run_id=anchor.root_run_id
    AND physical.workflow_step_key=anchor.workflow_step_key AND physical.agent_id=anchor.agent_id
    AND COALESCE(physical.workflow_step_origin_node_id,physical.id)=anchor.id
    AND physical.workflow_business_attempt=(receipt.value->>'attempt')::integer
    AND physical.state IN ('SUCCEEDED','FAILED')
WHERE root.organization_id=@organization_id::uuid AND root.id=@root_run_id::uuid AND root.id=root.root_run_id
    AND root.state IN ('QUEUED','RUNNING','WAITING_HUMAN') AND root.workflow_exception IS NULL
    AND version.spec->>'ExecutionMode'='SERVER_DAG_V1'
    AND jsonb_array_length(anchor.workflow_attempts)=(receipt.value->>'attempt')::integer
    AND (receipt.value->>'accepted'='false' OR (receipt.value->>'accepted'='true' AND EXISTS (
        SELECT 1 FROM control_plane.owner_gates gate
        JOIN control_plane.run_nodes gate_node ON gate_node.id=gate.node_id AND gate_node.organization_id=gate.organization_id
            AND gate_node.parent_node_id=physical.id
        JOIN control_plane.run_edges edge ON edge.organization_id=gate.organization_id AND edge.root_run_id=gate.root_run_id
            AND edge.type='WAITING_FOR' AND edge.source_node_id=physical.id AND edge.target_node_id=gate.node_id
        WHERE gate.organization_id=root.organization_id AND gate.root_run_id=root.id AND gate.project_id=root.project_id
            AND gate.integration_invocation_id IS NULL AND gate.state='CHANGES_REQUESTED' AND gate.decision='REQUEST_CHANGES'
    )))
    AND NOT EXISTS (SELECT 1 FROM control_plane.run_nodes newer
        WHERE newer.organization_id=anchor.organization_id AND newer.root_run_id=anchor.root_run_id
            AND COALESCE(newer.workflow_step_origin_node_id,newer.id)=anchor.id
            AND (newer.workflow_business_attempt>(receipt.value->>'attempt')::integer
                OR (newer.workflow_business_attempt=(receipt.value->>'attempt')::integer AND newer.state IN ('QUEUED','RUNNING','WAITING'))))
    AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements(root.workflow_exception_history) previous(value)
        WHERE previous.value->>'originNodeId'=anchor.id::text AND previous.value->'attempt'=receipt.value->'attempt'
            AND previous.value->>'finalNodeId'=physical.id::text)
    AND NOT EXISTS (SELECT 1
        FROM jsonb_array_elements(root.workflow_exception_history) covered(value)
        JOIN control_plane.owner_gates approval ON approval.id::text=covered.value->>'gateId'
            AND approval.ref=covered.value->>'gateRef' AND approval.organization_id=root.organization_id
            AND approval.root_run_id=root.id AND approval.project_id=root.project_id
            AND approval.integration_invocation_id IS NULL AND approval.state='APPROVED' AND approval.decision='APPROVE'
        CROSS JOIN LATERAL jsonb_array_elements((covered.value->'reworkPlan'->'targets')||(covered.value->'reworkPlan'->'rechecks')) member(value)
        WHERE version.spec->>'ReworkPolicy'='OWNER_APPROVED_UPSTREAM_V1'
            AND covered.value->>'kind'='UPSTREAM_REWORK' AND covered.value->>'state'='RECOVERED'
            AND covered.value->'reworkPlan'->>'schema'='kodex.workflow-upstream-rework.v1'
            AND COALESCE(covered.value->'reworkPlan'->>'blockReason','')=''
            AND member.value->>'stepKey'=anchor.workflow_step_key AND member.value->>'originNodeId'=anchor.id::text
            AND member.value->'previousAttempt'=receipt.value->'attempt')
ORDER BY published.position,anchor.id
LIMIT 1
FOR UPDATE OF anchor;
