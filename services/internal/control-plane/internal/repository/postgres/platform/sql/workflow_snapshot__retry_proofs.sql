-- name: workflow_snapshot__retry_proofs :many
SELECT edge.ref,version.ref,version.digest,origin.ref,origin.workflow_step_key,
       previous.workflow_business_attempt,fresh.workflow_business_attempt
FROM control_plane.runs root
JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id
    AND version.organization_id=root.organization_id
JOIN control_plane.workflows workflow ON workflow.id=version.workflow_id
    AND workflow.organization_id=root.organization_id AND workflow.project_id=root.project_id
    AND workflow.ref=root.target_ref
JOIN control_plane.run_edges edge ON edge.organization_id=root.organization_id
    AND edge.root_run_id=root.id AND edge.type='RETRY_OF'
JOIN control_plane.run_nodes previous ON previous.id=edge.source_node_id
    AND previous.organization_id=root.organization_id AND previous.root_run_id=root.id
JOIN control_plane.run_nodes fresh ON fresh.id=edge.target_node_id
    AND fresh.organization_id=root.organization_id AND fresh.root_run_id=root.id
JOIN control_plane.run_nodes origin ON origin.id=fresh.workflow_step_origin_node_id
    AND origin.organization_id=root.organization_id AND origin.root_run_id=root.id
    AND origin.workflow_step_origin_node_id IS NULL AND origin.workflow_business_attempt=1
JOIN control_plane.run_nodes parent ON parent.id=origin.parent_node_id
    AND parent.organization_id=root.organization_id AND parent.root_run_id=root.id
    AND parent.run_id=root.id AND parent.type='ROOT_PROCESS' AND parent.parent_node_id IS NULL
JOIN control_plane.runs old_run ON old_run.id=previous.run_id
    AND old_run.organization_id=root.organization_id AND old_run.project_id=root.project_id
    AND old_run.root_run_id=root.id AND old_run.parent_run_id=root.id
    AND old_run.workflow_version_id=root.workflow_version_id
JOIN control_plane.runs new_run ON new_run.id=fresh.run_id
    AND new_run.organization_id=root.organization_id AND new_run.project_id=root.project_id
    AND new_run.root_run_id=root.id AND new_run.parent_run_id=root.id
    AND new_run.workflow_version_id=root.workflow_version_id
JOIN control_plane.agents assigned ON assigned.id=origin.agent_id
    AND assigned.organization_id=root.organization_id AND assigned.project_id=root.project_id
CROSS JOIN LATERAL jsonb_array_elements(version.spec->'Steps') step(value)
CROSS JOIN LATERAL jsonb_array_elements(origin.workflow_attempts) receipt(value)
CROSS JOIN LATERAL jsonb_array_elements(root.workflow_exception_history) episode(value)
JOIN control_plane.owner_gates gate ON gate.id::text=episode.value->>'gateId'
    AND gate.ref=episode.value->>'gateRef' AND gate.organization_id=root.organization_id
    AND gate.root_run_id=root.id AND gate.project_id=root.project_id
    AND gate.integration_invocation_id IS NULL AND gate.state='APPROVED' AND gate.decision='APPROVE'
JOIN control_plane.run_nodes gate_node ON gate_node.id=gate.node_id
    AND gate_node.organization_id=root.organization_id AND gate_node.root_run_id=root.id
    AND gate_node.type='HUMAN_GATE'
    AND gate_node.parent_node_id::text=COALESCE(NULLIF(episode.value->>'managerNodeId',''),previous.id::text)
JOIN control_plane.runtime_revisions revision ON revision.id::text=receipt.value->>'runtimeRevisionId'
    AND revision.organization_id=root.organization_id AND revision.root_run_id=root.id
    AND revision.project_id=root.project_id AND revision.session_id=old_run.session_id
    AND revision.run_id=previous.run_id AND revision.node_id=previous.id
    AND revision.turn_id=previous.turn_id AND revision.agent_id=previous.agent_id
    AND revision.attempt=previous.attempt
JOIN control_plane.runtime_leases lease ON lease.id::text=receipt.value->'provenance'->>'leaseId'
    AND lease.runtime_revision_id=revision.id AND lease.organization_id=root.organization_id
    AND lease.run_id=previous.run_id AND lease.node_id=previous.id
    AND lease.generation=revision.generation AND lease.input_digest=revision.input_digest
    AND lease.state='COMPLETED'
JOIN LATERAL (
    WITH RECURSIVE bounds AS MATERIALIZED (
        SELECT count(*) AS nodes FROM control_plane.run_nodes owned
        WHERE owned.organization_id=root.organization_id AND owned.root_run_id=root.id AND owned.run_id=old_run.id
    ), lineage AS (
        SELECT previous.id,previous.parent_node_id,previous.attempt,ARRAY[previous.id] AS path
        UNION ALL
        SELECT ancestor.id,ancestor.parent_node_id,ancestor.attempt,lineage.path||ancestor.id
        FROM lineage
        JOIN control_plane.run_edges continuation ON continuation.organization_id=root.organization_id
            AND continuation.root_run_id=root.id AND continuation.type='CONTINUES' AND continuation.target_node_id=lineage.id
        JOIN control_plane.run_nodes ancestor ON ancestor.id=continuation.source_node_id AND ancestor.id=lineage.parent_node_id
            AND ancestor.organization_id=root.organization_id AND ancestor.root_run_id=root.id AND ancestor.run_id=old_run.id
            AND ancestor.type='AGENT_EXECUTION' AND ancestor.state='SUCCEEDED' AND ancestor.materialization_state='MATERIALIZED'
            AND ancestor.agent_id=previous.agent_id AND ancestor.workflow_step_key=origin.workflow_step_key
            AND ancestor.attempt+1=lineage.attempt AND ancestor.attempt>=1
            AND ancestor.workflow_business_attempt=previous.workflow_business_attempt
            AND COALESCE(ancestor.workflow_step_origin_node_id,ancestor.id)=origin.id
            AND ancestor.workflow_inputs=previous.workflow_inputs
        JOIN control_plane.session_turns turn ON turn.id=ancestor.turn_id AND turn.organization_id=root.organization_id
            AND turn.run_id=old_run.id AND turn.session_id=old_run.session_id
        JOIN control_plane.runtime_revisions captured ON captured.id::text=ancestor.workflow_captured_artifacts->>'runtimeRevisionId'
            AND captured.organization_id=root.organization_id AND captured.root_run_id=root.id AND captured.project_id=root.project_id
            AND captured.run_id=old_run.id AND captured.session_id=old_run.session_id AND captured.node_id=ancestor.id
            AND captured.turn_id=ancestor.turn_id AND captured.agent_id=ancestor.agent_id AND captured.attempt=ancestor.attempt
        JOIN control_plane.runtime_leases completed ON completed.id::text=ancestor.workflow_captured_artifacts->>'leaseId'
            AND completed.organization_id=root.organization_id AND completed.runtime_revision_id=captured.id
            AND completed.run_id=old_run.id AND completed.node_id=ancestor.id AND completed.generation=captured.generation
            AND completed.input_digest=captured.input_digest AND completed.state='COMPLETED'
        WHERE cardinality(lineage.path)<(SELECT nodes FROM bounds) AND NOT ancestor.id=ANY(lineage.path)
            AND ancestor.workflow_captured_artifacts->>'schema'='kodex.workflow-capture.v1'
            AND ancestor.workflow_captured_artifacts->>'turnId'=ancestor.turn_id::text
            AND ancestor.workflow_captured_artifacts->'runtimeAttempt'=to_jsonb(ancestor.attempt)
            AND ancestor.workflow_captured_artifacts->'businessAttempt'=to_jsonb(ancestor.workflow_business_attempt)
            AND ancestor.workflow_captured_artifacts->'generation'=to_jsonb(completed.generation)
    )
    SELECT 1 AS sealed
    WHERE 1=(SELECT count(*) FROM lineage first
        WHERE first.attempt=1 AND first.parent_node_id=origin.parent_node_id
            AND ((previous.workflow_business_attempt=1 AND first.id=origin.id) OR (
                previous.workflow_business_attempt>1 AND EXISTS (
                    SELECT 1 FROM jsonb_array_elements(root.workflow_exception_history) prior_episode(value)
                    JOIN control_plane.owner_gates prior_gate ON prior_gate.id::text=prior_episode.value->>'gateId'
                        AND prior_gate.ref=prior_episode.value->>'gateRef' AND prior_gate.organization_id=root.organization_id
                        AND prior_gate.root_run_id=root.id AND prior_gate.project_id=root.project_id
                        AND prior_gate.state='APPROVED' AND prior_gate.decision='APPROVE' AND prior_gate.integration_invocation_id IS NULL
                    JOIN control_plane.run_edges prior_retry ON prior_retry.organization_id=root.organization_id
                        AND prior_retry.root_run_id=root.id AND prior_retry.type='RETRY_OF'
                        AND prior_retry.target_node_id=first.id AND prior_retry.source_node_id::text=prior_episode.value->>'finalNodeId'
                    WHERE prior_episode.value->>'state'='RECOVERED' AND prior_episode.value->>'originNodeId'=origin.id::text
                        AND prior_episode.value->>'recoveryNodeId'=first.id::text AND prior_episode.value->>'stepKey'=origin.workflow_step_key
                        AND prior_episode.value->'attempt'=to_jsonb(previous.workflow_business_attempt-1)
                        AND prior_episode.value->'inputs'=previous.workflow_inputs
                ))))
      AND NOT EXISTS (SELECT 1 FROM lineage member
        WHERE (SELECT count(*) FROM control_plane.run_edges incoming
            WHERE incoming.organization_id=root.organization_id AND incoming.root_run_id=root.id
                AND incoming.type='CONTINUES' AND incoming.target_node_id=member.id)
            <>CASE WHEN member.attempt=1 THEN 0 ELSE 1 END)
) chain ON true
WHERE root.organization_id=@organization_id::uuid AND root.ref=@root_run_ref
    AND root.id=root.root_run_id AND root.target_type='WORKFLOW'
    AND version.spec->>'ExecutionMode'='SERVER_DAG_V1'
    AND previous.type='AGENT_EXECUTION' AND fresh.type='AGENT_EXECUTION' AND origin.type='AGENT_EXECUTION'
    AND previous.state IN ('SUCCEEDED','FAILED') AND previous.materialization_state='MATERIALIZED'
    AND fresh.materialization_state='MATERIALIZED' AND fresh.attempt=1
    AND fresh.parent_node_id=origin.parent_node_id
    AND previous.agent_id=origin.agent_id AND fresh.agent_id=origin.agent_id
    AND previous.workflow_step_key=origin.workflow_step_key AND fresh.workflow_step_key=origin.workflow_step_key
    AND COALESCE(previous.workflow_step_origin_node_id,previous.id)=origin.id
    AND previous.workflow_business_attempt BETWEEN 1 AND 3
    AND fresh.workflow_business_attempt=previous.workflow_business_attempt+1
    AND fresh.workflow_business_attempt BETWEEN 2 AND 4
    AND previous.workflow_inputs=fresh.workflow_inputs AND previous.workflow_inputs=episode.value->'inputs'
    AND old_run.id<>new_run.id AND old_run.id<>root.id AND new_run.id<>root.id
    AND old_run.source='AGENT_DELEGATION' AND new_run.source='AGENT_DELEGATION'
    AND old_run.target_type='AGENT' AND new_run.target_type='AGENT'
    AND old_run.target_ref=assigned.ref AND new_run.target_ref=assigned.ref
    AND new_run.retry_of_run_id IS NULL AND new_run.attempt=1
    AND step.value->>'Key'=origin.workflow_step_key AND step.value->>'AgentRef'=assigned.ref
    AND receipt.value->'attempt'=to_jsonb(previous.workflow_business_attempt)
    AND receipt.value->>'runRef'=old_run.ref
    AND receipt.value->'provenance'->>'rootRunId'=root.id::text
    AND receipt.value->'provenance'->>'originNodeId'=origin.id::text
    AND receipt.value->'provenance'->>'nodeId'=previous.id::text
    AND receipt.value->'provenance'->>'turnId'=previous.turn_id::text
    AND receipt.value->'provenance'->'runtimeAttempt'=to_jsonb(previous.attempt)
    AND receipt.value->'provenance'->'generation'=to_jsonb(lease.generation)
    AND previous.workflow_captured_artifacts->>'schema'='kodex.workflow-capture.v1'
    AND previous.workflow_captured_artifacts->>'runtimeRevisionId'=revision.id::text
    AND previous.workflow_captured_artifacts->>'leaseId'=lease.id::text
    AND previous.workflow_captured_artifacts->>'turnId'=previous.turn_id::text
    AND previous.workflow_captured_artifacts->'generation'=to_jsonb(lease.generation)
    AND previous.workflow_captured_artifacts->'runtimeAttempt'=to_jsonb(previous.attempt)
    AND previous.workflow_captured_artifacts->'businessAttempt'=to_jsonb(previous.workflow_business_attempt)
    AND episode.value->>'state'='RECOVERED'
    AND episode.value->>'originNodeId'=origin.id::text AND episode.value->>'finalNodeId'=previous.id::text
    AND episode.value->>'recoveryNodeId'=fresh.id::text AND episode.value->>'stepKey'=origin.workflow_step_key
    AND episode.value->'attempt'=to_jsonb(previous.workflow_business_attempt)
    AND (receipt.value->'accepted'='false'::jsonb OR (
        episode.value->>'reason'='OWNER_CHANGES_REQUESTED' AND EXISTS (
            SELECT 1 FROM control_plane.owner_gates result_gate
            JOIN control_plane.run_edges result_edge ON result_edge.organization_id=root.organization_id
                AND result_edge.root_run_id=root.id AND result_edge.type='WAITING_FOR'
                AND result_edge.source_node_id=previous.id AND result_edge.target_node_id=result_gate.node_id
            WHERE result_gate.organization_id=root.organization_id AND result_gate.root_run_id=root.id
                AND result_gate.project_id=root.project_id AND result_gate.integration_invocation_id IS NULL
                AND result_gate.state='CHANGES_REQUESTED' AND result_gate.decision='REQUEST_CHANGES'
        )))
ORDER BY edge.ref;
