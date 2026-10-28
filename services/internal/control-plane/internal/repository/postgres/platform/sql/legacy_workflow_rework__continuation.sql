-- name: legacy_workflow_rework__continuation :one
SELECT parent.run_id::text,parent.root_run_id::text,parent.agent_id::text,parent.attempt,parent.display_name,parent.role,
    physical.session_id::text,agent.ref,root.workflow_version_id::text,parent.workflow_step_key,false,false,parent.ref,
    jsonb_build_object('ownerReworkSchema','kodex.legacy-owner-rework.v1','completedChildren',COALESCE((
        SELECT jsonb_agg(jsonb_build_object('runRef',child.ref,'nodeRef',source.ref,
            'state',receipt.result_snapshot->>'state','resultSummary',receipt.result_snapshot->>'resultSummary',
            'artifactRefs',COALESCE((SELECT jsonb_agg(pin.value->>'ref' ORDER BY pin.value->>'ref')
                FROM jsonb_array_elements(receipt.result_snapshot->'artifacts') pin(value)),'[]'::jsonb))
            || CASE WHEN EXISTS (SELECT 1 FROM control_plane.repository_bundle_publications publication
                WHERE publication.organization_id=root.organization_id AND publication.origin_run_id=child.id
                    AND publication.root_run_id=root.id AND publication.project_id=root.project_id)
                THEN jsonb_build_object('repositoryPublicationOutcome',control_plane.repository_bundle_callback_outcome(root.organization_id,child.id))
                ELSE '{}'::jsonb END
            || CASE WHEN owner_decision.id IS NOT NULL THEN jsonb_build_object(
                'requestChanges',jsonb_build_object('gateRef',owner_decision.ref,'gateVersion',owner_decision.version,
                    'decision',owner_decision.decision,'comment',owner_decision.decision_comment),
                'acceptedResultGate',accepted_gate.value) ELSE '{}'::jsonb END
            ORDER BY callback.ref)
        FROM control_plane.run_edges callback
        JOIN control_plane.callback_receipts receipt ON receipt.callback_edge_id=callback.id
        JOIN control_plane.run_nodes source ON source.id=callback.source_node_id AND source.organization_id=root.organization_id
            AND source.root_run_id=root.id
        JOIN control_plane.runs child ON child.id=receipt.child_run_id AND child.organization_id=root.organization_id
            AND child.root_run_id=root.id AND child.project_id=root.project_id AND child.id=source.run_id
        LEFT JOIN control_plane.owner_gates owner_decision ON owner_decision.id=source.owner_rework_gate_id
            AND owner_decision.organization_id=root.organization_id AND owner_decision.root_run_id=root.id
            AND owner_decision.state='CHANGES_REQUESTED' AND owner_decision.decision='REQUEST_CHANGES'
        LEFT JOIN LATERAL (
            SELECT (jsonb_agg(jsonb_build_object('gateRef',result_gate.ref,'gateVersion',result_gate.version,
                'decision',result_gate.decision,'comment',result_gate.decision_comment)))->0 AS value
            FROM control_plane.run_nodes final_node
            JOIN control_plane.owner_gates result_gate ON result_gate.organization_id=root.organization_id
                AND result_gate.root_run_id=root.id AND result_gate.integration_invocation_id IS NULL
                AND result_gate.state='APPROVED' AND result_gate.decision='APPROVE'
            JOIN control_plane.run_nodes gate_node ON gate_node.id=result_gate.node_id AND gate_node.organization_id=root.organization_id
                AND gate_node.root_run_id=root.id AND gate_node.type='HUMAN_GATE' AND gate_node.parent_node_id=final_node.id
            WHERE final_node.organization_id=root.organization_id AND final_node.root_run_id=root.id AND final_node.run_id=child.id
                AND COALESCE(final_node.owner_rework_origin_node_id,final_node.id)=source.id
                AND final_node.owner_rework_gate_id=source.owner_rework_gate_id AND final_node.state='SUCCEEDED'
                AND NOT EXISTS (SELECT 1 FROM control_plane.run_edges future WHERE future.source_node_id=final_node.id AND future.type='CONTINUES')
            HAVING count(*)=1
        ) accepted_gate ON owner_decision.id IS NOT NULL
        WHERE callback.organization_id=root.organization_id AND callback.root_run_id=root.id
            AND callback.target_node_id=parent.id AND callback.type='CALLBACK_TO'
            AND receipt.result_snapshot->>'state' IN ('SUCCEEDED','FAILED','CANCELLED')
            AND NOT EXISTS (SELECT 1 FROM control_plane.runs replacement
                JOIN control_plane.run_nodes replacement_node ON replacement_node.run_id=replacement.id
                    AND replacement_node.organization_id=root.organization_id AND replacement_node.root_run_id=root.id
                    AND replacement_node.owner_rework_gate_id IS NOT NULL AND replacement_node.owner_rework_origin_node_id IS NULL
                WHERE replacement.retry_of_run_id=child.id AND replacement.organization_id=root.organization_id
                    AND replacement.root_run_id=root.id)
    ),'[]'::jsonb),'remainingStepKeys',COALESCE((
        SELECT jsonb_agg(step.value->>'Key' ORDER BY step.position)
        FROM jsonb_array_elements(version.spec->'Steps') WITH ORDINALITY step(value,position)
        WHERE NOT EXISTS (SELECT 1 FROM control_plane.run_nodes assigned WHERE assigned.organization_id=root.organization_id
            AND assigned.root_run_id=root.id AND assigned.workflow_step_key=step.value->>'Key'
            AND assigned.materialization_state='MATERIALIZED')
    ),'[]'::jsonb)),false
FROM control_plane.run_nodes parent
JOIN control_plane.runs physical ON physical.id=parent.run_id AND physical.organization_id=parent.organization_id
JOIN control_plane.runs root ON root.id=parent.root_run_id AND root.organization_id=parent.organization_id
    AND root.id=physical.id AND root.state IN ('RUNNING','WAITING_HUMAN')
JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
JOIN control_plane.workflows workflow ON workflow.id=version.workflow_id AND workflow.organization_id=root.organization_id
    AND workflow.project_id=root.project_id
JOIN control_plane.agents agent ON agent.id=parent.agent_id AND agent.organization_id=root.organization_id
    AND agent.project_id=root.project_id AND agent.ref=version.spec->>'CoordinatorAgentRef' AND agent.enabled AND agent.state='READY'
JOIN control_plane.subjects actor ON actor.id=root.initiated_by AND actor.organization_id=root.organization_id AND actor.active
JOIN control_plane.catalog_access_targets target ON target.organization_id=root.organization_id AND target.kind='WORKFLOW' AND target.id=workflow.id
WHERE parent.organization_id=@organization_id::uuid AND parent.id=@parent_node_id::uuid
    AND COALESCE(version.spec->>'ExecutionMode','')<>'SERVER_DAG_V1'
    AND control_plane.catalog_resource_visible(root.organization_id,actor.id,'workflow.launch','WORKFLOW',target.id,
        target.project_id,target.owner_id,target.related_ids,statement_timestamp(),false)
    AND parent.state IN ('SUCCEEDED','CANCELLED')
    AND (parent.state='SUCCEEDED' OR NOT EXISTS (SELECT 1 FROM control_plane.runtime_revisions revision WHERE revision.node_id=parent.id))
    AND NOT EXISTS (SELECT 1 FROM control_plane.runtime_leases lease WHERE lease.node_id=parent.id AND lease.state='CLAIMED')
    AND NOT EXISTS (SELECT 1 FROM control_plane.run_edges future WHERE future.source_node_id=parent.id AND future.type='CONTINUES')
    AND NOT EXISTS (SELECT 1 FROM control_plane.owner_gates gate WHERE gate.organization_id=root.organization_id
        AND gate.root_run_id=root.id AND gate.state='OPEN')
    AND EXISTS (SELECT 1 FROM control_plane.run_edges callback
        JOIN control_plane.run_nodes rework ON rework.id=callback.source_node_id AND rework.organization_id=root.organization_id
            AND rework.root_run_id=root.id AND rework.owner_rework_gate_id IS NOT NULL AND rework.owner_rework_origin_node_id IS NULL
        JOIN control_plane.owner_gates decision ON decision.id=rework.owner_rework_gate_id AND decision.organization_id=root.organization_id
            AND decision.root_run_id=root.id AND decision.state='CHANGES_REQUESTED' AND decision.decision='REQUEST_CHANGES'
        WHERE callback.organization_id=root.organization_id AND callback.root_run_id=root.id AND callback.type='CALLBACK_TO'
            AND callback.target_node_id=parent.id)
    AND NOT EXISTS (SELECT 1 FROM control_plane.run_edges callback
        WHERE callback.organization_id=root.organization_id AND callback.root_run_id=root.id
            AND callback.target_node_id=parent.id AND callback.type='CALLBACK_TO'
            AND NOT EXISTS (SELECT 1 FROM control_plane.callback_receipts receipt WHERE receipt.callback_edge_id=callback.id))
    AND NOT EXISTS (
        SELECT 1 FROM control_plane.run_edges callback
        JOIN control_plane.run_nodes rework ON rework.id=callback.source_node_id AND rework.organization_id=root.organization_id
            AND rework.root_run_id=root.id AND rework.owner_rework_gate_id IS NOT NULL AND rework.owner_rework_origin_node_id IS NULL
        WHERE callback.organization_id=root.organization_id AND callback.root_run_id=root.id
            AND callback.target_node_id=parent.id AND callback.type='CALLBACK_TO'
            AND NOT EXISTS (SELECT 1 FROM control_plane.runs replacement
                JOIN control_plane.run_nodes next_node ON next_node.run_id=replacement.id AND next_node.organization_id=root.organization_id
                    AND next_node.owner_rework_gate_id IS NOT NULL AND next_node.owner_rework_origin_node_id IS NULL
                WHERE replacement.retry_of_run_id=rework.run_id AND replacement.organization_id=root.organization_id AND replacement.root_run_id=root.id)
            AND NOT EXISTS (
                SELECT 1 FROM control_plane.run_nodes final_node
                JOIN control_plane.owner_gates result_gate ON result_gate.organization_id=root.organization_id AND result_gate.root_run_id=root.id
                    AND result_gate.integration_invocation_id IS NULL AND result_gate.state='APPROVED' AND result_gate.decision='APPROVE'
                JOIN control_plane.run_nodes gate_node ON gate_node.id=result_gate.node_id AND gate_node.organization_id=root.organization_id
                    AND gate_node.root_run_id=root.id AND gate_node.parent_node_id=final_node.id AND gate_node.type='HUMAN_GATE'
                WHERE final_node.organization_id=root.organization_id AND final_node.root_run_id=root.id AND final_node.run_id=rework.run_id
                    AND COALESCE(final_node.owner_rework_origin_node_id,final_node.id)=rework.id
                    AND final_node.owner_rework_gate_id=rework.owner_rework_gate_id AND final_node.state='SUCCEEDED'
                    AND NOT EXISTS (SELECT 1 FROM control_plane.run_edges next_edge
                        WHERE next_edge.source_node_id=final_node.id AND next_edge.type='CONTINUES')
            )
    )
FOR UPDATE OF parent;
