-- name: workflow_effect__receipt :one
SELECT invocation.ref,invocation.state,invocation.effect_key,invocation.input_digest,
       COALESCE(receipt.ref,''),COALESCE(receipt.response_digest,''),COALESCE(publication.ref,''),
       invocation.idempotency_key,COALESCE(gate.ref,''),invocation.bounded_input,
       COALESCE(publication.manifest_metadata,'null'::jsonb),COALESCE(publication.input_snapshot->>'SourcePolicy','')
FROM control_plane.run_nodes anchor
JOIN control_plane.run_nodes origin ON origin.id=@origin_node_id::uuid AND origin.organization_id=anchor.organization_id
    AND origin.root_run_id=anchor.root_run_id AND COALESCE(origin.workflow_step_origin_node_id,origin.id)=anchor.id
    AND origin.agent_id=anchor.agent_id AND origin.workflow_step_key=anchor.workflow_step_key
JOIN control_plane.runtime_revisions revision ON revision.id=@origin_revision_id::uuid AND revision.node_id=origin.id
    AND revision.organization_id=origin.organization_id AND revision.run_id=origin.run_id AND revision.root_run_id=origin.root_run_id
    AND revision.agent_id=origin.agent_id AND revision.turn_id=origin.turn_id AND revision.attempt=origin.attempt
JOIN control_plane.runtime_leases lease ON lease.id=@origin_lease_id::uuid AND lease.runtime_revision_id=revision.id
    AND lease.organization_id=revision.organization_id AND lease.node_id=origin.id AND lease.run_id=origin.run_id AND lease.generation=revision.generation
JOIN control_plane.integration_invocations invocation ON invocation.node_id=origin.id AND invocation.run_id=origin.run_id
    AND invocation.organization_id=origin.organization_id AND invocation.ref=@invocation_ref
LEFT JOIN control_plane.integration_effect_receipts receipt ON receipt.id=invocation.effect_receipt_id AND receipt.invocation_id=invocation.id
    AND receipt.organization_id=invocation.organization_id AND receipt.effect_key=invocation.effect_key AND receipt.input_digest=invocation.input_digest
    AND receipt.result_summary=invocation.result_summary
LEFT JOIN control_plane.repository_bundle_publications publication ON publication.integration_invocation_id=invocation.id
    AND publication.organization_id=origin.organization_id AND publication.root_run_id=origin.root_run_id AND publication.origin_node_id=origin.id
    AND publication.origin_runtime_revision_id=revision.id AND publication.origin_lease_ref=lease.ref AND publication.origin_generation=revision.generation
    AND publication.state='SUCCEEDED' AND publication.candidate_commit_sha=receipt.provider_effect_ref
LEFT JOIN control_plane.owner_gates gate ON gate.integration_invocation_id=invocation.id
WHERE anchor.organization_id=@organization_id::uuid AND anchor.root_run_id=@root_run_id::uuid AND anchor.id=@anchor_id::uuid
    AND invocation.operation=@operation AND invocation.connection_id=(SELECT id FROM control_plane.integration_connections WHERE organization_id=anchor.organization_id AND ref=@connection_ref)
    AND invocation.grant_id=(SELECT id FROM control_plane.integration_grants WHERE organization_id=anchor.organization_id AND ref=@grant_ref)
    AND invocation.grant_version=@grant_version AND invocation.definition_digest=@definition_digest
    AND invocation.resource_scope_digest=@scope_digest AND invocation.approval_policy=@approval_policy;
