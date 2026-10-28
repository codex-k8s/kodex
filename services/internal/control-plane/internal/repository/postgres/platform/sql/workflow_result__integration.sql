-- name: workflow_result__integration :many
WITH RECURSIVE final AS MATERIALIZED (
    SELECT node.id,node.organization_id,node.root_run_id,node.run_id,node.agent_id,node.turn_id,node.attempt,
           COALESCE(node.workflow_step_origin_node_id,node.id) AS origin_id,node.workflow_business_attempt,
           node.workflow_captured_artifacts,node.workflow_step_key,root.project_id,root.initiated_by AS actor_id,
           run.session_id,revision.id AS revision_id,revision.ref AS revision_ref,revision.generation,revision.safe_snapshot
    FROM control_plane.run_nodes node
    JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=node.organization_id
    JOIN control_plane.runs run ON run.id=node.run_id AND run.organization_id=node.organization_id
        AND run.root_run_id=root.id AND run.project_id=root.project_id
    JOIN control_plane.runtime_revisions revision ON revision.node_id=node.id AND revision.run_id=node.run_id
        AND revision.organization_id=node.organization_id AND revision.root_run_id=node.root_run_id
        AND revision.turn_id=node.turn_id AND revision.agent_id=node.agent_id AND revision.attempt=node.attempt
    JOIN control_plane.runtime_leases lease ON lease.runtime_revision_id=revision.id
        AND lease.organization_id=revision.organization_id AND lease.run_id=revision.run_id
        AND lease.node_id=revision.node_id AND lease.generation=revision.generation AND lease.state='COMPLETED'
    JOIN control_plane.subjects actor ON actor.id=root.initiated_by AND actor.organization_id=root.organization_id AND actor.active
    WHERE node.organization_id=@organization_id::uuid AND node.id=@node_id::uuid
        AND node.root_run_id=@root_run_id::uuid AND revision.id=@runtime_revision_id::uuid AND lease.id=@lease_id::uuid
        AND node.workflow_business_attempt BETWEEN 1 AND 4 AND node.state='SUCCEEDED'
        AND node.workflow_captured_artifacts->>'runtimeRevisionId'=revision.id::text
        AND node.workflow_captured_artifacts->>'leaseId'=lease.id::text
        AND node.workflow_captured_artifacts->'generation'=to_jsonb(lease.generation)
), lineage AS (
    SELECT final.id,final.turn_id,final.attempt,final.workflow_captured_artifacts,0 AS depth,ARRAY[final.id] AS path,false AS cycle
    FROM final
    UNION ALL
    SELECT previous.id,previous.turn_id,previous.attempt,previous.workflow_captured_artifacts,
           lineage.depth+1,lineage.path || previous.id,previous.id=ANY(lineage.path)
    FROM lineage CROSS JOIN final
    JOIN control_plane.run_edges edge ON edge.organization_id=final.organization_id AND edge.root_run_id=final.root_run_id
        AND edge.type='CONTINUES' AND edge.target_node_id=lineage.id
    JOIN control_plane.run_nodes previous ON previous.id=edge.source_node_id AND previous.organization_id=final.organization_id
        AND previous.root_run_id=final.root_run_id AND previous.run_id=final.run_id AND previous.agent_id=final.agent_id
        AND previous.state='SUCCEEDED' AND previous.attempt+1=lineage.attempt
        AND COALESCE(previous.workflow_step_origin_node_id,previous.id)=final.origin_id
        AND previous.workflow_business_attempt=final.workflow_business_attempt AND previous.workflow_step_key=final.workflow_step_key
    JOIN control_plane.session_turns turn ON turn.id=previous.turn_id AND turn.run_id=previous.run_id AND turn.session_id=final.session_id
    JOIN control_plane.runtime_revisions revision ON revision.id::text=previous.workflow_captured_artifacts->>'runtimeRevisionId'
        AND revision.organization_id=previous.organization_id AND revision.root_run_id=previous.root_run_id
        AND revision.node_id=previous.id AND revision.run_id=previous.run_id AND revision.turn_id=previous.turn_id
        AND revision.agent_id=previous.agent_id AND revision.attempt=previous.attempt
    JOIN control_plane.runtime_leases lease ON lease.id::text=previous.workflow_captured_artifacts->>'leaseId'
        AND lease.organization_id=revision.organization_id AND lease.runtime_revision_id=revision.id
        AND lease.node_id=previous.id AND lease.run_id=previous.run_id AND lease.generation=revision.generation
        AND to_jsonb(lease.generation)=previous.workflow_captured_artifacts->'generation' AND lease.state='COMPLETED'
    WHERE NOT lineage.cycle AND lineage.depth<65
        AND 1=(SELECT count(*) FROM control_plane.run_edges incoming
            WHERE incoming.organization_id=final.organization_id AND incoming.root_run_id=final.root_run_id
                AND incoming.type='CONTINUES' AND incoming.target_node_id=lineage.id)
)
SELECT invocation.operation,invocation.bounded_input,invocation.resource_scope,receipt.result_summary,
       receipt.response_digest,receipt.provider_effect_ref,
       CASE
        WHEN EXISTS (SELECT 1 FROM lineage WHERE cycle OR depth>64) OR EXISTS (
            SELECT 1 FROM lineage current
            WHERE (SELECT count(*) FROM control_plane.run_edges incoming
                WHERE incoming.organization_id=final.organization_id AND incoming.root_run_id=final.root_run_id
                    AND incoming.type='CONTINUES' AND incoming.target_node_id=current.id)>1
            OR EXISTS (SELECT 1 FROM control_plane.run_edges incoming
                WHERE incoming.organization_id=final.organization_id AND incoming.root_run_id=final.root_run_id
                    AND incoming.type='CONTINUES' AND incoming.target_node_id=current.id
                    AND NOT EXISTS (SELECT 1 FROM lineage previous
                        WHERE previous.id=incoming.source_node_id AND previous.depth=current.depth+1))
        ) THEN 'RECEIPT_LINEAGE_INVALID'
        WHEN invocation.operation<>'github.repository.bundle.publish' AND
            (SELECT count(*) FROM control_plane.runtime_revisions candidate
             WHERE candidate.organization_id=final.organization_id AND candidate.node_id=producer.id)<>1
            THEN 'RECEIPT_LINEAGE_AMBIGUOUS'
        WHEN NOT EXISTS (
            SELECT 1 FROM jsonb_array_elements(COALESCE(proof.safe_snapshot->'integrationGrants','[]'::jsonb)) pin(value)
            WHERE pin.value->>'ref'=grant_binding.ref AND pin.value->>'grantVersion'=invocation.grant_version::text
                AND pin.value->>'connectionRef'=connection.ref AND pin.value->>'capabilityKey'=invocation.capability_key
                AND pin.value->>'operation'=invocation.operation AND pin.value->>'definitionVersion'=invocation.definition_version
                AND pin.value->>'definitionDigest'=invocation.definition_digest
                AND pin.value->>'approvalPolicy'=invocation.approval_policy
        ) THEN 'RECEIPT_SCOPE_MISMATCH'
        WHEN invocation.operation='github.repository.bundle.publish' AND NOT EXISTS (
            SELECT 1 FROM control_plane.repository_bundle_publications publication
            WHERE publication.organization_id=final.organization_id AND publication.project_id=final.project_id
                AND publication.root_run_id=final.root_run_id AND publication.root_actor_id=final.actor_id
                AND publication.origin_run_id=final.run_id AND publication.origin_node_id=producer.id
                AND publication.origin_runtime_revision_id=proof.id AND publication.origin_lease_ref=producer_lease.ref
                AND publication.origin_generation=proof.generation
                AND publication.integration_invocation_id=invocation.id AND publication.connection_id=invocation.connection_id
                AND publication.grant_id=invocation.grant_id AND publication.state='SUCCEEDED'
                AND publication.candidate_commit_sha=receipt.provider_effect_ref
        ) THEN 'RECEIPT_LINEAGE_INVALID'
        ELSE '' END AS failure_reason
FROM lineage producer CROSS JOIN final
JOIN control_plane.runtime_revisions proof ON proof.id::text=producer.workflow_captured_artifacts->>'runtimeRevisionId'
    AND proof.organization_id=final.organization_id AND proof.root_run_id=final.root_run_id
    AND proof.run_id=final.run_id AND proof.node_id=producer.id AND proof.agent_id=final.agent_id
    AND proof.turn_id=producer.turn_id AND proof.attempt=producer.attempt
JOIN control_plane.runtime_leases producer_lease ON producer_lease.id::text=producer.workflow_captured_artifacts->>'leaseId'
    AND producer_lease.organization_id=proof.organization_id AND producer_lease.runtime_revision_id=proof.id
    AND producer_lease.node_id=producer.id AND producer_lease.run_id=proof.run_id AND producer_lease.generation=proof.generation
    AND to_jsonb(producer_lease.generation)=producer.workflow_captured_artifacts->'generation' AND producer_lease.state='COMPLETED'
JOIN control_plane.integration_invocations invocation ON invocation.node_id=producer.id AND invocation.run_id=final.run_id
    AND invocation.organization_id=final.organization_id AND invocation.state='SUCCEEDED'
    AND invocation.resource_kind='GITHUB_REPOSITORY'
JOIN control_plane.integration_effect_receipts receipt ON receipt.id=invocation.effect_receipt_id
    AND receipt.invocation_id=invocation.id AND receipt.organization_id=invocation.organization_id
    AND receipt.effect_key=invocation.effect_key AND receipt.input_digest=invocation.input_digest
    AND receipt.result_summary=invocation.result_summary
JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id
    AND connection.organization_id=final.organization_id
    AND connection.definition_key='github'
JOIN control_plane.agents agent ON agent.id=final.agent_id AND agent.organization_id=final.organization_id
    AND agent.project_id=final.project_id
JOIN control_plane.integration_grants grant_binding ON grant_binding.id=invocation.grant_id
    AND grant_binding.organization_id=final.organization_id AND grant_binding.connection_id=connection.id
    AND grant_binding.target_kind='AGENT' AND grant_binding.target_ref=agent.ref
    AND grant_binding.enabled AND grant_binding.version=invocation.grant_version
    AND grant_binding.resource_scope=invocation.resource_scope
    AND grant_binding.resource_scope_digest=invocation.resource_scope_digest
WHERE receipt.ref=@receipt_ref
ORDER BY producer.id
LIMIT 2
FOR SHARE OF grant_binding NOWAIT;
