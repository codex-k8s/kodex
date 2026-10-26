-- name: workflow_effect__result :many
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
, bridges AS MATERIALIZED (
SELECT invocation.operation,invocation.bounded_input,invocation.resource_scope,receipt.result_summary,
       receipt.response_digest,receipt.provider_effect_ref,'' AS failure_reason,
       anchor.id AS anchor_id,intent.value->>'resultKey' AS result_key,invocation.id AS invocation_id,receipt.id AS receipt_id,producer.depth
FROM lineage producer CROSS JOIN final
JOIN control_plane.run_nodes current_node ON current_node.id=producer.id AND current_node.organization_id=final.organization_id
    AND current_node.root_run_id=final.root_run_id AND current_node.run_id=final.run_id
JOIN control_plane.runtime_revisions current_proof ON current_proof.id::text=producer.workflow_captured_artifacts->>'runtimeRevisionId'
    AND current_proof.organization_id=final.organization_id AND current_proof.root_run_id=final.root_run_id
    AND current_proof.run_id=final.run_id AND current_proof.node_id=producer.id AND current_proof.agent_id=final.agent_id
    AND current_proof.turn_id=producer.turn_id AND current_proof.attempt=producer.attempt
JOIN control_plane.runtime_leases current_lease ON current_lease.id::text=producer.workflow_captured_artifacts->>'leaseId'
    AND current_lease.organization_id=current_proof.organization_id AND current_lease.runtime_revision_id=current_proof.id
    AND current_lease.node_id=producer.id AND current_lease.run_id=current_proof.run_id AND current_lease.generation=current_proof.generation
    AND to_jsonb(current_lease.generation)=producer.workflow_captured_artifacts->'generation' AND current_lease.state='COMPLETED'
CROSS JOIN LATERAL jsonb_array_elements(current_node.workflow_effect_rejoins) rejoin(value)
JOIN control_plane.run_nodes anchor ON anchor.id=final.origin_id AND anchor.organization_id=final.organization_id
    AND anchor.root_run_id=final.root_run_id AND anchor.agent_id=final.agent_id AND anchor.workflow_step_key=final.workflow_step_key
    AND anchor.workflow_step_origin_node_id IS NULL AND anchor.workflow_business_attempt=1
CROSS JOIN LATERAL jsonb_array_elements(anchor.workflow_effect_intents) intent(value)
JOIN control_plane.run_nodes origin ON origin.id::text=intent.value->>'originNodeId' AND origin.organization_id=final.organization_id
    AND origin.root_run_id=final.root_run_id AND origin.agent_id=final.agent_id AND origin.workflow_step_key=final.workflow_step_key
    AND COALESCE(origin.workflow_step_origin_node_id,origin.id)=anchor.id
    AND origin.run_id::text=intent.value->>'originRunId' AND to_jsonb(origin.attempt)=intent.value->'originAttempt'
    AND to_jsonb(origin.workflow_business_attempt)=intent.value->'originBusinessAttempt'
JOIN control_plane.runtime_revisions proof ON proof.id::text=intent.value->>'originRevisionId' AND proof.node_id=origin.id
    AND proof.organization_id=final.organization_id AND proof.root_run_id=final.root_run_id AND proof.run_id=origin.run_id
    AND proof.agent_id=final.agent_id AND proof.turn_id=origin.turn_id AND proof.attempt=origin.attempt
    AND to_jsonb(proof.generation)=intent.value->'originGeneration'
JOIN control_plane.runtime_leases origin_lease ON origin_lease.id::text=intent.value->>'originLeaseId' AND origin_lease.runtime_revision_id=proof.id
    AND origin_lease.organization_id=proof.organization_id AND origin_lease.run_id=origin.run_id AND origin_lease.node_id=origin.id AND origin_lease.generation=proof.generation
JOIN control_plane.integration_invocations invocation ON invocation.ref=intent.value->>'invocationRef' AND invocation.organization_id=final.organization_id
    AND invocation.run_id=origin.run_id AND invocation.node_id=origin.id AND invocation.state='SUCCEEDED'
    AND invocation.operation=intent.value->>'operation' AND invocation.effect_key=intent.value->>'effectKey'
    AND invocation.input_digest=intent.value->>'inputDigest' AND invocation.resource_scope_digest=intent.value->>'scopeDigest'
    AND invocation.definition_digest=intent.value->>'definitionDigest' AND to_jsonb(invocation.grant_version)=intent.value->'grantVersion'
    AND invocation.approval_policy=intent.value->>'approvalPolicy' AND (invocation.resource_kind='GITHUB_REPOSITORY' OR (invocation.resource_kind='SSH_TARGET' AND invocation.operation='ssh.compose.deploy'
      AND invocation.ssh_compose_snapshot->>'originRuntimeRevisionId'=proof.id::text
      AND invocation.ssh_compose_snapshot->>'originLeaseId'=origin_lease.id::text
      AND invocation.ssh_compose_snapshot->'originGeneration'=to_jsonb(proof.generation)
      AND invocation.ssh_compose_snapshot->>'deploymentRef'=invocation.ref
      AND invocation.ssh_compose_snapshot->>'deploymentEffectKey'=invocation.effect_key
      AND invocation.ssh_compose_snapshot->>'deploymentInputDigest'=invocation.input_digest))
JOIN control_plane.integration_effect_receipts receipt ON receipt.id=invocation.effect_receipt_id AND receipt.organization_id=final.organization_id
    AND receipt.invocation_id=invocation.id AND receipt.effect_key=invocation.effect_key AND receipt.input_digest=invocation.input_digest
    AND receipt.result_summary=invocation.result_summary AND receipt.ref=rejoin.value->>'receiptRef' AND receipt.response_digest=rejoin.value->>'receiptDigest'
JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id AND connection.organization_id=final.organization_id
    AND connection.ref=intent.value->>'connectionRef' AND to_jsonb(connection.version)=intent.value->'connectionVersion'
    AND connection.enabled AND connection.state='CONNECTED'
    AND ((connection.definition_key='github' AND invocation.resource_kind='GITHUB_REPOSITORY')
      OR (connection.definition_key='ssh-compose' AND invocation.resource_kind='SSH_TARGET' AND invocation.operation='ssh.compose.deploy'))
    AND connection.definition_digest=invocation.definition_digest AND connection.definition_version=invocation.definition_version
JOIN control_plane.agents agent ON agent.id=final.agent_id AND agent.organization_id=final.organization_id AND agent.project_id=final.project_id AND agent.enabled AND agent.state='READY'
JOIN control_plane.integration_grants grant_binding ON grant_binding.id=invocation.grant_id AND grant_binding.organization_id=final.organization_id
    AND grant_binding.connection_id=connection.id AND grant_binding.ref=intent.value->>'grantRef'
    AND grant_binding.enabled AND grant_binding.version=invocation.grant_version AND grant_binding.target_kind='AGENT' AND grant_binding.target_ref=agent.ref
    AND grant_binding.resource_scope=invocation.resource_scope AND grant_binding.resource_scope_digest=invocation.resource_scope_digest
    AND grant_binding.definition_digest=invocation.definition_digest AND grant_binding.definition_version=invocation.definition_version
WHERE receipt.ref=@receipt_ref AND rejoin.value->>'schema'='kodex.workflow-effect-rejoin.v1'
    AND intent.value->>'schema'='kodex.workflow-effect-intent.v1' AND rejoin.value->>'resultKey'=intent.value->>'resultKey'
    AND rejoin.value->>'operation'=intent.value->>'operation' AND rejoin.value->>'semanticDigest'=intent.value->>'semanticDigest'
    AND rejoin.value->>'invocationRef'=invocation.ref AND rejoin.value->>'runtimeRevisionId'=current_proof.id::text
    AND rejoin.value->>'leaseId'=current_lease.id::text AND rejoin.value->'generation'=to_jsonb(current_lease.generation)
    AND rejoin.value->'runtimeAttempt'=to_jsonb(current_node.attempt) AND rejoin.value->'businessAttempt'=to_jsonb(current_node.workflow_business_attempt)
    AND NOT EXISTS (SELECT 1 FROM lineage WHERE cycle OR depth>64)
    AND NOT EXISTS (SELECT 1 FROM lineage item WHERE
        (SELECT count(*) FROM control_plane.run_edges incoming WHERE incoming.organization_id=final.organization_id
         AND incoming.root_run_id=final.root_run_id AND incoming.type='CONTINUES' AND incoming.target_node_id=item.id)>1
        OR EXISTS (SELECT 1 FROM control_plane.run_edges incoming WHERE incoming.organization_id=final.organization_id
            AND incoming.root_run_id=final.root_run_id AND incoming.type='CONTINUES' AND incoming.target_node_id=item.id
            AND NOT EXISTS (SELECT 1 FROM lineage previous WHERE previous.id=incoming.source_node_id AND previous.depth=item.depth+1)))
    AND EXISTS (SELECT 1 FROM jsonb_array_elements(COALESCE(proof.safe_snapshot->'integrationGrants','[]'::jsonb)) pin
        WHERE pin->>'ref'=grant_binding.ref AND pin->>'grantVersion'=grant_binding.version::text AND pin->>'connectionRef'=connection.ref
            AND pin->>'operation'=invocation.operation AND pin->>'definitionDigest'=invocation.definition_digest AND pin->>'approvalPolicy'=invocation.approval_policy)
    AND EXISTS (SELECT 1 FROM jsonb_array_elements(COALESCE(current_proof.safe_snapshot->'integrationGrants','[]'::jsonb)) pin
        WHERE pin->>'ref'=grant_binding.ref AND pin->>'grantVersion'=grant_binding.version::text AND pin->>'connectionRef'=connection.ref
            AND pin->>'operation'=invocation.operation AND pin->>'definitionDigest'=invocation.definition_digest AND pin->>'approvalPolicy'=invocation.approval_policy)
FOR SHARE OF grant_binding NOWAIT
)
SELECT operation,bounded_input,resource_scope,result_summary,response_digest,provider_effect_ref,failure_reason
FROM (
    SELECT bridges.*,row_number() OVER (
        PARTITION BY anchor_id,result_key,operation,invocation_id,receipt_id ORDER BY depth
    ) AS exact_bridge_rank FROM bridges
) proved
WHERE exact_bridge_rank=1
ORDER BY anchor_id,result_key,operation,invocation_id,receipt_id
LIMIT 2;
