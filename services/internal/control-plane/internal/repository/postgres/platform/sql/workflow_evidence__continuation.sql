-- name: workflow_evidence__continuation :one
SELECT node.id::text,node.run_id::text,node.root_run_id::text,node.agent_id::text,node.attempt,
    node.display_name,node.role,physical.session_id::text,agent.ref,root.workflow_version_id::text,
    COALESCE((SELECT bool_and(control_plane.workflow_evidence_execution_assigned(node.organization_id,node.id,fact.id))
        FROM control_plane.integration_invocations fact WHERE fact.organization_id=node.organization_id AND fact.node_id=node.id
            AND fact.run_id=physical.id AND control_plane.workflow_evidence_delivery_operation(version.spec->>'ResultEvidenceProfile',fact.operation)),false),
    (SELECT count(*) FROM control_plane.run_edges edge WHERE edge.organization_id=node.organization_id AND edge.root_run_id=root.id AND edge.type='CONTINUES')<64,
    jsonb_build_object('schema','kodex.workflow-evidence-outcomes.v1','task',physical.task,'nativeOutcomes',(
        SELECT jsonb_agg(jsonb_build_object('invocationRef',fact.ref,'operation',fact.operation,'state',fact.state,
            'resultSummary',fact.result_summary,'safeErrorCode',fact.safe_error_code,'receiptRef',COALESCE(receipt.ref,''),
            'inputDigest',fact.input_digest,'ownerDecision',CASE WHEN gate.id IS NULL THEN NULL ELSE jsonb_build_object(
                'gateRef',gate.ref,'version',gate.version,'decision',gate.decision,'comment',gate.decision_comment) END) ORDER BY fact.ref)
        FROM control_plane.integration_invocations fact
        LEFT JOIN control_plane.integration_effect_receipts receipt ON receipt.id=fact.effect_receipt_id
            AND receipt.organization_id=node.organization_id AND receipt.invocation_id=fact.id
            AND receipt.effect_key=fact.effect_key AND receipt.input_digest=fact.input_digest
        LEFT JOIN control_plane.owner_gates gate ON gate.integration_invocation_id=fact.id
            AND gate.organization_id=node.organization_id AND gate.root_run_id=root.id
        WHERE fact.organization_id=node.organization_id AND fact.node_id=node.id AND fact.run_id=physical.id
    ))
FROM control_plane.integration_invocations invocation
JOIN control_plane.run_nodes node ON node.id=invocation.node_id AND node.organization_id=invocation.organization_id
JOIN control_plane.runs physical ON physical.id=node.run_id AND physical.organization_id=node.organization_id
    AND physical.id=invocation.run_id AND physical.state='RUNNING'
JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=node.organization_id
    AND root.id=physical.root_run_id AND root.project_id=physical.project_id AND root.state IN ('RUNNING','WAITING_HUMAN')
JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
JOIN control_plane.agents agent ON agent.id=node.agent_id AND agent.organization_id=node.organization_id
WHERE invocation.id=@invocation_id::uuid AND invocation.organization_id=@organization_id::uuid
    AND node.workflow_business_attempt BETWEEN 1 AND 4 AND node.owner_rework_gate_id IS NULL
    AND node.state='SUCCEEDED' AND node.finished_at IS NOT NULL
    AND version.spec->>'ExecutionMode'='SERVER_DAG_V1' AND version.spec->>'ResultEvidenceProfile' IN ('GITHUB_REVIEW_QA_V1','GITHUB_PDLC_V1')
    AND EXISTS(SELECT 1 FROM control_plane.integration_invocations evidence WHERE evidence.organization_id=node.organization_id
        AND evidence.node_id=node.id AND evidence.run_id=physical.id AND control_plane.workflow_evidence_delivery_operation(version.spec->>'ResultEvidenceProfile',evidence.operation))
    AND NOT EXISTS(SELECT 1 FROM control_plane.integration_invocations pending WHERE pending.organization_id=node.organization_id
        AND pending.node_id=node.id AND pending.run_id=physical.id AND pending.state NOT IN ('SUCCEEDED','FAILED','REJECTED','CANCELLED'))
    AND NOT EXISTS(SELECT 1 FROM control_plane.integration_invocations succeeded WHERE succeeded.organization_id=node.organization_id
        AND succeeded.node_id=node.id AND succeeded.run_id=physical.id AND succeeded.state='SUCCEEDED' AND NOT EXISTS(
            SELECT 1 FROM control_plane.integration_effect_receipts receipt WHERE receipt.id=succeeded.effect_receipt_id
                AND receipt.organization_id=node.organization_id AND receipt.invocation_id=succeeded.id
                AND receipt.effect_key=succeeded.effect_key AND receipt.input_digest=succeeded.input_digest AND receipt.result_summary=succeeded.result_summary))
    AND NOT EXISTS(SELECT 1 FROM control_plane.run_edges continuation WHERE continuation.organization_id=node.organization_id
        AND continuation.root_run_id=root.id AND continuation.source_node_id=node.id AND continuation.type='CONTINUES')
FOR UPDATE OF node,root;
