-- name: workflow_rework__superseded_gates :many
SELECT gate.id::text,gate.ref,gate.version,node.id::text,node.ref
FROM control_plane.runs root
JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
CROSS JOIN LATERAL jsonb_array_elements(root.workflow_exception_history) episode(value)
JOIN control_plane.owner_gates approval ON approval.id=@approval_gate_id::uuid AND approval.id::text=episode.value->>'gateId'
    AND approval.ref=episode.value->>'gateRef' AND approval.organization_id=root.organization_id
    AND approval.root_run_id=root.id AND approval.project_id=root.project_id
    AND approval.integration_invocation_id IS NULL AND approval.state='APPROVED' AND approval.decision='APPROVE'
CROSS JOIN LATERAL jsonb_array_elements((episode.value->'reworkPlan'->'targets')||(episode.value->'reworkPlan'->'rechecks')) member(value)
JOIN control_plane.run_nodes anchor ON anchor.id::text=member.value->>'originNodeId'
    AND anchor.organization_id=root.organization_id AND anchor.root_run_id=root.id
    AND anchor.workflow_step_origin_node_id IS NULL AND anchor.workflow_step_key=member.value->>'stepKey'
JOIN control_plane.run_nodes producer ON producer.id::text=anchor.workflow_attempts->((member.value->>'previousAttempt')::integer-1)->'provenance'->>'nodeId'
    AND producer.organization_id=root.organization_id AND producer.root_run_id=root.id
    AND COALESCE(producer.workflow_step_origin_node_id,producer.id)=anchor.id
    AND producer.workflow_business_attempt=(member.value->>'previousAttempt')::integer
    AND producer.state IN ('SUCCEEDED','FAILED')
JOIN control_plane.run_edges waiting ON waiting.organization_id=root.organization_id AND waiting.root_run_id=root.id
    AND waiting.source_node_id=producer.id AND waiting.type='WAITING_FOR'
JOIN control_plane.run_nodes node ON node.id=waiting.target_node_id AND node.parent_node_id=producer.id
    AND node.organization_id=root.organization_id AND node.root_run_id=root.id AND node.type='HUMAN_GATE' AND node.state='WAITING'
JOIN control_plane.owner_gates gate ON gate.node_id=node.id AND gate.organization_id=root.organization_id
    AND gate.root_run_id=root.id AND gate.project_id=root.project_id AND gate.scope_kind='PROJECT'
    AND gate.integration_invocation_id IS NULL AND gate.state='OPEN' AND gate.id<>approval.id
WHERE root.organization_id=@organization_id::uuid AND root.id=@root_run_id::uuid AND root.id=root.root_run_id
    AND root.state IN ('RUNNING','WAITING_HUMAN') AND version.spec->>'ReworkPolicy'='OWNER_APPROVED_UPSTREAM_V1'
    AND version.spec->>'ExecutionMode'='SERVER_DAG_V1'
    AND episode.value=root.workflow_exception AND episode.value->>'kind'='UPSTREAM_REWORK'
    AND episode.value->>'state'='WAITING_OWNER' AND episode.value->'reworkPlan'=@plan::jsonb
    AND COALESCE(episode.value->'reworkPlan'->>'blockReason','')=''
    AND EXISTS(SELECT 1 FROM jsonb_array_elements(version.spec->'Steps') step(value)
        WHERE step.value->>'Key'=anchor.workflow_step_key AND step.value->>'HumanGateAfter'='true')
    AND NOT EXISTS(SELECT 1 FROM control_plane.interaction_deliveries delivery
        WHERE delivery.organization_id=root.organization_id AND delivery.approval_gate_id=gate.id)
ORDER BY gate.id FOR UPDATE OF gate,node NOWAIT;
