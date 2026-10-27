-- name: workflow_rework__materialize :one
INSERT INTO control_plane.run_nodes(ref,organization_id,root_run_id,run_id,parent_node_id,type,state,
    agent_id,turn_id,display_name,role,workflow_step_key,attempt,next_actions,
    workflow_step_origin_node_id,workflow_business_attempt,workflow_inputs,workflow_rework_gate_id)
SELECT @node_ref,anchor.organization_id,anchor.root_run_id,@run_id::uuid,anchor.parent_node_id,'AGENT_EXECUTION','QUEUED',
    anchor.agent_id,@turn_id::uuid,@display_name,@role,anchor.workflow_step_key,1,ARRAY['OPEN','CANCEL'],
    anchor.id,@business_attempt::integer,@inputs::jsonb,gate.id
FROM control_plane.run_nodes anchor
JOIN control_plane.runs root ON root.id=anchor.root_run_id AND root.organization_id=anchor.organization_id
JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
JOIN control_plane.owner_gates gate ON gate.id=@gate_id::uuid AND gate.organization_id=root.organization_id
    AND gate.root_run_id=root.id AND gate.project_id=root.project_id AND gate.integration_invocation_id IS NULL
    AND gate.state='APPROVED' AND gate.decision='APPROVE'
JOIN control_plane.run_nodes previous ON previous.organization_id=anchor.organization_id AND previous.root_run_id=anchor.root_run_id
    AND COALESCE(previous.workflow_step_origin_node_id,previous.id)=anchor.id AND previous.id=@previous_node_id::uuid
    AND previous.workflow_business_attempt=@business_attempt::integer-1 AND previous.state IN ('SUCCEEDED','FAILED')
WHERE anchor.organization_id=@organization_id::uuid AND anchor.root_run_id=@root_run_id::uuid
    AND anchor.id=@origin_node_id::uuid AND anchor.workflow_step_origin_node_id IS NULL AND anchor.agent_id=@agent_id::uuid
    AND version.spec->>'ExecutionMode'='SERVER_DAG_V1' AND version.spec->>'ReworkPolicy'='OWNER_APPROVED_UPSTREAM_V1'
    AND root.state IN ('RUNNING','WAITING_HUMAN') AND @business_attempt::integer BETWEEN 2 AND 4
    AND jsonb_array_length(anchor.workflow_attempts)=@business_attempt::integer-1
    AND anchor.workflow_attempts->-1->'provenance'->>'nodeId'=previous.id::text
    AND @inputs::jsonb=control_plane.workflow_effective_inputs(anchor.organization_id,root.id,anchor.workflow_step_key)
    AND NOT EXISTS(SELECT 1 FROM control_plane.run_nodes other WHERE other.organization_id=anchor.organization_id
        AND other.root_run_id=anchor.root_run_id AND COALESCE(other.workflow_step_origin_node_id,other.id)=anchor.id
        AND other.workflow_business_attempt>=@business_attempt::integer)
RETURNING id::text;
