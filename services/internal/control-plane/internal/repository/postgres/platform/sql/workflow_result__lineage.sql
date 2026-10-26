-- name: workflow_result__lineage :one
SELECT step.value,node.workflow_inputs,root.project_id::text,node.state
FROM control_plane.run_nodes node
JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=node.organization_id
JOIN control_plane.runs run ON run.id=node.run_id AND run.organization_id=node.organization_id
    AND run.root_run_id=root.id AND run.project_id=root.project_id
JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id
    AND version.organization_id=root.organization_id
JOIN control_plane.workflows workflow ON workflow.id=version.workflow_id AND workflow.organization_id=root.organization_id
    AND workflow.project_id=root.project_id
CROSS JOIN LATERAL jsonb_array_elements(version.spec->'Steps') step(value)
JOIN control_plane.agents assigned ON assigned.id=node.agent_id AND assigned.organization_id=node.organization_id
    AND assigned.project_id=root.project_id AND assigned.ref=step.value->>'AgentRef'
JOIN control_plane.runtime_revisions revision ON revision.id=@runtime_revision_id::uuid
    AND revision.node_id=node.id AND revision.run_id=node.run_id AND revision.root_run_id=root.id
    AND revision.organization_id=node.organization_id AND revision.agent_id=node.agent_id
    AND revision.turn_id=node.turn_id AND revision.attempt=node.attempt
JOIN control_plane.runtime_leases lease ON lease.id=@lease_id::uuid AND lease.runtime_revision_id=revision.id
    AND lease.organization_id=revision.organization_id AND lease.run_id=revision.run_id AND lease.node_id=revision.node_id
    AND lease.generation=revision.generation AND lease.state='COMPLETED'
JOIN control_plane.subjects actor ON actor.id=root.initiated_by AND actor.organization_id=root.organization_id AND actor.active
WHERE node.id=@node_id::uuid AND node.root_run_id=@root_run_id::uuid AND node.organization_id=@organization_id::uuid
    AND version.spec->>'ExecutionMode'='SERVER_DAG_V1' AND step.value->>'Key'=node.workflow_step_key
    AND node.workflow_business_attempt BETWEEN 1 AND 4 AND node.state IN ('SUCCEEDED','FAILED')
    AND root.state IN ('RUNNING','WAITING_HUMAN')
    AND node.workflow_captured_artifacts->>'schema'='kodex.workflow-capture.v1'
    AND node.workflow_captured_artifacts->>'runtimeRevisionId'=revision.id::text
    AND node.workflow_captured_artifacts->>'leaseId'=lease.id::text
    AND node.workflow_captured_artifacts->'generation'=to_jsonb(lease.generation)
    AND node.workflow_captured_artifacts->>'turnId'=node.turn_id::text
    AND node.workflow_captured_artifacts->'runtimeAttempt'=to_jsonb(node.attempt)
    AND node.workflow_captured_artifacts->'businessAttempt'=to_jsonb(node.workflow_business_attempt)
    AND NOT EXISTS (
        SELECT 1 FROM jsonb_array_elements(node.workflow_inputs) input(value)
        WHERE NOT EXISTS (
            SELECT 1 FROM control_plane.run_nodes source
            CROSS JOIN LATERAL jsonb_array_elements(source.workflow_attempts) accepted(value)
            WHERE source.organization_id=node.organization_id AND source.root_run_id=root.id
                AND source.workflow_step_origin_node_id IS NULL AND source.workflow_business_attempt=1
                AND source.id::text=input.value->>'sourceNodeId' AND source.workflow_step_key=input.value->>'stepKey'
                AND step.value->'DependsOn' ? (source.workflow_step_key)
                AND accepted.value=input.value->'receipt' AND accepted.value->>'accepted'='true'
        )
    );
